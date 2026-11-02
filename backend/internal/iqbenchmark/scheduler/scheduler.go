// Package scheduler 实现检测任务的定期调度。
//
// 生命周期约定（与既有 ChannelMonitorRunner 一致）：
//   - 构造函数**不**启动任何 goroutine；
//   - Start(ctx) 才启动，Shutdown(ctx) 有界停止；
//   - 多实例以数据库租约去重（FOR UPDATE SKIP LOCKED + lease_expires_at）；
//   - enabled=false 时 Start 直接返回，零副作用（无 goroutine、无锁、无出站）。
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/repository"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/service"
)

// 调度参数（与 design.md §3.9 一致）。
const (
	// defaultTickInterval 是扫描到期任务的间隔。
	defaultTickInterval = 30 * time.Second
	// defaultLease 是任务租约时长；续期间隔为其 1/3。
	defaultLease = 90 * time.Second
	// leaseRenewInterval 是续期频率（90s / 3 = 30s）。
	leaseRenewInterval = 30 * time.Second
	// shutdownGrace 是 Shutdown 等待在途执行收敛的上限。
	shutdownGrace = 30 * time.Second
)

// TaskStore 是调度器需要的任务读写能力。
type TaskStore interface {
	ClaimDueTasks(ctx context.Context, workerID string, limit int, lease time.Duration) ([]repository.ClaimedTask, error)
	RenewLease(ctx context.Context, taskID int64, workerID string, lease time.Duration) error
	ReleaseLease(ctx context.Context, taskID int64, workerID string) error
	ReleaseAllLeases(ctx context.Context, workerID string) (int64, error)
	AdvanceNextRunAtAfterClaim(ctx context.Context, taskID int64, workerID string, next time.Time) error
	MarkTaskRunFinished(ctx context.Context, taskID int64, finishedAt time.Time, status string, nextRunAt *time.Time) error
	SetTaskStatus(ctx context.Context, id int64, status, reason string, nextRunAt *time.Time) error
	ListTasks(ctx context.Context) ([]iqbenchmark.Task, error)
}

// ClaimedTask 是领取结果（复用仓储层的定义，避免两套同构类型互相转换）。
type ClaimedTask = repository.ClaimedTask

// AgentProvider 提供内部账号快照（由 iqbenchmark 主包实现）。
type AgentProvider interface {
	// Snapshot 返回当前内部账号与就绪状态。
	Snapshot() (*iqbenchmark.InternalAgent, bool)
}

// SettingsProvider 是配置读取的窄接口。
type SettingsProvider interface {
	Get(ctx context.Context) iqbenchmark.Settings
}

// Scheduler 是检测任务的调度器。
type Scheduler struct {
	store    TaskStore
	runner   *service.RunService
	guard    *service.Guard
	settings SettingsProvider
	agent    AgentProvider
	cleaner  Cleaner
	logger   *slog.Logger

	workerID string

	mu      sync.Mutex
	started bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	// lastCleanupAt 记录上次清理时间（按 cleanup_interval_minutes 节流）。
	lastCleanupAt time.Time

	// inflight 记录在途任务，Shutdown 时用于等待收敛。
	inflight sync.WaitGroup
	// inflightTaskIDs 用于避免同一任务在同一进程内被重复执行。
	inflightTaskIDs sync.Map
}

// Cleaner 是清理任务的窄接口（由 ArtifactService 实现）。
type Cleaner interface {
	Cleanup(ctx context.Context, now time.Time) (service.CleanupStats, error)
}

// Config 是调度器构造参数。
type Config struct {
	Store    TaskStore
	Runner   *service.RunService
	Guard    *service.Guard
	Settings SettingsProvider
	Agent    AgentProvider
	Cleaner  Cleaner
	// WorkerID 是租约持有者标识；为空时自动生成（<hostname>-<rand>）。
	WorkerID string
	Logger   *slog.Logger
}

