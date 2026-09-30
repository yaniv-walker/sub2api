package repository

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/stretchr/testify/require"
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
