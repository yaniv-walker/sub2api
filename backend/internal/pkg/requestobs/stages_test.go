package requestobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func remediationContext(s *State, enabled bool) context.Context {
	return context.WithValue(WithContext(context.Background(), s), ctxkey.UpstreamRetryGuardrailsEnabled, enabled)
}

func TestStagesRequireUnifiedRequestSnapshot(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		s := New(time.Now())
		finish := StartStage(remediationContext(s, enabled), StageAuthentication)
		finish()
		finish()
		samples := s.StageSnapshot()
		if !enabled {
			require.Empty(t, samples)
			continue
		}
		require.Len(t, samples, 1)
		require.Equal(t, "authentication", samples[0].Stage)
		require.Equal(t, 1, samples[0].Calls)
	}
	StartStage(nil, StageAuthentication)()
	StartStage(context.Background(), StageAuthentication)()
}

func TestStagesAggregateBoundedConcurrentOperations(t *testing.T) {
	s := New(time.Now())
	ctx := remediationContext(s, true)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); finish := StartStage(ctx, StageRouting); finish(); finish() }()
	}
	wg.Wait()
	StartStage(ctx, Stage(255))()
	samples := s.StageSnapshot()
	require.Len(t, samples, 1)
	require.Equal(t, 100, samples[0].Calls)
	samples[0].Calls = 0
	require.Equal(t, 100, s.StageSnapshot()[0].Calls)
}

func TestStageDurationsAndFirstUpstreamAreIndependent(t *testing.T) {
	start := time.Unix(100, 0)
	s := New(start)
	s.recordStage(StageAuthentication, 7*time.Millisecond)
	s.recordStage(StageAuthentication, 2*time.Millisecond)
	s.recordStage(StageAccountSlotWait, 25*time.Millisecond)
	s.recordStage(StageRouting, -time.Second)
	s.MarkUpstreamStarted(start.Add(50 * time.Millisecond))
	s.MarkUpstreamStarted(start.Add(time.Second))
	require.Equal(t, start.Add(50*time.Millisecond), s.FirstUpstreamAt())
	samples := s.StageSnapshot()
	require.Len(t, samples, 2)
	require.Equal(t, int64(9), samples[0].DurationMS)
	require.Equal(t, 2, samples[0].Calls)
	require.Equal(t, "account_slot_wait", samples[1].Stage)
	require.Equal(t, int64(25), samples[1].DurationMS)
	require.True(t, New(start).FirstUpstreamAt().IsZero())
}

func TestWithStageSuppressesNestedRoutingButKeepsSubstages(t *testing.T) {
	s := New(time.Now())
	original := remediationContext(s, true)
	ctx, finish := WithStage(original, StageRouting)
	child, finishChild := WithStage(ctx, StageRouting)
	require.Equal(t, ctx, child)
	StartStage(child, StageStickyLookup)()
	finishChild()
	finish()
	samples := s.StageSnapshot()
	require.Len(t, samples, 2)
	require.Equal(t, 1, samples[0].Calls)
	require.Equal(t, "routing", samples[0].Stage)
	require.Equal(t, "sticky_lookup", samples[1].Stage)
	disabled := remediationContext(s, false)
	same, stop := WithStage(disabled, StageRouting)
	require.Equal(t, disabled, same)
	stop()
	require.Equal(t, samples, s.StageSnapshot())
}

func TestObserveUpstreamStartRespectsGateAndFirstAttempt(t *testing.T) {
	start := time.Unix(200, 0)
	s := New(start)
	ObserveUpstreamStart(remediationContext(s, false), start.Add(time.Millisecond))
	require.True(t, s.FirstUpstreamAt().IsZero())
	ObserveUpstreamStart(remediationContext(s, true), start.Add(3*time.Millisecond))
	ObserveUpstreamStart(remediationContext(s, true), start.Add(time.Second))
	require.Equal(t, start.Add(3*time.Millisecond), s.FirstUpstreamAt())
	ObserveUpstreamStart(nil, start)
}
