package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// RunService 编排一次检测执行（一个 task 的一次 tick）。
//
// 执行语义（顺序与并发都是硬性的）：
//   - 一个 run 内题目**串行**执行：并发会让同一 run 的成本与耗时不具可比性，
//     而横向对比的前提正是「同一时刻、同一条件」；
//   - 任务之间并发，上限 max_task_concurrency（默认 4）；
//   - 每个 case 独立成败，单题失败不终止整轮；
//   - 失败/超时/被拦截也写产出（占位卡片）。
//
// 单题执行逻辑在 Executor 里（它被用户自测链路复用），
// RunService 只负责「编排一轮」：前置校验、成本、终态。
type RunService struct {
	Store     Store
	Gateway   *GatewayClient
	Artifacts *ArtifactService
	Guard     *Guard
	Settings  *SettingsProvider
	Logger    *slog.Logger
	Now       func() time.Time

	// executor 是共享的单题执行器（惰性构造，保证与 RunService 同源依赖）。
	executor *Executor

	// Concurrency 是跨任务并发的信号量（nil 表示不限制，仅测试用）。
	Concurrency chan struct{}
}

// NewRunService 构造执行服务。
func NewRunService(store Store, gateway *GatewayClient, artifacts *ArtifactService, guard *Guard, settings *SettingsProvider, logger *slog.Logger) *RunService {
	if logger == nil {
		logger = slog.Default()
	}
	return &RunService{
		Store:     store,
		Gateway:   gateway,
		Artifacts: artifacts,
		Guard:     guard,
		Settings:  settings,
		Logger:    logger,
		Now:       time.Now,
	}
}

// Executor 返回共享的单题执行器。
//
// 惰性构造而不是增加构造参数：RunService 在 Wire 里已有 6 个依赖，
// 再加一个只为内部复用而存在的参数只会让每个调用点变噪音。
func (s *RunService) Executor() *Executor {
	if s == nil {
		return nil
	}
	if s.executor == nil {
		s.executor = NewExecutor(s.Gateway, s.Artifacts, s.Logger)
	}
	return s.executor
}

