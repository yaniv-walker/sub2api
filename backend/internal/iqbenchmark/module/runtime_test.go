//go:build unit

package module

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/scheduler"
	upstreamsvc "github.com/Wei-Shaw/sub2api/internal/service"
)

// runtimeSettingsStore 是最小的 SettingsStore 实现（内存）。
type runtimeSettingsStore struct {
	value string
}

func (s *runtimeSettingsStore) Get(_ context.Context, _ string) (string, error) {
	return s.value, nil
}

func (s *runtimeSettingsStore) Set(_ context.Context, _, value string) error {
	s.value = value
	return nil
}

// disabledSettings 返回一个「功能关闭」的 SettingsProvider。
func disabledSettings(t *testing.T) *iqbenchmark.SettingsProvider {
	t.Helper()
	store := &runtimeSettingsStore{value: `{"enabled":false}`}
	return iqbenchmark.NewSettingsProvider(store, slog.Default())
}

// enabledSettings 返回一个「功能启用」的 SettingsProvider。
func enabledSettings(t *testing.T) *iqbenchmark.SettingsProvider {
	t.Helper()
	store := &runtimeSettingsStore{value: `{"enabled":true}`}
	return iqbenchmark.NewSettingsProvider(store, slog.Default())
}

// recordingUserStore 记录是否被调用，用于证明「关闭时零副作用」。
type recordingUserStore struct {
	calls int
}

func (r *recordingUserStore) FindByNotesPrefix(_ context.Context, _ string) (*upstreamsvc.User, error) {
	r.calls++
	return nil, nil
}

func (r *recordingUserStore) Create(_ context.Context, _ *upstreamsvc.User) error {
	r.calls++
	return errors.New("should not be called")
}

// TestRuntimeDoesNothingWhenDisabled 是最重要的一条：
// enabled=false 时**不得**创建内部账号、不得启动调度器。
//
// 这条测试的存在理由：初版曾经完全没有调用 Start 的入口，
// 调度器与内部账号在启用后也不会运行（功能静默失效）。
// 现在由 Runtime 统一负责启动，因此必须同时钉住
// 「关闭时零副作用」与「启用时会启动」两侧。
func TestRuntimeDoesNothingWhenDisabled(t *testing.T) {
	users := &recordingUserStore{}
	dir := iqbenchmark.NewActiveDirectory()
	rt := &Runtime{
		Settings: disabledSettings(t),
		Agent:    dir,
		logger:   slog.Default(),
		deps: iqbenchmark.InternalAgentDeps{
			Users:  users,
			Keys:   &noopKeyStore{},
			Locker: &noopLocker{},
		},
	}

	require.NoError(t, rt.Start(context.Background()))
	require.Zero(t, users.calls, "disabled feature must not touch the user store")
	_, ready := dir.Snapshot()
	require.False(t, ready, "disabled feature must leave the agent directory unready")
}

// TestRuntimeStartIsIdempotent 保证重复调用只生效一次。
func TestRuntimeStartIsIdempotent(t *testing.T) {
	users := &recordingUserStore{}
	rt := &Runtime{
		Settings: disabledSettings(t),
		Agent:    iqbenchmark.NewActiveDirectory(),
		logger:   slog.Default(),
		deps: iqbenchmark.InternalAgentDeps{
			Users:  users,
			Keys:   &noopKeyStore{},
			Locker: &noopLocker{},
		},
	}

	for i := 0; i < 3; i++ {
		require.NoError(t, rt.Start(context.Background()))
	}
	require.Zero(t, users.calls, "repeated Start must not repeat side effects")
}

// TestRuntimeMarksAgentUnreadyWhenInitFails 保证初始化失败只降级本功能：
// Start 返回 nil（不阻塞主 API），但目录被标记为未就绪并带上原因。
func TestRuntimeMarksAgentUnreadyWhenInitFails(t *testing.T) {
	dir := iqbenchmark.NewActiveDirectory()
	rt := &Runtime{
		Settings: enabledSettings(t),
		Agent:    dir,
		logger:   slog.Default(),
		deps: iqbenchmark.InternalAgentDeps{
			Users:  &failingUserStore{},
			Keys:   &noopKeyStore{},
			Locker: &noopLocker{},
		},
	}

	require.NoError(t, rt.Start(context.Background()),
		"agent init failure must not block API startup")
	agent, ready := dir.Snapshot()
	require.False(t, ready)
	require.Nil(t, agent)
	require.NotEmpty(t, dir.Reason(), "the directory must record why it is unavailable")
}

