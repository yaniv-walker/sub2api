package handler

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
)

// upstreamRetryGuardrailsEnabled is deliberately request-scoped so a setting
// change affects new requests only. An in-flight request keeps its snapshot.
func upstreamRetryGuardrailsEnabled(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	enabled, _ := c.Request.Context().Value(ctxkey.UpstreamRetryGuardrailsEnabled).(bool)
	return enabled
}

// upstreamRetryAllowedAfterWrite keeps the legacy SafeToFailoverAfterWrite
// exception disabled when the opt-in strict policy is enabled. Existing
// callers already pass the protocol-specific write snapshot.
func upstreamRetryAllowedAfterWrite(c *gin.Context, downstreamResponseStarted, rawDownstreamResponseStarted bool, safeToFailoverAfterWrite bool) bool {
	if c == nil {
		return false
	}
	if c.Request != nil && c.Request.Context().Err() != nil {
		return false
	}
	if upstreamRetryGuardrailsEnabled(c) && (downstreamResponseStarted || rawDownstreamResponseStarted) {
		return false
	}
	// Preserve the legacy behavior: a non-semantic keepalive does not count as
	// downstream output for the existing failover decision.
	if !downstreamResponseStarted {
		return true
	}
	return safeToFailoverAfterWrite
}

// withUpstreamRetryGuardrailsValue is kept small for middleware and tests.
func withUpstreamRetryGuardrailsValue(ctx context.Context, enabled bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxkey.UpstreamRetryGuardrailsEnabled, enabled)
}