// New 构造调度器。**不启动任何 goroutine**（必须显式调用 Start）。
func New(cfg Config) *Scheduler {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	workerID := cfg.WorkerID
	if workerID == "" {
		workerID = fmt.Sprintf("iq-%d-%d", time.Now().UnixNano()%1e6, rand.Int63()%1e6)
	}
	return &Scheduler{
		store:    cfg.Store,
		runner:   cfg.Runner,
		guard:    cfg.Guard,
		settings: cfg.Settings,
		agent:    cfg.Agent,
		cleaner:  cfg.Cleaner,
		logger:   logger,
		workerID: workerID,
	}
}

// WorkerID 返回本实例的租约标识（供测试与日志使用）。
func (s *Scheduler) WorkerID() string { return s.workerID }

// Start 启动调度循环。
//
// 三重前置条件（任一不满足即直接返回，不启动 goroutine）：
//  1. 功能未启用（settings.Enabled == false）；
//  2. 依赖缺失（store/runner/guard/settings）；
//  3. 已经在运行。
//
// 返回 nil 表示「已启动」或「按设计未启动」，两种情况都不应被视为错误。
func (s *Scheduler) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if s.store == nil || s.runner == nil || s.guard == nil || s.settings == nil {
		s.logger.Warn("iqbenchmark: scheduler not started, dependencies missing")
		return nil
	}
	if !s.settings.Get(ctx).Enabled {
		s.logger.Info("iqbenchmark: scheduler not started, feature is disabled")
		return nil
	}

	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.mu.Unlock()

	go s.loop()
	s.logger.Info("iqbenchmark: scheduler started", "worker_id", s.workerID)
	return nil
}

// Shutdown 有界停止调度器。
//
// 顺序：
//  1. 关闭 stopCh 通知循环退出；
//  2. 等待循环真正退出（受 grace 约束）；
//  3. 等待在途任务收敛（受 grace 约束）；
//  4. 释放本实例持有的全部租约（否则其他实例要等到租约过期才能接管）。
func (s *Scheduler) Shutdown(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	stopCh := s.stopCh
	doneCh := s.doneCh
	s.started = false
	s.mu.Unlock()

	close(stopCh)

	grace := shutdownGrace
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < grace {
			grace = remaining
		}
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()

	select {
	case <-doneCh:
	case <-timer.C:
		s.logger.Warn("iqbenchmark: scheduler loop did not stop within grace period",
			"grace", grace.String())
	}

	// 等在设计途任务；超时则放弃（租约会自然过期，由其他实例接管）。
	inflightDone := make(chan struct{})
	go func() {
		s.inflight.Wait()
		close(inflightDone)
	}()
	select {
	case <-inflightDone:
	case <-timer.C:
		s.logger.Warn("iqbenchmark: in-flight tasks did not finish within grace period")
	}

	// 释放租约：用独立超时，避免调用方 ctx 已取消导致租约泄漏。
	relCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	released, err := s.store.ReleaseAllLeases(relCtx, s.workerID)
	if err != nil {
		s.logger.Warn("iqbenchmark: release leases on shutdown failed", "error", err)
		return nil
	}
	if released > 0 {
		s.logger.Info("iqbenchmark: released task leases on shutdown", "count", released)
	}
	return nil
}