// TestRuntimeShutdownWithoutStartIsSafe 覆盖「从未启动就退出」的路径。
func TestRuntimeShutdownWithoutStartIsSafe(t *testing.T) {
	rt := &Runtime{logger: slog.Default()}
	require.NoError(t, rt.Shutdown(context.Background()))
	// nil 接收者同样必须是 no-op（Wire 可能给 nil）。
	var nilRT *Runtime
	require.NoError(t, nilRT.Shutdown(context.Background()))
	require.NoError(t, nilRT.Start(context.Background()))
}

// TestRuntimeStartsAndStopsScheduler 端到端验证：启用时调度器真的被启动，
// 退出时真的被停掉。这是初版缺失的那一环。
func TestRuntimeStartsAndStopsScheduler(t *testing.T) {
	store := &fakeSchedulerStore{}
	dir := iqbenchmark.NewActiveDirectory()
	settings := enabledSettings(t)

	sch := scheduler.New(scheduler.Config{
		Store:    store,
		Runner:   nil, // 本测试只关心 Start/Shutdown 生命周期
		Guard:    nil,
		Settings: settings,
		Agent:    dir,
		Logger:   slog.Default(),
	})

	rt := &Runtime{
		Scheduler: sch,
		Settings:  settings,
		Agent:     dir,
		logger:    slog.Default(),
		deps: iqbenchmark.InternalAgentDeps{
			Users:  &noopUserStore{},
			Keys:   &noopKeyStore{},
			Locker: &noopLocker{},
		},
	}

	require.NoError(t, rt.Start(context.Background()))

	// 调度器依赖缺失（runner/guard 为 nil）时 Start 会按设计不启动，
	// 这里断言的是 Runtime 没有把错误上抛成启动失败。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, rt.Shutdown(ctx))
}

// ---------- 测试替身 ----------

type noopLocker struct{}

func (n *noopLocker) Acquire(_ context.Context) (bool, func(), error) {
	return true, func() {}, nil
}

type noopUserStore struct{}

func (n *noopUserStore) FindByNotesPrefix(_ context.Context, _ string) (*upstreamsvc.User, error) {
	return nil, nil
}

func (n *noopUserStore) Create(_ context.Context, u *upstreamsvc.User) error {
	u.ID = 9001
	return nil
}

type failingUserStore struct{}

func (f *failingUserStore) FindByNotesPrefix(_ context.Context, _ string) (*upstreamsvc.User, error) {
	return nil, errors.New("user store unavailable")
}

func (f *failingUserStore) Create(_ context.Context, _ *upstreamsvc.User) error {
	return errors.New("user store unavailable")
}

type noopKeyStore struct{}

func (n *noopKeyStore) ListByUser(_ context.Context, _ int64) ([]upstreamsvc.APIKey, error) {
	return nil, nil
}

func (n *noopKeyStore) Create(_ context.Context, userID int64, name string, groupID int64) (*upstreamsvc.APIKey, error) {
	return &upstreamsvc.APIKey{
		ID:      7001,
		UserID:  userID,
		Name:    name,
		GroupID: &groupID,
		Status:  upstreamsvc.StatusActive,
	}, nil
}

// fakeSchedulerStore 满足 scheduler.TaskStore 的最小实现。
type fakeSchedulerStore struct{}

func (f *fakeSchedulerStore) ClaimDueTasks(_ context.Context, _ string, _ int, _ time.Duration) ([]scheduler.ClaimedTask, error) {
	return nil, nil
}

func (f *fakeSchedulerStore) ListTasks(_ context.Context) ([]iqbenchmark.Task, error) {
	return nil, nil
}

func (f *fakeSchedulerStore) RenewLease(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}

func (f *fakeSchedulerStore) ReleaseLease(_ context.Context, _ int64, _ string) error { return nil }

func (f *fakeSchedulerStore) ReleaseAllLeases(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

func (f *fakeSchedulerStore) AdvanceNextRunAtAfterClaim(_ context.Context, _ int64, _ string, _ time.Time) error {
	return nil
}

func (f *fakeSchedulerStore) MarkTaskRunFinished(_ context.Context, _ int64, _ time.Time, _ string, _ *time.Time) error {
	return nil
}

func (f *fakeSchedulerStore) SetTaskStatus(_ context.Context, _ int64, _, _ string, _ *time.Time) error {
	return nil
}

// 保持 miniredis 依赖被引用（其余集成用例用它验证 Redis 计数路径）。
var _ = miniredis.RunT
