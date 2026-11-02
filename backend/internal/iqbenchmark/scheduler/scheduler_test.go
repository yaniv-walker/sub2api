//go:build unit

package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- 测试替身 ----

type fakeTaskStore struct {
	mu sync.Mutex

	claimCalls  []string
	claimResult []ClaimedTask
	claimErr    error
	released    []int64
	releasedAll int
	advanced    map[int64]time.Time
	renewErr    error
	finished    map[int64]string
	statusCalls []statusCall
	listTasks   []iqbenchmark.Task
	advanceErr  error
	releaseErr  error
}

type statusCall struct {
	ID     int64
	Status string
	Reason string
	Next   *time.Time
}

func (f *fakeTaskStore) ClaimDueTasks(_ context.Context, workerID string, _ int, _ time.Duration) ([]ClaimedTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimCalls = append(f.claimCalls, workerID)
	return f.claimResult, f.claimErr
}

func (f *fakeTaskStore) RenewLease(_ context.Context, taskID int64, _ string, _ time.Duration) error {
	return f.renewErr
}

func (f *fakeTaskStore) ReleaseLease(_ context.Context, taskID int64, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, taskID)
	return f.releaseErr
}

func (f *fakeTaskStore) ReleaseAllLeases(_ context.Context, _ string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(f.releasedAll), nil
}

func (f *fakeTaskStore) AdvanceNextRunAtAfterClaim(_ context.Context, taskID int64, _ string, next time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.advanceErr != nil {
		return f.advanceErr
	}
	if f.advanced == nil {
		f.advanced = map[int64]time.Time{}
	}
	f.advanced[taskID] = next
	return nil
}

func (f *fakeTaskStore) MarkTaskRunFinished(_ context.Context, taskID int64, _ time.Time, status string, _ *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.finished == nil {
		f.finished = map[int64]string{}
	}
	f.finished[taskID] = status
	return nil
}

func (f *fakeTaskStore) SetTaskStatus(_ context.Context, id int64, status, reason string, next *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusCalls = append(f.statusCalls, statusCall{ID: id, Status: status, Reason: reason, Next: next})
	return nil
}

func (f *fakeTaskStore) ListTasks(_ context.Context) ([]iqbenchmark.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listTasks, nil
}

// guardStore 是 Guard 需要的最小能力（SetTaskStatus + DailyCost）。
// 与 fakeTaskStore 分开，避免为了满足宽接口而堆无用方法。
type guardStore struct {
	mu          sync.Mutex
	statusCalls []statusCall
}

func (g *guardStore) SetTaskStatus(_ context.Context, id int64, status, reason string, next *time.Time) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.statusCalls = append(g.statusCalls, statusCall{ID: id, Status: status, Reason: reason, Next: next})
	return nil
}

func (g *guardStore) DailyCost(_ context.Context, _ int64, _ time.Time) (float64, error) {
	return 0, nil
}

type fakeSettings struct {
	settings iqbenchmark.Settings
}

func (f *fakeSettings) Get(_ context.Context) iqbenchmark.Settings { return f.settings }

type fakeAgent struct {
	agent *iqbenchmark.InternalAgent
	ready bool
}

func (f *fakeAgent) Snapshot() (*iqbenchmark.InternalAgent, bool) { return f.agent, f.ready }

type fakeCleaner struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeCleaner) Cleanup(_ context.Context, _ time.Time) (service.CleanupStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return service.CleanupStats{}, nil
}

// ---- 测试 ----

// 构造函数绝不能启动 goroutine（硬性要求：必须有 Start/Shutdown 生命周期）。
func TestNewDoesNotStartGoroutine(t *testing.T) {
	s := New(Config{Store: &fakeTaskStore{}, Settings: &fakeSettings{}})
	require.NotNil(t, s)
	s.mu.Lock()
	require.False(t, s.started, "构造函数不得把 started 置为 true")
	require.Nil(t, s.stopCh, "构造函数不得创建 stopCh（即不得启动循环）")
	s.mu.Unlock()
}

