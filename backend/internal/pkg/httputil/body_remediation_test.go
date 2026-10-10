package httputil

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/stretchr/testify/require"
)

func TestBodyRemediationObservationPreservesNormalizedResult(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		s := requestobs.New(time.Now())
		req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader("{\"content\":\"private\ntext\"}"))
		req = req.WithContext(context.WithValue(requestobs.WithContext(req.Context(), s), ctxkey.UpstreamRetryGuardrailsEnabled, enabled))
		body, err := ReadLenientJSONRequestBodyWithPrealloc(req, 1024)
		require.NoError(t, err)
		require.JSONEq(t, `{"content":"private\ntext"}`, string(body))
		if enabled {
			require.Len(t, s.StageSnapshot(), 1)
			require.Equal(t, "body_read_decode", s.StageSnapshot()[0].Stage)
		} else {
			require.Empty(t, s.StageSnapshot())
		}
	}
}
