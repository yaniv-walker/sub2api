package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"mime"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// appendRequestObservabilityFields adds bounded metadata to the existing
// completion event only for requests carrying observability state.
func appendRequestObservabilityFields(fields []zap.Field, c *gin.Context, endTime time.Time) []zap.Field {
	state := requestobs.FromContext(c.Request.Context())
	statusCode := c.Writer.Status()
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

	return fields
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
		// A failed attempt is not proof of the final stream outcome. The
		// generic writer deliberately does not parse protocol error frames.
		return requestobs.TerminalUnknown
	}
	if snapshot.StatusCode >= 500 {
		return requestobs.TerminalHandlerError
	}
	if snapshot.StatusCode >= 200 && snapshot.StatusCode < 500 {
		return requestobs.TerminalCompleted
	}
	return requestobs.TerminalUnknown
}
