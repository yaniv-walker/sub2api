package middleware

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// UpstreamRetryGuardrails snapshots the runtime policy once per request.
// The request path only performs an in-memory setting read.
func UpstreamRetryGuardrails(settingService *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request == nil {
			c.Next()
			return
		}
		enabled := settingService != nil && settingService.UpstreamRetryGuardrailsEnabled()
		ctx := context.WithValue(c.Request.Context(), ctxkey.UpstreamRetryGuardrailsEnabled, enabled)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
