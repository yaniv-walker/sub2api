package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/stretchr/testify/require"
)

func TestRemediationProtocolClassifiesOnlyKnownTerminals(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, event := range []string{"response.completed", "response.done", "message_stop", "[DONE]", "response.failed", "response.incomplete", "", "private-payload"} {
			state := requestobs.New(time.Now())
			ctx := context.WithValue(requestobs.WithContext(context.Background(), state), ctxkey.UpstreamRetryGuardrailsEnabled, enabled)
			observeRemediationProtocol(ctx, event)
			got := state.Snapshot()
			if !enabled {
				require.Empty(t, got.ProtocolTerminal)
				continue
			}
			complete := event == "response.completed" || event == "response.done" || event == "message_stop" || event == "[DONE]"
			require.Equal(t, complete, got.ProtocolComplete)
			if event == "" || event == "private-payload" {
				require.Equal(t, "missing_terminal", got.ProtocolTerminal)
			} else {
				require.Equal(t, event, got.ProtocolTerminal)
			}
		}
	}
	observeRemediationProtocol(nil, "")
}
