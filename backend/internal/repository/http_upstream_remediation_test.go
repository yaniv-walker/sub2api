package repository

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/stretchr/testify/require"
)

func TestRemediationHTTPBoundaryMarksStartBeforeTransport(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		state := requestobs.New(time.Now())
		ctx := context.WithValue(requestobs.WithContext(t.Context(), state), ctxkey.UpstreamRetryGuardrailsEnabled, enabled)
		var first time.Time
		client := &http.Client{Transport: observabilityRoundTrip(func(req *http.Request) (*http.Response, error) {
			if enabled {
				require.False(t, state.FirstUpstreamAt().IsZero())
			} else {
				require.True(t, state.FirstUpstreamAt().IsZero())
			}
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("private response")), Request: req}, nil
		})}
		for i := 0; i < 2; i++ {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test/v1/responses", nil)
			require.NoError(t, err)
			resp, err := doUpstreamRequest(client, req, 42)
			require.NoError(t, err)
			data, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, "private response", string(data))
			require.NoError(t, resp.Body.Close())
			if i == 0 {
				first = state.FirstUpstreamAt()
			} else {
				require.Equal(t, first, state.FirstUpstreamAt())
			}
		}
		require.Equal(t, 2, state.Snapshot().UpstreamAttempts)
	}
}