// loop 是主循环。
func (s *Scheduler) loop() {
	defer close(s.doneCh)

	ticker := time.NewTicker(defaultTickInterval)
	defer ticker.Stop()

	// 启动时立即跑一次，避免重启后要等一个 tick 才开始。
	s.tick()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

// tick 执行一次「扫描到期任务 → 领取 → 并发执行」。
func (s *Scheduler) tick() {
	// 每个 tick 用独立 ctx：单个 tick 不应超过租约时长。
	ctx, cancel := context.WithTimeout(context.Background(), defaultLease)
	defer cancel()

	settings := s.settings.Get(ctx)
	if !settings.Enabled {
		// 运行中被关闭：不再领取新任务（在途任务会自然结束）。
		return
	}

	// 账号熔断前置检查：不可用时整轮不执行，避免每个任务各失败一次。
	agent, ready := s.agentSnapshot()
	if !ready || !agent.Active() {
		s.tripAccountDisabled(ctx, "internal agent is not ready or not active")
		return
	}

	// 预算熔断前置检查：超预算则不领取任何任务。
	if exhausted, spent, err := s.guard.CheckBudget(ctx, agent.KeyID(), settings.DailyCostBudget, time.Now()); err != nil {
		s.logger.Error("iqbenchmark: budget check failed, skipping this tick", "error", err)
		return
	} else if exhausted {
		tasks, listErr := s.store.ListTasks(ctx)
		if listErr != nil {
			s.logger.Error("iqbenchmark: list tasks for budget trip failed", "error", listErr)
			return
		}
		if err := s.guard.TripBudgetExhausted(ctx, tasks, spent, settings.DailyCostBudget); err != nil {
			s.logger.Error("iqbenchmark: trip budget exhausted failed", "error", err)
		}
		return
	}

	// 跨日自动恢复预算熔断的任务。
	s.recoverBudgetIfNewDay(ctx)

	limit := settings.MaxTaskConcurrency
	if limit <= 0 {
		limit = 4
	}
	claimed, err := s.store.ClaimDueTasks(ctx, s.workerID, limit, defaultLease)
	if err != nil {
		s.logger.Error("iqbenchmark: claim due tasks failed", "error", err)
		return
	}
	if len(claimed) == 0 {
		s.runCleanupIfDue(ctx, settings)
		return
	}

	for i := range claimed {
		item := claimed[i]
		// 进程内去重：同一任务不应有两个 goroutine 同时跑。
		if _, loaded := s.inflightTaskIDs.LoadOrStore(item.Task.ID, struct{}{}); loaded {
			_ = s.store.ReleaseLease(ctx, item.Task.ID, s.workerID)
			continue
		}
		// 跨任务并发上限：满员时释放租约，下个 tick 再抢。
		if !s.runner.AcquireSlot() {
			s.inflightTaskIDs.Delete(item.Task.ID)
			_ = s.store.ReleaseLease(ctx, item.Task.ID, s.workerID)
			continue
		}

		s.inflight.Add(1)
		go func(claimed ClaimedTask) {
			defer s.inflight.Done()
			defer s.runner.ReleaseSlot()
			defer s.inflightTaskIDs.Delete(claimed.Task.ID)
			s.executeTask(claimed, agent)
		}(item)
	}
}

// executeTask 在租约保护下执行一个任务。
func (s *Scheduler) executeTask(claimed ClaimedTask, agent *iqbenchmark.InternalAgent) {
	task := claimed.Task
	ctx, cancel := context.WithTimeout(context.Background(), maxTaskRuntime(task))
	defer cancel()

	// 领取成功后立刻推进 next_run_at：即使本轮崩溃，也不会在同一时刻反复重试。
	next := nextRunFor(task, time.Now())
	if err := s.store.AdvanceNextRunAtAfterClaim(ctx, task.ID, s.workerID, next); err != nil {
		if errors.Is(err, iqbenchmark.ErrLeaseLost) {
			s.logger.Warn("iqbenchmark: lease lost before execution", "task_id", task.ID)
			return
		}
		s.logger.Error("iqbenchmark: advance next_run_at failed", "task_id", task.ID, "error", err)
		return
	}

	// 启动续期循环；丢失租约立即中止本轮（不写任何产出）。
	renewCtx, cancelRenew := context.WithCancel(ctx)
	defer cancelRenew()
	leaseLost := make(chan struct{})
	go s.renewLoop(renewCtx, task.ID, leaseLost)

	run, err := s.runner.RunTask(ctx, service.RunTaskInput{
		Task:    &task,
		Agent:   agent,
		Trigger: iqbenchmark.RunTriggerSchedule,
	})

	select {
	case <-leaseLost:
		s.logger.Warn("iqbenchmark: lease lost during execution, result discarded", "task_id", task.ID)
		return
	default:
	}

	if err != nil {
		s.handleRunError(ctx, task, err, next)
		return
	}

	// 正常结束：记录 last_run_at 与状态，释放租约。
	finishStatus := iqbenchmark.TaskStatusIdle
	if status := s.applyCounters(task, run); status != "" {
		finishStatus = status
	}
	if err := s.store.MarkTaskRunFinished(ctx, task.ID, time.Now(), finishStatus, &next); err != nil {
		s.logger.Error("iqbenchmark: mark task run finished failed", "task_id", task.ID, "error", err)
	}
}

// handleRunError 按错误类型落熔断状态。
func (s *Scheduler) handleRunError(ctx context.Context, task iqbenchmark.Task, err error, next time.Time) {
	switch {
	case errors.Is(err, iqbenchmark.ErrAccountDisabled):
		s.tripAccountDisabled(ctx, "internal agent became inactive during execution")
	case errors.Is(err, service.ErrBudgetExhausted):
		tasks, listErr := s.store.ListTasks(ctx)
		if listErr != nil {
			return
		}
		_ = s.guard.TripBudgetExhausted(ctx, tasks, 0, 0)
	case errors.Is(err, iqbenchmark.ErrInternalAgentMissing), errors.Is(err, iqbenchmark.ErrNoTargetGroup):
		// 未就绪：不写任务状态（不是任务本身的错），仅告警。
		s.logger.Error("iqbenchmark: internal agent not ready, task skipped",
			"task_id", task.ID, "error", err)
	default:
		// 其他错误：保持 idle，下个周期重试（不清空 next_run_at）。
		s.logger.Warn("iqbenchmark: task execution failed, will retry next cycle",
			"task_id", task.ID, "error", err)
		_ = s.store.MarkTaskRunFinished(ctx, task.ID, time.Now(), iqbenchmark.TaskStatusIdle, &next)
	}
}

// applyCounters 由 run 计数推导任务状态。
//
// 全失败（success=0 且 failed>0）时标记 quota_exhausted 是过于激进的推断，
// 因此这里只在「明确信号」下改状态，其余保持 idle 让下轮重试。
func (s *Scheduler) applyCounters(task iqbenchmark.Task, run *iqbenchmark.Run) string {
	if run == nil {
		return ""
	}
	if run.SuccessCount > 0 {
		return iqbenchmark.TaskStatusIdle
	}
	if run.FailedCount+run.TimeoutCount+run.BlockedCount == 0 {
		return ""
	}
	// 全失败：不直接熔断，交由下轮结果再判断（避免一次网络抖动就停掉任务）。
	s.logger.Warn("iqbenchmark: all cases failed in this run",
		"task_id", task.ID, "run_id", run.ID,
		"failed", run.FailedCount, "timeout", run.TimeoutCount, "blocked", run.BlockedCount)
	return iqbenchmark.TaskStatusIdle
}

// renewLoop 定期续期，直到 ctx 取消或租约丢失。
func (s *Scheduler) renewLoop(ctx context.Context, taskID int64, lost chan<- struct{}) {
	ticker := time.NewTicker(leaseRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.store.RenewLease(ctx, taskID, s.workerID, defaultLease); err != nil {
				if errors.Is(err, iqbenchmark.ErrLeaseLost) {
					s.logger.Warn("iqbenchmark: lease lost, signalling abort", "task_id", taskID)
					select {
					case lost <- struct{}{}:
					default:
					}
					return
				}
				// 续期瞬时失败（DB 抖动）：不立即放弃，下个周期再试。
				s.logger.Warn("iqbenchmark: lease renewal failed", "task_id", taskID, "error", err)
			}
		}
	}
}

