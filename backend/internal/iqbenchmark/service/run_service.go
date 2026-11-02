package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/sanitize"
)

// RunService 编排一次检测执行（一个 task 的一次 tick）。
//
// 执行语义（顺序与并发都是硬性的）：
//   - 一个 run 内题目**串行**执行：并发会让同一 run 的成本与耗时不具可比性，
//     而横向对比的前提正是「同一时刻、同一条件」；
//   - 任务之间并发，上限 max_task_concurrency（默认 4）；
//   - 每个 case 独立成败，单题失败不终止整轮；
//   - 失败/超时/被拦截也写产出（占位卡片）。
type RunService struct {
	Store     Store
	Gateway   *GatewayClient
	Artifacts *ArtifactService
	Guard     *Guard
	Settings  *SettingsProvider
	Logger    *slog.Logger
	Now       func() time.Time

	// Concurrency 是跨任务并发的信号量（nil 表示不限制，仅测试用）。
	Concurrency chan struct{}

	// errMu 保护 lastErrQuota（并发执行时多任务共享同一 RunService）。
	errMu        sync.Mutex
	lastErrQuota bool
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
	// Key 明文在这里取出一次，贯穿整轮执行：每轮内 Key 不会变，
	// 反复从 Agent 读会让「用哪个 Key」的判定散落在多处。
	apiKey := in.Agent.KeyString()
	if strings.TrimSpace(apiKey) == "" {
		// Ready() 只保证 Key 非 nil，不保证 Key.Key 非空；
		// 这里补一道，避免带着空 Bearer token 去打网关。
		return nil, iqbenchmark.ErrInternalAgentMissing
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
	if exhausted, spent, err := s.Guard.CheckBudget(ctx, in.Agent.KeyID(), settings.DailyCostBudget, s.now()); err != nil {
		return nil, err
	} else if exhausted {
		return nil, ErrBudgetExhausted
	} else {
		_ = spent
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

	counts := RunCounts{}
	var totalDuration int64

	for i := range ordered {
		c := &ordered[i]
		// 每个题目执行前检查一次容量：满员时后续题目记失败而非静默丢弃。
		if err := s.Artifacts.EnsureCapacity(ctx); err != nil {
			s.recordCaseFailure(ctx, run, task, c, iqbenchmark.ArtifactStatusFailed, iqbenchmark.ErrCodeStorageDisabled)
			counts.Failed++
			continue
		}

		status, duration, reqID := s.executeCase(ctx, apiKey, run, task, c)
		totalDuration += duration
		switch status {
		case iqbenchmark.ArtifactStatusSuccess:
			counts.Success++
		case iqbenchmark.ArtifactStatusTimeout:
			counts.Timeout++
		case iqbenchmark.ArtifactStatusBlocked:
			counts.Blocked++
		default:
			counts.Failed++
		}

		// 配额耗尽必须立即中止整轮：继续跑只会重复失败并继续扣尝试次数。
		if status == iqbenchmark.ArtifactStatusFailed && s.lastErrorWasQuotaExhausted() {
			s.Logger.Warn("iqbenchmark: quota exhausted mid-run, aborting remaining cases",
				"run_id", run.ID, "task_id", task.ID, "completed", i+1, "total", len(ordered))
			break
		}
		_ = reqID
	}

	counts.TotalDurationMS = totalDuration
	finalStatus := runFinalStatus(counts)
	if err := s.Store.FinishRun(ctx, run.ID, s.now(), finalStatus, counts); err != nil {
		s.Logger.Error("iqbenchmark: finish run failed", "run_id", run.ID, "error", err)
	}
	run.Status = finalStatus
	run.SuccessCount = counts.Success
	run.FailedCount = counts.Failed
	run.TimeoutCount = counts.Timeout
	run.BlockedCount = counts.Blocked
	run.TotalDurationMS = counts.TotalDurationMS
	return run, nil
}

// executeCase 执行单个题目并写入产出，返回产出状态 / 耗时 / request_id。
//
// apiKey 是本次执行使用的明文 Key，一路传到网关调用点，存在栈上而不是结构体字段上。
func (s *RunService) executeCase(ctx context.Context, apiKey string, run *iqbenchmark.Run, task *iqbenchmark.Task, c *iqbenchmark.Case) (string, int64, string) {
	snapshot := iqbenchmark.CaseSnapshot(c)
	started := s.now()

	base := ArtifactCreateInput{
		RunID:         run.ID,
		TaskID:        task.ID,
		CaseID:        c.ID,
		CaseSnapshot:  snapshot,
		GroupID:       task.GroupID,
		Model:         task.Model,
		RequestParams: DecodeSafeJSON(c.Params),
	}

	if iqbenchmark.IsImageCase(c.Kind) {
		return s.executeImageCase(ctx, apiKey, base, task, c, started)
	}
	return s.executeTextCase(ctx, apiKey, base, task, c, started)
}

// executeTextCase 执行文本 / 代码类题目。
func (s *RunService) executeTextCase(ctx context.Context, apiKey string, base ArtifactCreateInput, task *iqbenchmark.Task, c *iqbenchmark.Case, started time.Time) (string, int64, string) {
	settings := s.settings(ctx)
	resp, err := s.Gateway.Chat(ctx, ChatRequest{
		Model:        task.Model,
		Prompt:       c.Prompt,
		SystemPrompt: c.SystemPrompt,
		Params:       c.Params,
		APIKey:       apiKey,
	})
	duration := s.now().Sub(started).Milliseconds()
	base.DurationMS = int(duration)

	if err != nil {
		status, code := classifyInvokeError(err)
		s.setLastError(code)
		base.ErrorCode = code
		if _, saveErr := s.Artifacts.SaveFailedArtifact(ctx, base, status, code); saveErr != nil {
			s.Logger.Error("iqbenchmark: persist failed artifact failed",
				"run_id", base.RunID, "case_id", base.CaseID, "error", saveErr)
		}
		return status, duration, ""
	}
	base.InputTokens = resp.InputTokens
	base.OutputTokens = resp.OutputTokens

	renderKind := sanitize.Classify(sanitize.ClassifyInput{Text: resp.Text, ExpectKind: c.ExpectKind})
	base.RequestParams = withRequestID(base.RequestParams, resp.RequestID)
	artifact, saveErr := s.Artifacts.SaveTextArtifact(ctx, base, renderKind, resp.Text)
	if saveErr != nil {
		s.Logger.Error("iqbenchmark: persist artifact failed",
			"run_id", base.RunID, "case_id", base.CaseID, "error", saveErr)
		return iqbenchmark.ArtifactStatusFailed, duration, resp.RequestID
	}
	_ = settings
	return artifact.Status, duration, resp.RequestID
}

// executeImageCase 执行图片类题目。
//
// 图片字节由调用方注入的下载/上传链路处理（见 image_uploader 适配）；
// 这里只把网关返回的 URL / b64 记录为产出引用。
func (s *RunService) executeImageCase(ctx context.Context, apiKey string, base ArtifactCreateInput, task *iqbenchmark.Task, c *iqbenchmark.Case, started time.Time) (string, int64, string) {
	resp, err := s.Gateway.Image(ctx, ImageRequest{
		Model:  task.Model,
		Prompt: c.Prompt,
		Params: c.Params,
		APIKey: apiKey,
	})
	duration := s.now().Sub(started).Milliseconds()
	base.DurationMS = int(duration)

	if err != nil {
		status, code := classifyInvokeError(err)
		s.setLastError(code)
		base.ErrorCode = code
		if _, saveErr := s.Artifacts.SaveFailedArtifact(ctx, base, status, code); saveErr != nil {
			s.Logger.Error("iqbenchmark: persist failed image artifact failed",
				"run_id", base.RunID, "case_id", base.CaseID, "error", saveErr)
		}
		return status, duration, ""
	}

	base.RequestParams = withRequestID(base.RequestParams, resp.RequestID)
	if len(resp.URLs) == 0 && len(resp.B64) == 0 {
		// 模型没给任何图片：记为 no_artifact 失败（不是成功空产出）。
		code := iqbenchmark.ErrCodeNoArtifact
		base.ErrorCode = code
		if _, saveErr := s.Artifacts.SaveFailedArtifact(ctx, base, iqbenchmark.ArtifactStatusFailed, code); saveErr != nil {
			s.Logger.Error("iqbenchmark: persist no-artifact failure failed", "run_id", base.RunID, "error", saveErr)
		}
		return iqbenchmark.ArtifactStatusFailed, duration, resp.RequestID
	}

	// 首版只用 URL 直链（b64 需经 ImageResultUploader 转存，见 known limitations）。
	if len(resp.URLs) > 0 {
		if _, saveErr := s.Artifacts.SaveImageArtifact(ctx, base, resp.URLs[0], ""); saveErr != nil {
			s.Logger.Error("iqbenchmark: persist image artifact failed", "run_id", base.RunID, "error", saveErr)
			return iqbenchmark.ArtifactStatusFailed, duration, resp.RequestID
		}
		return iqbenchmark.ArtifactStatusSuccess, duration, resp.RequestID
	}

	// 只有 b64：无对象存储时降级为 storage_disabled。
	code := iqbenchmark.ErrCodeStorageDisabled
	base.ErrorCode = code
	if _, saveErr := s.Artifacts.SaveFailedArtifact(ctx, base, iqbenchmark.ArtifactStatusFailed, code); saveErr != nil {
		s.Logger.Error("iqbenchmark: persist storage-disabled failure failed", "run_id", base.RunID, "error", saveErr)
	}
	return iqbenchmark.ArtifactStatusFailed, duration, resp.RequestID
}

// recordCaseFailure 记录一条失败产出（容量不足等前置失败）。
func (s *RunService) recordCaseFailure(ctx context.Context, run *iqbenchmark.Run, task *iqbenchmark.Task, c *iqbenchmark.Case, status, code string) {
	base := ArtifactCreateInput{
		RunID:         run.ID,
		TaskID:        task.ID,
		CaseID:        c.ID,
		CaseSnapshot:  iqbenchmark.CaseSnapshot(c),
		GroupID:       task.GroupID,
		Model:         task.Model,
		RequestParams: DecodeSafeJSON(c.Params),
	}
	if _, err := s.Artifacts.SaveFailedArtifact(ctx, base, status, code); err != nil {
		s.Logger.Error("iqbenchmark: record case failure failed",
			"run_id", run.ID, "case_id", c.ID, "error", err)
	}
}

// classifyInvokeError 把调用错误映射为产出状态 + 稳定错误码。
func classifyInvokeError(err error) (status, code string) {
	var non2xx *ErrGatewayNon2xx
	switch {
	case errors.Is(err, iqbenchmark.ErrDeadlineExceeded):
		return iqbenchmark.ArtifactStatusTimeout, iqbenchmark.ErrCodeTimeout
	case errors.Is(err, iqbenchmark.ErrGatewayUnreachable):
		return iqbenchmark.ArtifactStatusFailed, iqbenchmark.ErrCodeGatewayUnreachable
	case errors.As(err, &non2xx):
		switch non2xx.Status {
		case 429:
			return iqbenchmark.ArtifactStatusFailed, iqbenchmark.ErrCodeRateLimited
		case 402, 403:
			return iqbenchmark.ArtifactStatusFailed, iqbenchmark.ErrCodeInsufficientBalance
		case 400, 422:
			return iqbenchmark.ArtifactStatusFailed, iqbenchmark.ErrCodeCapabilityMissing
		default:
			return iqbenchmark.ArtifactStatusFailed, iqbenchmark.ErrCodeUpstreamError
		}
	default:
		return iqbenchmark.ArtifactStatusFailed, iqbenchmark.ErrCodeUpstreamError
	}
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

// withRequestID 把 request_id 记入请求参数快照，供事后成本回采。
func withRequestID(params map[string]any, requestID string) map[string]any {
	if strings.TrimSpace(requestID) == "" {
		return params
	}
	if params == nil {
		params = map[string]any{}
	}
	params["request_id"] = requestID
	return params
}

// ------- 配额耗尽的中途传播 -------

// lastErrorWasQuotaExhausted 判断最近一次题目失败是否因配额耗尽。
//
// 用执行期状态而非返回值，是因为 executeCase 的返回签名已承载状态与耗时；
// 这里用独立字段把「需要中止整轮」这一信息带出来，避免污染主流程的可读性。
func (s *RunService) lastErrorWasQuotaExhausted() bool {
	if s == nil {
		return false
	}
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.lastErrQuota
}

func (s *RunService) setLastError(code string) {
	if s == nil {
		return
	}
	s.errMu.Lock()
	defer s.errMu.Unlock()
	s.lastErrQuota = code == iqbenchmark.ErrCodeInsufficientBalance || code == iqbenchmark.ErrCodeRateLimited
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
