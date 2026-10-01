package repository

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type observabilityRoundTrip func(*http.Request) (*http.Response, error)

func (f observabilityRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDoUpstreamRequestRecords503ThenSuccess(t *testing.T) {
	state := requestobs.New(time.Now())
	ctx := requestobs.WithContext(t.Context(), state)
	attempts := 0
	client := &http.Client{Transport: observabilityRoundTrip(func(req *http.Request) (*http.Response, error) {
		attempts++
		status := http.StatusServiceUnavailable
		if attempts == 2 {
			status = http.StatusOK
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("private response body")),
			Request:    req,
		}, nil
	})}

	for range 2 {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test/v1/chat", nil)
		require.NoError(t, err)
		resp, err := doUpstreamRequest(client, req, 42)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}

	snapshot := state.Snapshot()
	require.Equal(t, 2, snapshot.UpstreamAttempts)
	require.Equal(t, 1, snapshot.UpstreamErrors)
	require.Equal(t, http.StatusOK, snapshot.UpstreamLastStatus)
	require.GreaterOrEqual(t, snapshot.UpstreamTotalDuration, time.Duration(0))
}

func TestDoUpstreamRequestRecordsTransportError(t *testing.T) {
	state := requestobs.New(time.Now())
	client := &http.Client{Transport: observabilityRoundTrip(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("private error details")
	})}
	req, err := http.NewRequestWithContext(requestobs.WithContext(t.Context(), state), http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)
	_, err = doUpstreamRequest(client, req, 42)
	require.Error(t, err)
	snapshot := state.Snapshot()
	require.Equal(t, 1, snapshot.UpstreamAttempts)
	require.Equal(t, 1, snapshot.UpstreamErrors)
	require.Equal(t, 0, snapshot.UpstreamLastStatus)
}

type panickingObservabilitySink struct{}

func (panickingObservabilitySink) Write([]byte) (int, error) { panic("broken telemetry sink") }
func (panickingObservabilitySink) Sync() error               { return nil }

func TestDoUpstreamRequestSinkFailurePreservesResponse(t *testing.T) {
	state := requestobs.New(time.Now())
	core := zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), panickingObservabilitySink{}, zapcore.InfoLevel)
	ctx := logger.IntoContext(requestobs.WithContext(t.Context(), state), zap.New(core))
	calls := 0
	client := &http.Client{Transport: observabilityRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("intact body")), Request: req}, nil
	})}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test", nil)
	require.NoError(t, err)
	resp, err := doUpstreamRequest(client, req, 42)
	require.NoError(t, err)
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "intact body", string(data))
	require.NoError(t, resp.Body.Close())
	require.NoError(t, ctx.Err(), "attempt cleanup must not cancel caller")
	require.Equal(t, 1, calls, "telemetry failure must not replay the request")
}