// tripAccountDisabled 全局熔断。
func (s *Scheduler) tripAccountDisabled(ctx context.Context, reason string) {
	tasks, err := s.store.ListTasks(ctx)
	if err != nil {
		s.logger.Error("iqbenchmark: list tasks for account trip failed", "error", err)
		return
	}
	// 只处理仍可调度的任务，避免每次 tick 重复刷状态。
	pending := make([]iqbenchmark.Task, 0, len(tasks))
	for i := range tasks {
		status := tasks[i].Status
		if status == iqbenchmark.TaskStatusAccountDisabled {
			continue
		}
		pending = append(pending, tasks[i])
	}
	if len(pending) == 0 {
		return
	}
	if err := s.guard.TripAccountDisabled(ctx, pending, reason); err != nil {
		s.logger.Error("iqbenchmark: trip account disabled failed", "error", err)
	}
}

// recoverBudgetIfNewDay 跨日恢复预算熔断。
func (s *Scheduler) recoverBudgetIfNewDay(ctx context.Context) {
	tasks, err := s.store.ListTasks(ctx)
	if err != nil {
		return
	}
	if _, err := s.guard.RecoverBudgetIfNewDay(ctx, tasks); err != nil {
		s.logger.Warn("iqbenchmark: recover budget tasks failed", "error", err)
	}
}

