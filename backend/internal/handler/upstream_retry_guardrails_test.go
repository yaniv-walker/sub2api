package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpstreamRetryAllowedAfterWrite_LegacyModeKeepsSafeException(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, _ = c.Writer.Write([]byte("non-semantic keepalive"))

	require.True(t, upstreamRetryAllowedAfterWrite(c, true, true, true))
}

func TestUpstreamRetryAllowedAfterWrite_LegacyModeKeepsKeepaliveFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	// Legacy mode treats comment-only keepalive bytes as non-semantic and
	// preserves the original pre-output failover behavior.
	require.True(t, upstreamRetryAllowedAfterWrite(c, false, true, false))
}

func TestUpstreamRetryAllowedAfterWrite_PreservesPreResponseFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	// A failed upstream attempt that wrote nothing to the client remains
	// retryable even when SafeToFailoverAfterWrite is false.
	require.True(t, upstreamRetryAllowedAfterWrite(c, false, false, false))
}

func TestUpstreamRetryAllowedAfterWrite_StrictModeAllowsPreResponseFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx := withUpstreamRetryGuardrailsValue(context.Background(), true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)

	require.True(t, upstreamRetryAllowedAfterWrite(c, false, false, false))
}

func TestUpstreamRetryAllowedAfterWrite_StrictModeRejectsAnyWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx := withUpstreamRetryGuardrailsValue(context.Background(), true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	_, _ = c.Writer.Write([]byte("non-semantic keepalive"))

	require.False(t, upstreamRetryAllowedAfterWrite(c, true, true, true))
}

func TestUpstreamRetryAllowedAfterWrite_StrictModeRejectsNonSemanticWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx := withUpstreamRetryGuardrailsValue(context.Background(), true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)

	// The adjusted semantic counter is unchanged, but the raw writer did emit
	// a keepalive/comment. Strict mode must still stop replay after any write.
	require.False(t, upstreamRetryAllowedAfterWrite(c, false, true, true))
}

func TestUpstreamRetryAllowedAfterWrite_StrictModeRejectsSemanticWriteSignal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx := withUpstreamRetryGuardrailsValue(context.Background(), true)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)

	require.False(t, upstreamRetryAllowedAfterWrite(c, true, false, true))
}

func TestUpstreamRetryAllowedAfterWrite_CanceledRequestIsNeverReplayable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)

	require.False(t, upstreamRetryAllowedAfterWrite(c, true, true, true))
}

func TestUpstreamRetryGuardrailsValueIsRequestScoped(t *testing.T) {
	ctx := withUpstreamRetryGuardrailsValue(context.Background(), true)
	require.True(t, ctx.Value(ctxkey.UpstreamRetryGuardrailsEnabled).(bool))
}
