//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/stretchr/testify/require"
)

func TestValidateIntervalBounds(t *testing.T) {
	cases := []struct {
		name     string
		interval int
		jitter   int
		wantErr  error
	}{
		{"下界合法", 15, 0, nil},
		{"上界合法", 10080, 0, nil},
		{"低于下界", 14, 0, ErrIntervalTooSmall},
		{"高于上界", 10081, 0, ErrIntervalTooLarge},
		{"抖动为负", 30, -1, ErrJitterNegative},
		{"抖动把间隔压到 900 秒以下", 15, 1, ErrJitterTooLarge},
		{"抖动恰好不压破 900 秒", 15, 0, nil},
		{"长间隔可容纳大抖动", 60, 1800, nil},
		{"长间隔但抖动过大", 60, 2701, ErrJitterTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateInterval(tc.interval, tc.jitter)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestNextRunAtNeverBelowMinimumGap(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	// 即使抖动把基准时间推前，也不能早于 now + 900s。
	for i := 0; i < 200; i++ {
		next := nextRunAt(now, 15, 900)
		require.False(t, next.Before(now.Add(900*time.Second)),
			"下次执行时间不得早于 now+900s，实际 %v", next)
	}
}

func TestNextRunAtIsNearIntervalWithoutJitter(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	next := nextRunAt(now, 30, 0)
	require.Equal(t, now.Add(30*time.Minute), next)
}

func TestNextRunAtJitterStaysWithinBand(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	interval := 60
	jitter := 300
	base := now.Add(time.Duration(interval) * time.Minute)
	for i := 0; i < 500; i++ {
		next := nextRunAt(now, interval, jitter)
		require.False(t, next.Before(base.Add(-time.Duration(jitter)*time.Second)))
		require.False(t, next.After(base.Add(time.Duration(jitter)*time.Second)))
	}
}

// ---- 熔断守卫 ----

// fakeStore 只实现守卫用到的方法（GuardStore 已收窄为两个方法，
// 因此这里无需为无关方法造桩）。
type fakeStore struct {
	statusCalls []statusCall
	dailyCost   float64
	dailyErr    error
}

type statusCall struct {
	TaskID  int64
	Status  string
	Reason  string
	NextRun *time.Time
}

func (f *fakeStore) SetTaskStatus(_ context.Context, id int64, status, reason string, next *time.Time) error {
	f.statusCalls = append(f.statusCalls, statusCall{TaskID: id, Status: status, Reason: reason, NextRun: next})
	return nil
}

func (f *fakeStore) DailyCost(_ context.Context, _ int64, _ time.Time) (float64, error) {
	return f.dailyCost, f.dailyErr
}

func fixedGuard(t *testing.T, store GuardStore) *Guard {
	t.Helper()
	now := time.Date(2026, 11, 2, 10, 30, 0, 0, time.UTC)
	g := NewGuard(store, nil)
	g.Now = func() time.Time { return now }
	return g
}

func TestTripQuotaExhaustedClearsNextRun(t *testing.T) {
	store := &fakeStore{}
	g := fixedGuard(t, store)
	require.NoError(t, g.TripQuotaExhausted(context.Background(), 7, "upstream 429"))

	require.Len(t, store.statusCalls, 1)
	require.Equal(t, iqbenchmark.TaskStatusQuotaExhausted, store.statusCalls[0].Status)
	// 必须清空 next_run_at：否则到期扫描会反复捡起同一个失败任务。
	require.Nil(t, store.statusCalls[0].NextRun)
}

func TestRecoveryQuotaRewritesNextRun(t *testing.T) {
	store := &fakeStore{}
	g := fixedGuard(t, store)
	task := &iqbenchmark.Task{ID: 7, IntervalMinute: 30}
	require.NoError(t, g.RecoveryQuota(context.Background(), task))

	require.Len(t, store.statusCalls, 1)
	require.Equal(t, iqbenchmark.TaskStatusIdle, store.statusCalls[0].Status)
	require.NotNil(t, store.statusCalls[0].NextRun, "恢复后必须重新写入 next_run_at，否则永远不再调度")
	require.Equal(t, g.now().Add(30*time.Minute), *store.statusCalls[0].NextRun)
}

func TestTripBudgetExhaustedPausesAllTasksUntilNextDay(t *testing.T) {
	store := &fakeStore{}
	g := fixedGuard(t, store)
	tasks := []iqbenchmark.Task{{ID: 1}, {ID: 2}, {ID: 3}}
	require.NoError(t, g.TripBudgetExhausted(context.Background(), tasks, 10.5, 10))

	require.Len(t, store.statusCalls, 3, "预算熔断必须是全局的，覆盖所有任务")
	for _, call := range store.statusCalls {
		require.Equal(t, iqbenchmark.TaskStatusBudgetExhausted, call.Status)
		require.NotNil(t, call.NextRun)
		// 次日 0 点（本地时区）。
		require.Equal(t, time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC), *call.NextRun)
	}
}

func TestRecoverBudgetIfNewDayOnlyTouchesBudgetTasks(t *testing.T) {
	store := &fakeStore{}
	g := fixedGuard(t, store)
	tasks := []iqbenchmark.Task{
		{ID: 1, Status: iqbenchmark.TaskStatusBudgetExhausted, IntervalMinute: 30},
		{ID: 2, Status: iqbenchmark.TaskStatusAccountDisabled, IntervalMinute: 30},
		{ID: 3, Status: iqbenchmark.TaskStatusIdle, IntervalMinute: 30},
	}
	recovered, err := g.RecoverBudgetIfNewDay(context.Background(), tasks)
	require.NoError(t, err)
	require.Equal(t, 1, recovered, "只应恢复 budget_exhausted 的任务")
	require.Len(t, store.statusCalls, 1)
	require.Equal(t, int64(1), store.statusCalls[0].TaskID)
	require.Equal(t, iqbenchmark.TaskStatusIdle, store.statusCalls[0].Status)
}

func TestTripAccountDisabledRequiresManualRecovery(t *testing.T) {
	store := &fakeStore{}
	g := fixedGuard(t, store)
	tasks := []iqbenchmark.Task{{ID: 1}, {ID: 2}}
	require.NoError(t, g.TripAccountDisabled(context.Background(), tasks, "internal agent disabled by moderation"))

	require.Len(t, store.statusCalls, 2)
	for _, call := range store.statusCalls {
		require.Equal(t, iqbenchmark.TaskStatusAccountDisabled, call.Status)
		// 人工恢复前不写 next_run_at，避免被调度器重新捡起。
		require.Nil(t, call.NextRun)
	}
}

func TestRecoverFromAccountDisabledIsIdempotent(t *testing.T) {
	store := &fakeStore{}
	g := fixedGuard(t, store)

	// 非 account_disabled 状态不做任何写入。
	require.NoError(t, g.RecoverFromAccountDisabled(context.Background(), &iqbenchmark.Task{ID: 1, Status: iqbenchmark.TaskStatusIdle}))
	require.Empty(t, store.statusCalls)

	// account_disabled 才恢复。
	require.NoError(t, g.RecoverFromAccountDisabled(context.Background(), &iqbenchmark.Task{
		ID: 2, Status: iqbenchmark.TaskStatusAccountDisabled, IntervalMinute: 30,
	}))
	require.Len(t, store.statusCalls, 1)
	require.Equal(t, iqbenchmark.TaskStatusIdle, store.statusCalls[0].Status)
	require.NotNil(t, store.statusCalls[0].NextRun)
}

func TestCheckBudgetSkipsQueryWhenUnlimited(t *testing.T) {
	store := &fakeStore{dailyErr: errShouldNotBeQueried{}}
	g := fixedGuard(t, store)
	exhausted, spent, err := g.CheckBudget(context.Background(), 99, 0, g.now())
	require.NoError(t, err, "预算为 0（不限制）时不应查询数据库")
	require.False(t, exhausted)
	require.Zero(t, spent)
}

type errShouldNotBeQueried struct{}

func (errShouldNotBeQueried) Error() string { return "DailyCost must not be queried" }

func TestCheckBudgetDetectsExhaustion(t *testing.T) {
	store := &fakeStore{dailyCost: 12.5}
	g := fixedGuard(t, store)
	exhausted, spent, err := g.CheckBudget(context.Background(), 99, 10, g.now())
	require.NoError(t, err)
	require.True(t, exhausted)
	require.InDelta(t, 12.5, spent, 1e-9)
}

func TestCheckBudgetAllowsBelowBudget(t *testing.T) {
	store := &fakeStore{dailyCost: 3.5}
	g := fixedGuard(t, store)
	exhausted, _, err := g.CheckBudget(context.Background(), 99, 10, g.now())
	require.NoError(t, err)
	require.False(t, exhausted)
}

// 查询失败必须报错而不是「当作未超限」：静默放行会让成本无界增长。
func TestCheckBudgetPropagatesQueryError(t *testing.T) {
	store := &fakeStore{dailyErr: errShouldNotBeQueried{}}
	g := fixedGuard(t, store)
	_, _, err := g.CheckBudget(context.Background(), 99, 10, g.now())
	require.Error(t, err)
	require.Contains(t, err.Error(), "query daily cost")
}

func TestDayStartUsesLocalMidnight(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, 11, 2, 23, 59, 0, 0, loc)
	start := dayStart(now)
	require.Equal(t, time.Date(2026, 11, 2, 0, 0, 0, 0, loc), start)
	require.Equal(t, 0, start.Hour())
	require.Equal(t, loc, start.Location())
}
