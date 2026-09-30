package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"mime"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Logger 请求日志中间件
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 开始时间
		startTime := time.Now()

		// 请求路径
		path := c.Request.URL.Path

		// 处理请求
		c.Next()

		// 跳过健康检查等高频探针路径的日志
		if path == "/health" || path == "/setup/status" {
			return
		}

		endTime := time.Now()
		latency := endTime.Sub(startTime)

		method := c.Request.Method
		statusCode := c.Writer.Status()
		clientIP := ip.GetClientIP(c)
		protocol := c.Request.Proto
		accountID, hasAccountID := c.Request.Context().Value(ctxkey.AccountID).(int64)
		platform, _ := c.Request.Context().Value(ctxkey.Platform).(string)
		model, _ := c.Request.Context().Value(ctxkey.Model).(string)
		state := requestobs.FromContext(c.Request.Context())
		reason, rejected := GetIngressRejectReason(c)
		if rejected {
			recordIngressReject(c, reason)
			allowed, droppedSummary := globalIngressRejectAccessSampler.allow(endTime)
			if droppedSummary > 0 {
				logger.FromContext(c.Request.Context()).Info("ingress rejection access logs dropped",
					zap.String("component", "http.access"),
					zap.Uint64("dropped_count", droppedSummary),
					zap.Bool(logger.OpsSystemLogSkipField, true),
				)
			}
			if !allowed {
				return
			}
		}

		fields := []zap.Field{
			zap.String("component", "http.access"),
			zap.Int("status_code", statusCode),
			zap.Int64("latency_ms", latency.Milliseconds()),
			zap.String("client_ip", clientIP),
			zap.String("protocol", protocol),
			zap.String("method", method),
			zap.String("path", path),
		}
		if state != nil {
			snapshot := state.Snapshot()
			snapshot.StatusCode = statusCode
			routeTemplate := c.FullPath()
			if routeTemplate == "" {
				routeTemplate = "unmatched"
			}
			fields = append(fields,
				zap.String("route_template", routeTemplate),
				zap.Bool("streaming", isStreamingResponse(c.Writer.Header().Get("Content-Type"))),
				zap.String("status_class", statusClass(statusCode)),
				zap.String("terminal_reason", string(requestTerminalReason(c, snapshot))),
				zap.Int64("bytes_written", snapshot.BytesWritten),
				zap.Int64("write_count", snapshot.WriteCount),
				zap.Int("upstream_attempts", snapshot.UpstreamAttempts),
				zap.Int("upstream_errors", snapshot.UpstreamErrors),
			)
			if externalRequestID, ok := c.Request.Context().Value(ctxkey.ExternalRequestID).(string); ok && externalRequestID != "" {
				sum := sha256.Sum256([]byte(externalRequestID))
				fields = append(fields, zap.String("external_request_id_sha256", hex.EncodeToString(sum[:])))
			}
			if !snapshot.FirstWriteAt.IsZero() {
				fields = append(fields, zap.Int64("first_write_ms", snapshot.FirstWriteAt.Sub(snapshot.StartedAt).Milliseconds()))
			}
			if snapshot.MaxWriteGap > 0 {
				fields = append(fields, zap.Int64("max_write_gap_ms", snapshot.MaxWriteGap.Milliseconds()))
			}
			if !snapshot.LastWriteAt.IsZero() {
				fields = append(fields, zap.Int64("terminal_silence_ms", endTime.Sub(snapshot.LastWriteAt).Milliseconds()))
			}
			if snapshot.UpstreamLastStatus > 0 {
				fields = append(fields,
					zap.Int("upstream_last_status", snapshot.UpstreamLastStatus),
					zap.Int64("upstream_last_duration_ms", snapshot.UpstreamLastDuration.Milliseconds()),
					zap.Int64("upstream_total_duration_ms", snapshot.UpstreamTotalDuration.Milliseconds()),
				)
			}
		}
		if rejected {
			fields = append(fields,
				zap.String("ingress_reject_reason", string(reason)),
				zap.Bool(logger.OpsSystemLogSkipField, true),
			)
		}
		if hasAccountID && accountID > 0 {
			fields = append(fields, zap.Int64("account_id", accountID))
		}
		if platform != "" {
			fields = append(fields, zap.String("platform", platform))
		}
		if model != "" {
			fields = append(fields, zap.String("model", model))
		}

		l := logger.FromContext(c.Request.Context()).With(fields...)
		l.Info("http request completed", zap.Time("completed_at", endTime))

		if len(c.Errors) > 0 {
			l.Warn("http request contains gin errors", zap.String("errors", c.Errors.String()))
		}
	}
}

func statusClass(statusCode int) string {
	switch statusCode / 100 {
	case 1:
		return "1xx"
	case 2:
		return "2xx"
	case 3:
		return "3xx"
	case 4:
		return "4xx"
	case 5:
		return "5xx"
	default:
		return "unknown"
	}
}

func isStreamingResponse(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && strings.EqualFold(mediaType, "text/event-stream")
}

func requestTerminalReason(c *gin.Context, snapshot requestobs.Snapshot) requestobs.TerminalReason {
	if snapshot.WriteFailed {
		return requestobs.TerminalDownstreamWrite
	}
	if c != nil && c.Request != nil {
		switch c.Request.Context().Err() {
		case context.Canceled:
			return requestobs.TerminalClientCancelled
		case context.DeadlineExceeded:
			return requestobs.TerminalDeadline
		}
	}
	if snapshot.StatusCode == 499 {
		return requestobs.TerminalClientCancelled
	}
	if snapshot.UpstreamErrors > 0 && snapshot.StatusCode >= 500 {
		return requestobs.TerminalUpstreamError
	}
	if snapshot.UpstreamErrors > 0 && snapshot.UpstreamLastStatus >= 500 && snapshot.StatusCode == 200 {
		return requestobs.TerminalUpstreamError
	}
	if snapshot.StatusCode >= 500 {
		return requestobs.TerminalHandlerError
	}
	if snapshot.StatusCode >= 200 && snapshot.StatusCode < 500 {
		return requestobs.TerminalCompleted
	}
	return requestobs.TerminalUnknown
}