// runCleanupIfDue 按 cleanup_interval_minutes 触发清理。
//
// 用「上次清理时间 + 间隔」判断，而不是每个 tick 都跑：
// 清理涉及大范围 UPDATE，跑太勤会与正常读写争锁。
func (s *Scheduler) runCleanupIfDue(ctx context.Context, settings iqbenchmark.Settings) {
	if s.cleaner == nil || settings.CleanupIntervalMinutes <= 0 {
		return
	}
	s.mu.Lock()
	last := s.lastCleanupAt
	if !last.IsZero() && time.Since(last) < time.Duration(settings.CleanupIntervalMinutes)*time.Minute {
		s.mu.Unlock()
		return
	}
	s.lastCleanupAt = time.Now()
	s.mu.Unlock()

	if _, err := s.cleaner.Cleanup(ctx, time.Now()); err != nil {
		s.logger.Warn("iqbenchmark: cleanup failed", "error", err)
	}
}

// agentSnapshot 安全读取内部账号快照。
func (s *Scheduler) agentSnapshot() (*iqbenchmark.InternalAgent, bool) {
	if s.agent == nil {
		return nil, false
	}
	return s.agent.Snapshot()
}

// nextRunFor 计算下次执行时间（与 service 层同一套夹紧规则）。
func nextRunFor(task iqbenchmark.Task, now time.Time) time.Time {
	interval := task.IntervalMinute
	if interval <= 0 {
		interval = 15
	}
	base := now.Add(time.Duration(interval) * time.Minute)
	if task.JitterSeconds > 0 {
		offset := rand.Intn(2*task.JitterSeconds+1) - task.JitterSeconds
		base = base.Add(time.Duration(offset) * time.Second)
	}
	// 抖动不得把间隔压到 900 秒以下（与 service.validateInterval 同一约束）。
	if earliest := now.Add(900 * time.Second); base.Before(earliest) {
		return earliest
	}
	return base
}

// maxTaskRuntime 估算单次任务的最长允许运行时间。
//
// 按「题目数 × 单题超时 + 余量」估算，避免用一个固定大值导致慢任务长期占租约。
func maxTaskRuntime(task iqbenchmark.Task) time.Duration {
	caseCount := len(task.CaseIDs)
	if caseCount <= 0 {
		caseCount = 1
	}
	// 单题超时上限 900s（settings.Normalize 已夹紧），加 60s 余量。
	return time.Duration(caseCount)*900*time.Second + 60*time.Second
}