// enabled=false 时 Start 必须零副作用。
func TestStartIsNoOpWhenDisabled(t *testing.T) {
	settings := &fakeSettings{settings: iqbenchmark.DefaultSettings()} // Enabled=false
	require.False(t, settings.settings.Enabled)

	s := New(Config{
		Store:    &fakeTaskStore{},
		Settings: settings,
		Runner:   &service.RunService{},
		Guard:    service.NewGuard(&guardStore{}, nil),
	})
	require.NoError(t, s.Start(context.Background()))

	s.mu.Lock()
	require.False(t, s.started, "禁用时不得启动")
	require.Nil(t, s.stopCh)
	s.mu.Unlock()
}

func TestStartDoesNotStartTwice(t *testing.T) {
	store := &fakeTaskStore{}
	settings := &fakeSettings{settings: enabledSettings()}
	s := New(Config{
		Store:    store,
		Settings: settings,
		Runner:   service.NewRunService(nil, nil, nil, nil, nil, nil),
		Guard:    service.NewGuard(&guardStore{}, nil),
		Agent:    &fakeAgent{ready: false},
	})
	require.NoError(t, s.Start(context.Background()))
	require.NoError(t, s.Start(context.Background()), "重复 Start 必须是 no-op 而不是报错")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, s.Shutdown(ctx))
}

func TestShutdownBeforeStartIsSafe(t *testing.T) {
	s := New(Config{})
	require.NoError(t, s.Shutdown(context.Background()))
}

