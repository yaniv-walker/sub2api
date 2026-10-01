package middleware

import (
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/gin-gonic/gin"
)

// requestObservabilityWriter observes response writes without buffering or
// inspecting their contents. It is deliberately a thin wrapper so streaming
// handlers retain their existing Flush/Hijack behavior.
type requestObservabilityWriter struct {
	gin.ResponseWriter
	state *requestobs.State
}

func (w *requestObservabilityWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *requestObservabilityWriter) WriteHeader(statusCode int) {
	w.state.SetStatus(statusCode)
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *requestObservabilityWriter) WriteHeaderNow() {
	w.state.SetStatus(w.ResponseWriter.Status())
	w.ResponseWriter.WriteHeaderNow()
}

func (w *requestObservabilityWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.state.SetStatus(w.ResponseWriter.Status())
	w.state.ObserveWrite(n, err, time.Now())
	return n, err
}

func (w *requestObservabilityWriter) WriteString(data string) (int, error) {
	n, err := w.ResponseWriter.WriteString(data)
	w.state.SetStatus(w.ResponseWriter.Status())
	w.state.ObserveWrite(n, err, time.Now())
	return n, err
}

func (w *requestObservabilityWriter) Flush() {
	w.state.SetStatus(w.ResponseWriter.Status())
	w.ResponseWriter.Flush()
}

// RequestObservability installs request-scoped state and observes downstream
// writes. It does not emit logs itself; the existing Logger middleware emits
// one bounded completion event after all handlers return.
func RequestObservability(enabled ...func() bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request == nil || (len(enabled) > 0 && (enabled[0] == nil || !enabled[0]())) {
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
		defer func() { c.Writer = originalWriter }()
		c.Next()
	}
}
