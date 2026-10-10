package middleware

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type remediationErrorBody struct {
	err      error
	closeErr error
}

func (b *remediationErrorBody) Read([]byte) (int, error) { return 0, b.err }
func (b *remediationErrorBody) Close() error             { return b.closeErr }

func TestGatewayRemediationOneSnapshotAndTransparentBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []bool{false, true} {
		sink := initMiddlewareTestLogger(t)
		var reads atomic.Int32
		r := gin.New()
		r.Use(RequestLogger(), GatewayRemediation(func() bool { reads.Add(1); return enabled }), Logger())
		r.POST("/v1/messages", func(c *gin.Context) {
			require.Equal(t, enabled, requestobs.FromContext(c.Request.Context()) != nil)
			guard, _ := c.Request.Context().Value(ctxkey.UpstreamRetryGuardrailsEnabled).(bool)
			require.Equal(t, enabled, guard)
			body, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			c.String(http.StatusOK, string(body))
		})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader("private-payload")))
		require.Equal(t, "private-payload", w.Body.String())
		require.Equal(t, int32(1), reads.Load())
		found := false
		for _, event := range sink.list() {
			if event.Message == "gateway remediation body read completed" {
				found = true
				require.Equal(t, int64(15), event.Fields["body_bytes_read"])
				require.Equal(t, true, event.Fields["body_read_complete"])
				require.NotContains(t, event.Fields, "body")
			}
		}
		require.Equal(t, enabled, found)
	}
}

func TestRemediationBodyPreservesTruncatedRead(t *testing.T) {
	body := &remediationBody{ReadCloser: io.NopCloser(strings.NewReader("abc")), declared: 9}
	got, err := io.ReadAll(body)
	// The aspect observes truncation, but never invents a read error.
	require.NoError(t, err)
	require.Equal(t, "abc", string(got))
	s := body.snapshot()
	require.Equal(t, int64(3), s.bytes)
	require.False(t, s.complete)
	require.Equal(t, "truncated_body", s.errorKind)
	require.NoError(t, body.Close())
}

func TestRemediationBodyPreservesErrorsAndClose(t *testing.T) {
	for _, tc := range []struct {
		err  error
		kind string
	}{
		{context.Canceled, "client_disconnect"},
		{context.DeadlineExceeded, "deadline"},
		{io.ErrUnexpectedEOF, "truncated_body"},
		{syscall.ECONNRESET, "client_disconnect"},
		{&http.MaxBytesError{Limit: 1}, "max_bytes"},
	} {
		closeErr := errors.New("close failed")
		b := &remediationBody{ReadCloser: &remediationErrorBody{err: tc.err, closeErr: closeErr}, declared: 10}
		_, err := b.Read(make([]byte, 8))
		require.True(t, tc.err == err, "Read must preserve the original error value")
		require.Equal(t, tc.kind, b.snapshot().errorKind)
		require.False(t, b.snapshot().complete)
		require.Same(t, closeErr, b.Close())
	}
}

func TestRemediationBodyUnknownLengthRequiresEOF(t *testing.T) {
	b := &remediationBody{ReadCloser: io.NopCloser(strings.NewReader("abcdef")), declared: -1}
	require.Equal(t, "not_read", b.snapshot().errorKind)
	_, err := b.Read(make([]byte, 3))
	require.NoError(t, err)
	require.False(t, b.snapshot().complete)
	require.Equal(t, "not_complete", b.snapshot().errorKind)
	_, err = io.ReadAll(b)
	require.NoError(t, err)
	require.True(t, b.snapshot().complete)
}

func TestGatewayRemediationInFlightKeepsOneGateSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var enabled atomic.Bool
	enabled.Store(true)
	r := gin.New()
	r.Use(GatewayRemediation(enabled.Load))
	r.POST("/v1/messages", func(c *gin.Context) {
		enabled.Store(false)
		require.NotNil(t, requestobs.FromContext(c.Request.Context()))
		require.Equal(t, true, c.Request.Context().Value(ctxkey.UpstreamRetryGuardrailsEnabled))
		_, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		c.Status(204)
	})
	r.POST("/probe", func(c *gin.Context) {
		require.Nil(t, requestobs.FromContext(c.Request.Context()))
		require.Equal(t, false, c.Request.Context().Value(ctxkey.UpstreamRetryGuardrailsEnabled))
		c.Status(204)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/messages", strings.NewReader("abc")))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/probe", nil))
}

func TestGatewayRemediationRealHTTPMeasuresBlockingReads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	initMiddlewareTestLogger(t)
	readStarted := make(chan struct{})
	observed := make(chan remediationBodySnapshot, 1)
	r := gin.New()
	r.Use(GatewayRemediation(func() bool { return true }))
	r.POST("/v1/messages", func(c *gin.Context) {
		close(readStarted)
		data, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Status(500)
			return
		}
		observed <- c.Request.Body.(*remediationBody).snapshot()
		c.String(200, string(data))
	})
	srv := httptest.NewServer(r)
	defer srv.Close()
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	uploadDone := make(chan struct{})
	go func() {
		defer close(uploadDone)
		select {
		case <-readStarted:
		case <-time.After(3 * time.Second):
			_ = writer.CloseWithError(context.DeadlineExceeded)
			return
		}
		time.Sleep(40 * time.Millisecond)
		_, _ = writer.Write([]byte("private-body"))
		_ = writer.Close()
	}()
	req, err := http.NewRequest("POST", srv.URL+"/v1/messages", reader)
	require.NoError(t, err)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "private-body", string(body))
	select {
	case s := <-observed:
		require.Equal(t, int64(12), s.bytes)
		require.True(t, s.complete)
		require.GreaterOrEqual(t, s.duration, 20*time.Millisecond)
	case <-time.After(time.Second):
		t.Fatal("body observation missing")
	}
	<-uploadDone
}