// Shutdown 必须释放本实例全部租约：否则其他实例要等租约自然过期。
func TestShutdownReleasesAllLeases(t *testing.T) {
	store := &fakeTaskStore{releasedAll: 3}
	settings := &fakeSettings{settings: enabledSettings()}
	s := New(Config{
		Store:    store,
		Settings: settings,
		Runner:   service.NewRunService(nil, nil, nil, nil, nil, nil),
		Guard:    service.NewGuard(&guardStore{}, nil),
		Agent:    &fakeAgent{ready: false},
	})
	require.NoError(t, s.Start(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, s.Shutdown(ctx))

	store.mu.Lock()
	require.Equal(t, 3, store.releasedAll)
	store.mu.Unlock()
}

// 账号不可用：不领取任务，直接全局熔断。
func TestTickTripsAccountDisabledWithoutClaiming(t *testing.T) {
	store := &fakeTaskStore{
		listTasks: []iqbenchmark.Task{
			{ID: 1, Status: iqbenchmark.TaskStatusIdle},
			{ID: 2, Status: iqbenchmark.TaskStatusIdle},
		},
	}
	guard := &guardStore{}
	settings := &fakeSettings{settings: enabledSettings()}
	s := New(Config{
		Store:    store,
		Settings: settings,
		Runner:   service.NewRunService(nil, nil, nil, nil, nil, nil),
		Guard:    service.NewGuard(guard, nil),
		Agent:    &fakeAgent{ready: false},
	})
	s.tick()

	store.mu.Lock()
	require.Empty(t, store.claimCalls, "账号不可用时不得领取任务")
	store.mu.Unlock()

	guard.mu.Lock()
	defer guard.mu.Unlock()
	require.Len(t, guard.statusCalls, 2)
	for _, call := range guard.statusCalls {
		require.Equal(t, iqbenchmark.TaskStatusAccountDisabled, call.Status)
		require.Nil(t, call.Next, "人工恢复前不得写 next_run_at")
	}
}

// 已处于 account_disabled 的任务不应被重复刷状态（避免每个 tick 都写库）。
func TestTickDoesNotRepeatAccountDisabledTrip(t *testing.T) {
	store := &fakeTaskStore{
		listTasks: []iqbenchmark.Task{
			{ID: 1, Status: iqbenchmark.TaskStatusAccountDisabled},
			{ID: 2, Status: iqbenchmark.TaskStatusAccountDisabled},
		},
	}
	guard := &guardStore{}
	s := New(Config{
		Store:    store,
		Settings: &fakeSettings{settings: enabledSettings()},
		Runner:   service.NewRunService(nil, nil, nil, nil, nil, nil),
		Guard:    service.NewGuard(guard, nil),
		Agent:    &fakeAgent{ready: false},
	})
	s.tick()

	guard.mu.Lock()
	defer guard.mu.Unlock()
	require.Empty(t, guard.statusCalls, "已经是熔断态的任务不应重复写状态")
}

// 功能被关闭后，运行中的 tick 不再领取新任务。
func TestTickStopsClaimingWhenDisabledAtRuntime(t *testing.T) {
	store := &fakeTaskStore{}
	s := New(Config{
		Store:    store,
		Settings: &fakeSettings{settings: iqbenchmark.DefaultSettings()},
		Runner:   service.NewRunService(nil, nil, nil, nil, nil, nil),
		Guard:    service.NewGuard(&guardStore{}, nil),
		Agent:    &fakeAgent{ready: true},
	})
	s.tick()

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Empty(t, store.claimCalls)
}

// 租约已丢失时不得继续执行（不写产出、不改任务）。
func TestExecuteTaskAbortsWhenLeaseLostBeforeRun(t *testing.T) {
	store := &fakeTaskStore{advanceErr: iqbenchmark.ErrLeaseLost}
	s := New(Config{
		Store:    store,
		Settings: &fakeSettings{settings: enabledSettings()},
		Runner:   service.NewRunService(nil, nil, nil, nil, nil, nil),
		Guard:    service.NewGuard(&guardStore{}, nil),
	})
	s.executeTask(ClaimedTask{Task: iqbenchmark.Task{ID: 7, CaseIDs: []int64{1}}}, nil)

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Empty(t, store.finished, "租约丢失时不得标记任务完成")
}

// 领取后必须立刻推进 next_run_at（防崩溃后热点重试）。
func TestExecuteTaskAdvancesNextRunBeforeRun(t *testing.T) {
	store := &fakeTaskStore{}
	s := New(Config{
		Store:    store,
		Settings: &fakeSettings{settings: enabledSettings()},
		Runner:   service.NewRunService(nil, nil, nil, nil, nil, nil),
		Guard:    service.NewGuard(&guardStore{}, nil),
	})
	// Runner 依赖缺失会返回错误，但 next_run_at 必须已被推进。
	s.executeTask(ClaimedTask{Task: iqbenchmark.Task{ID: 7, CaseIDs: []int64{1}, IntervalMinute: 30}}, nil)

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Contains(t, store.advanced, int64(7), "必须先推进 next_run_at 再执行")
}

// nextRunFor：抖动不得把间隔压到 900 秒以下。
func TestNextRunForEnforcesMinimumGap(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	task := iqbenchmark.Task{IntervalMinute: 15, JitterSeconds: 900}
	for i := 0; i < 300; i++ {
		next := nextRunFor(task, now)
		require.False(t, next.Before(now.Add(900*time.Second)),
			"下次执行时间不得早于 now+900s，实际 %v", next)
	}
}

func TestNextRunForUsesDefaultIntervalWhenZero(t *testing.T) {
	now := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	next := nextRunFor(iqbenchmark.Task{IntervalMinute: 0}, now)
	require.Equal(t, now.Add(15*time.Minute), next)
}

func TestMaxTaskRuntimeScalesWithCaseCount(t *testing.T) {
	one := maxTaskRuntime(iqbenchmark.Task{CaseIDs: []int64{1}})
	three := maxTaskRuntime(iqbenchmark.Task{CaseIDs: []int64{1, 2, 3}})
	require.True(t, three > one, "题目越多允许的运行时间越长")
	require.True(t, maxTaskRuntime(iqbenchmark.Task{}) > 0, "无题目时也必须给出正的上限")
}

// 清理必须按间隔节流，不能每个 tick 都跑。
func TestCleanupIsThrottledByInterval(t *testing.T) {
	cleaner := &fakeCleaner{}
	s := New(Config{Settings: &fakeSettings{}, Cleaner: cleaner})
	settings := enabledSettings()
	settings.CleanupIntervalMinutes = 60
	ctx := context.Background()

	s.runCleanupIfDue(ctx, settings)
	s.runCleanupIfDue(ctx, settings)
	s.runCleanupIfDue(ctx, settings)

	cleaner.mu.Lock()
	defer cleaner.mu.Unlock()
	require.Equal(t, 1, cleaner.calls, "同一间隔内只应清理一次")
}

func TestCleanupSkippedWhenNoCleaner(t *testing.T) {
	s := New(Config{Settings: &fakeSettings{}})
	// 不应 panic。
	s.runCleanupIfDue(context.Background(), enabledSettings())
}

// ---- 目标失效熔断 ----

// fakeTargetValidator 是 TargetValidator 的替身。
type fakeTargetValidator struct {
	err   error
	calls int
}

func (f *fakeTargetValidator) CheckTaskTarget(_ context.Context, _ *iqbenchmark.Task) error {
	f.calls++
	return f.err
}

// TestExecuteTaskTripsInvalidTarget 断言目标失效时：熔断、释放租约、**不发起模型调用**。
//
// 「不发起调用」是关键：目标失效属于配置问题，占用一次真实模型调用
// 既浪费配额又污染 usage 统计。
func TestExecuteTaskTripsInvalidTarget(t *testing.T) {
	store := &fakeTaskStore{}
	guard := &guardStore{}
	tv := &fakeTargetValidator{
		err: fmt.Errorf("%w (group_id=3)", iqbenchmark.ErrTargetModelUnavailable),
	}
	// Runner 依赖全部为 nil：如果校验没有拦住，RunTask 会走到 nil 解引用或错误分支，
	// 从而在 store.finished 或 guard.statusCalls 上留下痕迹。
	s := New(Config{
		Store:    store,
		Settings: &fakeSettings{settings: enabledSettings()},
		Runner:   service.NewRunService(nil, nil, nil, nil, nil, nil),
		Guard:    service.NewGuard(guard, nil),
		Targets:  tv,
	})
	s.executeTask(ClaimedTask{Task: iqbenchmark.Task{ID: 11, GroupID: 3, Model: "m", CaseIDs: []int64{1}}}, nil)

	require.Equal(t, 1, tv.calls, "必须调用目标校验")

	guard.mu.Lock()
	defer guard.mu.Unlock()
	require.Len(t, guard.statusCalls, 1, "目标失效必须写一次任务状态")
	assert.Equal(t, iqbenchmark.TaskStatusInvalidTarget, guard.statusCalls[0].Status)
	assert.Nil(t, guard.statusCalls[0].Next, "人工恢复前不得写 next_run_at")

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Empty(t, store.finished, "不得把任务标记为跑完")
}

// TestExecuteTaskProceedsOnTransientTargetError 断言查询类错误（非目标失效哨兵）
// 不会被误判成配置失效 —— 否则一次数据库抖动就会把任务永久挂起。
func TestExecuteTaskProceedsOnTransientTargetError(t *testing.T) {
	store := &fakeTaskStore{}
	guard := &guardStore{}
	tv := &fakeTargetValidator{err: errors.New("dial tcp: connection refused")}
	s := New(Config{
		Store:    store,
		Settings: &fakeSettings{settings: enabledSettings()},
		Runner:   service.NewRunService(nil, nil, nil, nil, nil, nil),
		Guard:    service.NewGuard(guard, nil),
		Targets:  tv,
	})
	s.executeTask(ClaimedTask{Task: iqbenchmark.Task{ID: 12, GroupID: 3, Model: "m", CaseIDs: []int64{1}}}, nil)

	require.Equal(t, 1, tv.calls)
	guard.mu.Lock()
	defer guard.mu.Unlock()
	for _, call := range guard.statusCalls {
		assert.NotEqual(t, iqbenchmark.TaskStatusInvalidTarget, call.Status,
			"暂时性故障不得触发 invalid_target 熔断")
	}
}

// TestExecuteTaskSkipsTargetCheckWhenValidatorAbsent 断言未注入校验器时不 panic 且照常执行。
func TestExecuteTaskSkipsTargetCheckWhenValidatorAbsent(t *testing.T) {
	store := &fakeTaskStore{}
	s := New(Config{
		Store:    store,
		Settings: &fakeSettings{settings: enabledSettings()},
		Runner:   service.NewRunService(nil, nil, nil, nil, nil, nil),
		Guard:    service.NewGuard(&guardStore{}, nil),
	})
	s.executeTask(ClaimedTask{Task: iqbenchmark.Task{ID: 13, CaseIDs: []int64{1}, IntervalMinute: 30}}, nil)

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Contains(t, store.advanced, int64(13), "无校验器时应照常推进并执行")
}

// ---- 辅助 ----

func enabledSettings() iqbenchmark.Settings {
	s := iqbenchmark.DefaultSettings()
	s.Enabled = true
	s.MaxTaskConcurrency = 4
	return s
}

var _ = errors.Is
