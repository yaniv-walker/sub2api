package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGatewayStagesSlotWaitOnlyWhenNeeded(t *testing.T) {
	for _, immediate := range []bool{true, false} {
		for _, enabled := range []bool{true, false} {
			t.Run(map[bool]string{true: "immediate", false: "wait"}[immediate]+map[bool]string{true: "/on", false: "/off"}[enabled], func(t *testing.T) {
				cache := &helperConcurrencyCacheStub{userSeq: []bool{immediate, true}, waitAllowed: true}
				helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 5*time.Millisecond)
				c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
				s := requestobs.New(time.Now())
				ctx := context.WithValue(requestobs.WithContext(c.Request.Context(), s), ctxkey.UpstreamRetryGuardrailsEnabled, enabled)
				c.Request = c.Request.WithContext(ctx)
				started := false
				release, err := helper.acquireUserSlotWithWaitTimeout(c, 2, 1, time.Second, false, &started)
				require.NoError(t, err)
				require.NotNil(t, release)
				release()
				samples := s.StageSnapshot()
				if !enabled {
					require.Empty(t, samples)
					return
				}
				byStage := map[string]requestobs.StageSample{}
				for _, sample := range samples {
					byStage[sample.Stage] = sample
				}
				require.GreaterOrEqual(t, byStage["user_slot_acquire"].Calls, 1)
				if immediate {
					require.NotContains(t, byStage, "user_slot_wait")
				} else {
					require.Equal(t, 1, byStage["user_slot_wait"].Calls)
				}
			})
		}
	}
}

func TestGatewayStagesSlotCancelReleasesWaitCounter(t *testing.T) {
	cache := &helperConcurrencyCacheStub{userSeq: []bool{false}, waitAllowed: true}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 5*time.Millisecond)
	c, _ := newHelperTestContext(http.MethodPost, "/v1/messages")
	s := requestobs.New(time.Now())
	ctx, cancel := context.WithCancel(context.WithValue(requestobs.WithContext(c.Request.Context(), s), ctxkey.UpstreamRetryGuardrailsEnabled, true))
	c.Request = c.Request.WithContext(ctx)
	cancel()
	started := false
	release, err := helper.acquireUserSlotWithWaitTimeout(c, 2, 1, time.Second, false, &started)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, release)
	require.Equal(t, 1, cache.waitIncrementCalls)
	require.Equal(t, 1, cache.waitDecrementCalls)
	require.True(t, s.FirstUpstreamAt().IsZero())
	require.Zero(t, s.Snapshot().UpstreamAttempts)
	samples := s.StageSnapshot()
	require.Len(t, samples, 2)
	require.Equal(t, "user_slot_wait", samples[1].Stage)
}

func TestGatewayJSONValidationRemainsTransparent(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		c, _ := newHelperTestContext(http.MethodPost, "/v1/responses")
		s := requestobs.New(time.Now())
		c.Request = c.Request.WithContext(context.WithValue(requestobs.WithContext(c.Request.Context(), s), ctxkey.UpstreamRetryGuardrailsEnabled, enabled))
		require.True(t, validGatewayJSON(c, []byte(`{"input":"private prompt"}`)))
		require.False(t, validGatewayJSON(c, []byte(`{"input":`)))
		samples := s.StageSnapshot()
		if !enabled {
			require.Empty(t, samples)
			continue
		}
		require.Len(t, samples, 1)
		require.Equal(t, "json_validation", samples[0].Stage)
		require.Equal(t, 2, samples[0].Calls)
	}
}
