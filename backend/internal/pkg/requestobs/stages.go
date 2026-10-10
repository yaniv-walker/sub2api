package requestobs

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// Stage is an allowlisted operation boundary, not an arbitrary label.
// Durations may overlap (routing includes sticky/profit checks); do not sum them.
type Stage uint8

const (
	StageAuthentication Stage = iota
	StageAuthEligibility
	StageBodyDecode
	StageJSONParse
	StageJSONValidation
	StageSecurityAudit
	StageBillingEligibility
	StageRouting
	StageCompositeRouting
	StageStickyLookup
	StageProfitGate
	StageUserSlotAcquire
	StageAccountSlotAcquire
	StageUserSlotWait
	StageAccountSlotWait
	StageMessageQueue
	StageMessageThrottle
	stageCount
)

var stageNames = [stageCount]string{
	"authentication", "auth_eligibility", "body_read_decode", "json_parse",
	"json_validation", "security_audit", "billing_eligibility", "routing", "composite_routing", "sticky_lookup",
	"profit_gate", "user_slot_acquire", "account_slot_acquire",
	"user_slot_wait", "account_slot_wait", "message_queue", "message_throttle",
}

type stageTiming struct {
	calls    int
	duration time.Duration
}

// StageSample omits unobserved boundaries. Zero milliseconds can mean a
// completed operation shorter than one millisecond; Calls disambiguates it.
type StageSample struct {
	Stage      string `json:"stage"`
	Calls      int    `json:"calls"`
	DurationMS int64  `json:"duration_ms"`
}

// StartStage is a passive aspect: it neither changes errors nor adds deadlines.
// Only the unified, immutable request snapshot authorizes new instrumentation.
func StartStage(ctx context.Context, stage Stage) func() {
	if ctx == nil || stage >= stageCount {
		return noopStage
	}
	enabled, _ := ctx.Value(ctxkey.UpstreamRetryGuardrailsEnabled).(bool)
	state := FromContext(ctx)
	if !enabled || state == nil {
		return noopStage
	}
	start := time.Now()
	var once sync.Once
	return func() { once.Do(func() { state.recordStage(stage, time.Since(start)) }) }
}

func noopStage() {}

func (s *State) recordStage(stage Stage, duration time.Duration) {
	if s == nil || stage >= stageCount || duration < 0 {
		return
	}
	s.mu.Lock()
	s.stages[stage].calls++
	s.stages[stage].duration += duration
	s.mu.Unlock()
}

func (s *State) StageSnapshot() []StageSample {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var samples []StageSample
	for stage, timing := range s.stages {
		if timing.calls > 0 {
			samples = append(samples, StageSample{Stage: stageNames[stage], Calls: timing.calls, DurationMS: timing.duration.Milliseconds()})
		}
	}
	return samples
}

// MarkUpstreamStarted records the first HTTP attempt entry, not a completed
// response or model first token. No attempt leaves the timestamp absent.
func (s *State) MarkUpstreamStarted(now time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.firstUpstreamAt.IsZero() {
		s.firstUpstreamAt = now
	}
	s.mu.Unlock()
}

func (s *State) FirstUpstreamAt() time.Time {
	if s == nil {
		return time.Time{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.firstUpstreamAt
}

type activeStagesKey struct{}

// WithStage suppresses recursive instrumentation of the same boundary, e.g.
// the OpenAI scheduler falling back to legacy selection. Different sub-stages
// remain visible. Disabled requests retain their original context.
func WithStage(ctx context.Context, stage Stage) (context.Context, func()) {
	if ctx == nil || stage >= stageCount {
		return ctx, noopStage
	}
	enabled, _ := ctx.Value(ctxkey.UpstreamRetryGuardrailsEnabled).(bool)
	if !enabled || FromContext(ctx) == nil {
		return ctx, noopStage
	}
	active, _ := ctx.Value(activeStagesKey{}).(uint32)
	bit := uint32(1) << stage
	if active&bit != 0 {
		return ctx, noopStage
	}
	return context.WithValue(ctx, activeStagesKey{}, active|bit), StartStage(ctx, stage)
}

// ObserveUpstreamStart follows the same gate as the phase decorators.
func ObserveUpstreamStart(ctx context.Context, now time.Time) {
	if ctx == nil {
		return
	}
	enabled, _ := ctx.Value(ctxkey.UpstreamRetryGuardrailsEnabled).(bool)
	if enabled {
		FromContext(ctx).MarkUpstreamStarted(now)
	}
}
