package middleware

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/gin-gonic/gin"
)

// GatewayRemediation is the single request aspect for additive remediation
// behavior. Disabled mode only propagates a false request snapshot and leaves
// the request body, writer, and handler path untouched.
func GatewayRemediation(enabled func() bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request == nil {
			c.Next()
			return
		}
		active := enabled != nil && enabled()
		ctx := context.WithValue(c.Request.Context(), ctxkey.UpstreamRetryGuardrailsEnabled, active)
		c.Request = c.Request.WithContext(ctx)
		if !active {
			c.Next()
			return
		}

		state := requestobs.FromContext(c.Request.Context())
		if state == nil {
			state = requestobs.New(time.Now())
			c.Request = c.Request.WithContext(requestobs.WithContext(c.Request.Context(), state))
		}

		originalWriter := c.Writer
		c.Writer = &requestObservabilityWriter{ResponseWriter: originalWriter, state: state}
		var body *remediationBody
		if c.Request.Body != nil {
			body = newRemediationBody(c.Request, logger.FromContext(c.Request.Context()))
			c.Request.Body = body
		}
		defer func() {
			if body != nil {
				body.logCompletion()
			}
			c.Writer = originalWriter
		}()
		c.Next()
	}
}
