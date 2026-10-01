package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failingObservabilityWriter struct {
	gin.ResponseWriter
}

func (w *failingObservabilityWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("client socket closed")
}

func TestRequestObservabilityCapturesStreamingFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLogger(t)
	r := gin.New()
	r.Use(RequestLogger(), RequestObservability(), Logger())
	r.GET("/v1/stream/:id", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		_, _ = c.Writer.Write([]byte("a"))
		_, _ = c.Writer.Write([]byte("b"))
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/stream/secret-id", nil))

	require.Equal(t, http.StatusOK, w.Code)
	for _, event := range sink.list() {
		if event == nil || event.Message != "http request completed" {
			continue
		}
		require.Equal(t, "/v1/stream/:id", event.Fields["route_template"])
		require.Equal(t, true, event.Fields["streaming"])
		require.Equal(t, "completed", event.Fields["terminal_reason"])
		require.Equal(t, int64(2), event.Fields["bytes_written"])
		require.Equal(t, int64(2), event.Fields["write_count"])
		require.Contains(t, event.Fields, "first_write_ms")
		require.NotContains(t, event.Fields, "first_token_ms")
		return
	}
	t.Fatal("request completion event not found")
}

func TestRequestObservabilityPreservesWriterInterfaces(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestObservability())
	r.GET("/", func(c *gin.Context) {
		_, ok := c.Writer.(http.Flusher)
		require.True(t, ok)
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestRequestObservabilityDetectsDownstreamWriteError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := initMiddlewareTestLogger(t)
	r := gin.New()
	r.Use(RequestLogger())
	r.Use(func(c *gin.Context) {
		c.Writer = &failingObservabilityWriter{ResponseWriter: c.Writer}
		c.Next()
	})
	r.Use(RequestObservability(), Logger())
	r.GET("/v1/stream", func(c *gin.Context) {
		_, err := c.Writer.Write([]byte("private body"))
		require.Error(t, err)
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/stream", nil))
	for _, event := range sink.list() {
		if event != nil && event.Message == "http request completed" {
			require.Equal(t, "downstream_write_error", event.Fields["terminal_reason"])
			require.NotContains(t, event.Fields, "private body")
			return
		}
	}
	t.Fatal("request completion event not found")
}

func TestRequestTerminalReasonIsConservative(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request = req

	require.Equal(t, requestobs.TerminalDownstreamWrite, requestTerminalReason(c, requestobs.Snapshot{WriteFailed: true, StatusCode: 200}))
	require.Equal(t, requestobs.TerminalUpstreamError, requestTerminalReason(c, requestobs.Snapshot{StatusCode: 502, UpstreamErrors: 1}))
	require.Equal(t, requestobs.TerminalUnknown, requestTerminalReason(c, requestobs.Snapshot{StatusCode: 200, UpstreamErrors: 1, UpstreamLastStatus: 503}))
	require.Equal(t, requestobs.TerminalCompleted, requestTerminalReason(c, requestobs.Snapshot{StatusCode: 200, UpstreamErrors: 1, UpstreamLastStatus: 200}))
	require.Equal(t, requestobs.TerminalHandlerError, requestTerminalReason(c, requestobs.Snapshot{StatusCode: 500}))
	require.Equal(t, requestobs.TerminalUnknown, requestTerminalReason(c, requestobs.Snapshot{StatusCode: 0}))

	canceled, cancel := context.WithCancel(req.Context())
	cancel()
	c.Request = req.WithContext(canceled)
	require.Equal(t, requestobs.TerminalClientCancelled, requestTerminalReason(c, requestobs.Snapshot{StatusCode: 200}))
	deadline, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	c.Request = req.WithContext(deadline)
	require.Equal(t, requestobs.TerminalDeadline, requestTerminalReason(c, requestobs.Snapshot{StatusCode: 200}))
}

func TestStatusClassUsesFixedValues(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{200, "2xx"}, {403, "4xx"}, {503, "5xx"}, {0, "unknown"}, {999, "unknown"},
	} {
		require.Equal(t, tc.want, statusClass(tc.status))
	}
}

func TestRequestObservabilityRuntimeSwitchPreservesInFlightSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var enabled atomic.Bool
	enabled.Store(true)
	router := gin.New()
	router.Use(RequestObservability(enabled.Load))
	started := make(chan *requestobs.State, 1)
	release := make(chan struct{})
	router.GET("/long", func(c *gin.Context) {
		state := requestobs.FromContext(c.Request.Context())
		_, _ = c.Writer.WriteString("first")
		started <- state
		<-release
		_, _ = c.Writer.WriteString("last")
	})
	router.GET("/probe", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"observed": requestobs.FromContext(c.Request.Context()) != nil})
	})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/long", nil))
	}()
	var state *requestobs.State
	select {
	case state = <-started:
	case <-time.After(time.Second):
		t.Fatal("long request did not start")
	}
	enabled.Store(false)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	require.JSONEq(t, `{"observed":false}`, w.Body.String())
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("long request did not finish")
	}
	require.NotNil(t, state)
	require.Equal(t, int64(2), state.Snapshot().WriteCount)
	enabled.Store(true)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
	require.JSONEq(t, `{"observed":true}`, w.Body.String())
}

func BenchmarkRequestObservabilityMiddleware(b *testing.B) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		b.Run(name, func(b *testing.B) {
			r := gin.New()
			if enabled {
				r.Use(RequestObservability())
			}
			r.GET("/v1/stream", func(c *gin.Context) {
				_, _ = c.Writer.Write([]byte("data: test\n\n"))
			})
			req := httptest.NewRequest(http.MethodGet, "/v1/stream", nil)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				r.ServeHTTP(httptest.NewRecorder(), req)
			}
		})
	}
}
