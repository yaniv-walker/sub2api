package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
)

// observeRemediationProtocol consumes only the adapter's existing classification;
// it never reads response bytes or changes protocol output or billing results.
func observeRemediationProtocol(ctx context.Context, event string) {
	if ctx == nil {
		return
	}
	enabled, _ := ctx.Value(ctxkey.UpstreamRetryGuardrailsEnabled).(bool)
	if !enabled {
		return
	}
	state := requestobs.FromContext(ctx)
	if state == nil {
		return
	}
	switch event {
	case "response.completed", "response.done", "message_stop", "[DONE]":
		state.MarkProtocolTerminal(event, true)
	case "response.failed", "response.incomplete", "response.cancelled", "response.canceled", "error":
		state.MarkProtocolTerminal(event, false)
	default:
		state.MarkProtocolTerminal("missing_terminal", false)
	}
}