func (s *RunService) now() time.Time {
	if s == nil || s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func (s *RunService) settings(ctx context.Context) Settings {
	if s.Settings == nil {
		return DefaultSettings()
	}
	return s.Settings.Get(ctx)
}

// RunTaskInput 是一次任务执行所需的最小输入。
type RunTaskInput struct {
	Task *iqbenchmark.Task
	// Agent 是内部账号（必须 Ready 且 Active 才会执行）。
	Agent *iqbenchmark.InternalAgent
	// Trigger 为 schedule / manual / retry。
	Trigger string
}

// RunTask 执行一次检测任务，返回 run 记录。
//
// 前置校验全部在真正发起调用之前完成：账号不可用、预算超限、无题目、
// 分组缺失都在这里短路，避免「已经开始调用才发现没配额」。
func (s *RunService) RunTask(ctx context.Context, in RunTaskInput) (*iqbenchmark.Run, error) {
	if in.Task == nil {
		return nil, iqbenchmark.ErrTaskNotFound
	}
	task := in.Task
	settings := s.settings(ctx)

	if in.Agent == nil || !in.Agent.Ready() {
		return nil, iqbenchmark.ErrInternalAgentMissing
	}
	// 每轮都校验账号是否仍为 active：内容审核可能已自动封号。
	if !in.Agent.Active() {
		return nil, iqbenchmark.ErrAccountDisabled
	}
	if s.Gateway == nil {
		return nil, iqbenchmark.ErrNoTargetGroup
	}
	// 身份在这里构造一次，贯穿整轮执行：每轮内 Key 不会变，
	// 反复从 Agent 读会让「用哪个 Key」的判定散落在多处。
	identity := ExecutionIdentity{
		GatewayKey:    in.Agent.KeyString(),
		UsageAPIKeyID: in.Agent.KeyID(),
		Source:        iqbenchmark.SourceScheduled,
	}
	if err := identity.Valid(); err != nil {
		// Ready() 只保证 Key 非 nil，不保证 Key.Key 非空；
		// 这里补一道，避免带着空 Bearer token 去打网关。
		return nil, err
	}

	// 题目：按配置顺序串行执行；已失效的题目跳过（不阻断整轮）。
	cases, err := s.Store.ListCasesByIDs(ctx, task.CaseIDs)
	if err != nil {
		return nil, fmt.Errorf("iqbenchmark: load cases: %w", err)
	}
	ordered := make([]iqbenchmark.Case, 0, len(task.CaseIDs))
	for _, id := range task.CaseIDs {
		if c, ok := cases[id]; ok && c.Enabled {
			ordered = append(ordered, c)
		}
	}
	if len(ordered) == 0 {
		return nil, iqbenchmark.ErrCaseInvalid
	}
	// 硬上限：单轮最多执行 max_artifacts_per_run 个题目，超出部分跳过并记录。
	perRunLimit := settings.MaxArtifactsPerRun
	if perRunLimit > 0 && len(ordered) > perRunLimit {
		s.Logger.Warn("iqbenchmark: case count exceeds max_artifacts_per_run, truncating",
			"task_id", task.ID, "cases", len(ordered), "limit", perRunLimit)
		ordered = ordered[:perRunLimit]
	}

	// 预算：在发起任何调用之前校验，避免「跑完才发现超预算」。
	if exhausted, _, err := s.Guard.CheckBudget(ctx, in.Agent.KeyID(), settings.DailyCostBudget, s.now()); err != nil {
		return nil, err
	} else if exhausted {
		return nil, ErrBudgetExhausted
	}

	startedAt := s.now()
	run, err := s.Store.CreateRun(ctx, RunCreateInput{
		TaskID:           task.ID,
		GroupID:          task.GroupID,
		Model:            task.Model,
		Trigger:          defaultTrigger(in.Trigger),
		StartedAt:        startedAt,
		InternalUserID:   in.Agent.UserID(),
		InternalAPIKeyID: in.Agent.KeyID(),
	})
	if err != nil {
		return nil, fmt.Errorf("iqbenchmark: create run: %w", err)
	}

	executor := s.Executor()
	counts := RunCounts{}
	var totalDuration int64

	for i := range ordered {
		c := &ordered[i]
		execIn := ExecuteInput{
			RunID:    run.ID,
			TaskID:   task.ID,
			Identity: identity,
			Case:     c,
			GroupID:  task.GroupID,
			Model:    task.Model,
		}
		// 每个题目执行前检查一次容量：满员时后续题目记失败而非静默丢弃。
		if err := s.Artifacts.EnsureCapacity(ctx); err != nil {
			executor.RecordCaseFailure(ctx, execIn, iqbenchmark.ArtifactStatusFailed,
				iqbenchmark.ErrCodeStorageDisabled, settings.UserTestRequireReview)
			counts.Failed++
			continue
		}

		result, execErr := executor.Execute(ctx, execIn, settings.UserTestRequireReview)
		if execErr != nil {
			// 身份/依赖类错误在进入循环前就该被拦下；走到这里说明配置或依赖
			// 在运行期变了：记一条 ERROR 并计入失败，不静默丢弃这一题。
			s.Logger.Error("iqbenchmark: execute case failed to start",
				"run_id", run.ID, "case_id", c.ID, "error", execErr)
			counts.Failed++
			continue
		}
		totalDuration += result.DurationMS
		switch result.Status {
		case iqbenchmark.ArtifactStatusSuccess:
			counts.Success++
		case iqbenchmark.ArtifactStatusTimeout:
			counts.Timeout++
		case iqbenchmark.ArtifactStatusBlocked:
			counts.Blocked++
		default:
			counts.Failed++
		}
		// 判定口径的计数与上面的调用口径并行累计。
		//
		// 只有可判定的题才进 logic_total：generated（无 expected，例如图片题）
		// 与 unknown（连产出都没有）都不进分母 —— 把"不能判"算进分母
		// 会让纯出图任务的通过率恒为 0，那是错误信息，不是降级信号。
		switch result.Verdict {
		case iqbenchmark.VerdictOK, iqbenchmark.VerdictDegraded:
			counts.LogicTotal++
			if result.Verdict == iqbenchmark.VerdictOK {
				counts.LogicPassed++
			}
		}

		// 配额耗尽必须立即中止整轮：继续跑只会重复失败并继续扣尝试次数。
		// 判定用**本次结果**而不是共享字段，避免并发任务互相覆盖。
		if result.QuotaExhausted() {
			s.Logger.Warn("iqbenchmark: quota exhausted mid-run, aborting remaining cases",
				"run_id", run.ID, "task_id", task.ID, "completed", i+1, "total", len(ordered))
			break
		}
	}

	counts.TotalDurationMS = totalDuration
	finalStatus := runFinalStatus(counts)
	// run 级判定由计数汇总推导，与 Status 各说一件事：
	// Status 说「跑得怎么样」，Verdict 说「模型答得怎么样」。
	finalVerdict := iqbenchmark.AggregateRunVerdict(
		counts.Success+counts.Failed+counts.Timeout+counts.Blocked,
		counts.LogicTotal, counts.LogicPassed,
	)
	if err := s.Store.FinishRun(ctx, run.ID, s.now(), finalStatus, counts); err != nil {
		s.Logger.Error("iqbenchmark: finish run failed", "run_id", run.ID, "error", err)
	}
	run.Status = finalStatus
	run.Verdict = finalVerdict
	run.LogicTotal = counts.LogicTotal
	run.LogicPassed = counts.LogicPassed
	run.SuccessCount = counts.Success
	run.FailedCount = counts.Failed
	run.TimeoutCount = counts.Timeout
	run.BlockedCount = counts.Blocked
	run.TotalDurationMS = counts.TotalDurationMS
	return run, nil
}

// runFinalStatus 由计数推导终态。
//
// 有成功且有失败 → partial（作品展需要区分「全失败」与「部分成功」，
// 否则模型能力差异会被抹平）。
func runFinalStatus(counts RunCounts) string {
	total := counts.Success + counts.Failed + counts.Timeout + counts.Blocked
	switch {
	case total == 0:
		return iqbenchmark.RunStatusFailed
	case counts.Success == total:
		return iqbenchmark.RunStatusSuccess
	case counts.Success == 0:
		return iqbenchmark.RunStatusFailed
	default:
		return iqbenchmark.RunStatusPartial
	}
}

func defaultTrigger(trigger string) string {
	switch trigger {
	case iqbenchmark.RunTriggerManual, iqbenchmark.RunTriggerRetry:
		return trigger
	default:
		return iqbenchmark.RunTriggerSchedule
	}
}

// ------- 并发控制 -------

// AcquireSlot 获取一个任务并发槽位；满员时返回 false（不阻塞）。
//
// 用非阻塞获取而不是阻塞等待：调度器每个 tick 都应该在有界时间内结束，
// 阻塞等待会让 tick 无限延长并堆积。
func (s *RunService) AcquireSlot() bool {
	if s == nil || s.Concurrency == nil {
		return true
	}
	select {
	case s.Concurrency <- struct{}{}:
		return true
	default:
		return false
	}
}

// ReleaseSlot 释放并发槽位。
func (s *RunService) ReleaseSlot() {
	if s == nil || s.Concurrency == nil {
		return
	}
	select {
	case <-s.Concurrency:
	default:
	}
}

// RecoverCosts 回采未收集成本的 run。
//
// usage_logs 是异步落库的，run 结束时拿不到成本，因此必须事后按
// request_id 回采。回采失败的 run 保持 cost_collected_at IS NULL，
// 下一轮清理会继续尝试（不无限重试，由清理周期自然收敛）。
func (s *RunService) RecoverCosts(ctx context.Context, lookback time.Duration, limit int) (int, error) {
	if lookback <= 0 {
		lookback = 10 * time.Minute
	}
	if limit <= 0 {
		limit = 50
	}
	runsNeedingCost, ok := s.Store.(runCostCollector)
	if !ok {
		return 0, nil
	}
	runs, err := runsNeedingCost.ListRunsNeedingCostCollection(ctx, s.now().Add(-lookback), limit)
	if err != nil {
		return 0, fmt.Errorf("iqbenchmark: list runs needing cost: %w", err)
	}
	collected := 0
	for i := range runs {
		run := &runs[i]
		artifacts, err := s.Store.ListArtifactsByRun(ctx, run.ID)
		if err != nil {
			s.Logger.Warn("iqbenchmark: list artifacts for cost collection failed",
				"run_id", run.ID, "error", err)
			continue
		}
		requestIDs := make([]string, 0, len(artifacts))
		for j := range artifacts {
			if id, ok := artifacts[j].RequestParams["request_id"].(string); ok && id != "" {
				requestIDs = append(requestIDs, id)
			}
		}
		if len(requestIDs) == 0 {
			continue
		}
		sum, ok := s.Store.(usageSummer)
		if !ok {
			break
		}
		snap, err := sum.SumUsageByRequestIDs(ctx, run.InternalAPIKeyID, requestIDs)
		if err != nil {
			s.Logger.Warn("iqbenchmark: sum usage by request ids failed", "run_id", run.ID, "error", err)
			continue
		}
		if snap.Matched == 0 {
			// usage_logs 还没落库，本轮跳过，下轮再试。
			continue
		}
		if err := s.Store.SetRunCost(ctx, run.ID, snap.Cost, snap.InputTokens, snap.OutputTokens, s.now()); err != nil {
			s.Logger.Warn("iqbenchmark: write back run cost failed", "run_id", run.ID, "error", err)
			continue
		}
		collected++
	}
	return collected, nil
}

// runCostCollector 是可选的仓储能力（便于单测注入）。
type runCostCollector interface {
	ListRunsNeedingCostCollection(ctx context.Context, olderThan time.Time, limit int) ([]iqbenchmark.Run, error)
}

// usageSummer 是可选的仓储能力。
type usageSummer interface {
	SumUsageByRequestIDs(ctx context.Context, apiKeyID int64, requestIDs []string) (iqbenchmark.UsageSnapshotByRequestIDs, error)
}
