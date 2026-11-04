package modeldegradation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

// 本文件编排一次检测的完整生命周期。
//
// 执行模型（与参考实现一致）：POST /runs 立即返回 status=running，
// 真实执行在后台 goroutine 里进行，前端每 3 秒轮询 GET /runs/:id。
//
// 为什么不阻塞到跑完：生成题超时上限是 60 分钟。一个 HTTP 请求挂那么久，
// 中间任何一层（浏览器、反代、网关）都会先超时断开，用户看到的是
// 「一直转圈然后失败」，而实际检测可能还在正常跑。
//
// 并发控制：单个用户同时只允许一个在跑的检测。理由不是性能，而是
// 「同时开 5 个生成题检测」会让上游限流，反而全部失败 —— 那看起来像降智。

// 错误定义。
var (
	ErrNoQuestions      = errors.New("modeldegradation: 请至少选择或填写一道题目")
	ErrNoTasks          = errors.New("modeldegradation: 请至少选择一个检测项")
	ErrLogicNeedsExpect = errors.New("modeldegradation: 自定义逻辑题必须填写期望答案")
	ErrRenderTimeout    = errors.New("modeldegradation: 生成题超时时间超出允许范围")
	ErrModelMissing     = errors.New("modeldegradation: 请填写模型名称")
	ErrAPIKeyMissing    = errors.New("modeldegradation: 请填写上游 API Key")
	ErrConcurrencyLimit = errors.New("modeldegradation: 已有检测正在进行中，请等它结束")
	ErrRunNotFound      = errors.New("modeldegradation: 检测记录不存在")
	ErrRunNotRunning    = errors.New("modeldegradation: 该检测已经结束")
)

// Service 是降智检测的编排入口。
type Service struct {
	Store  Store
	Logger *slog.Logger
	Now    func() time.Time

	// Resolver 可注入（测试用）。为 nil 时用系统解析。
	Resolver *net.Resolver

	// 逻辑题超时。生成题按用户选择（最长 60 分钟），逻辑题固定短超时：
	// 一道算术题让模型想 15 分钟多半是卡住了，早失败早给用户反馈。
	LogicTimeout time.Duration

	// 单用户并发闸门。key 是 userID。
	mu       sync.Mutex
	inflight map[int64]context.CancelFunc
}

// NewService 构造服务。
func NewService(store Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		Store:        store,
		Logger:       logger,
		Now:          time.Now,
		LogicTimeout: 3 * time.Minute,
		inflight:     make(map[int64]context.CancelFunc),
	}
}

// StartOutcome 是发起检测的结果。
type StartOutcome struct {
	Run *RunDetail
}

// Start 校验并启动一次检测。
//
// 校验顺序刻意与前端一致（baseUrl → apiKey → model → tasks → timeout →
// questions → custom expected），这样前端先拦一次、后端再拦一次，
// 用户得到的提示文案相同，不会出现「前端说 A、后端说 B」。
func (s *Service) Start(ctx context.Context, userID int64, ownerEmail string, req StartRequest) (*RunDetail, error) {
	// ── 校验 ──
	if strings.TrimSpace(req.APIKey) == "" {
		return nil, ErrAPIKeyMissing
	}
	if strings.TrimSpace(req.ModelID) == "" {
		return nil, ErrModelMissing
	}
	if len(req.Tasks) == 0 {
		return nil, ErrNoTasks
	}
	if len(req.Questions) == 0 {
		return nil, ErrNoQuestions
	}
	if !IsValidProtocol(req.Protocol) {
		return nil, fmt.Errorf("modeldegradation: 未知协议 %q", req.Protocol)
	}
	if !IsValidReasoningEffort(req.ReasoningEffort) {
		return nil, fmt.Errorf("modeldegradation: 未知思考等级 %q", req.ReasoningEffort)
	}

	wantRender := contains(req.Tasks, TaskTypeRender)
	if wantRender {
		// 区间校验用秒做单位，与前端「分钟 × 60」的换算对齐。
		if req.RenderTimeoutSeconds < MinRenderTimeoutMinutes*60 ||
			req.RenderTimeoutSeconds > MaxRenderTimeoutMinutes*60 {
			return nil, ErrRenderTimeout
		}
	}

	// 自定义逻辑题必须有期望答案，否则永远判不了分。
	for _, q := range req.Questions {
		if q.TaskType == TaskTypeLogic && q.IsCustom() && strings.TrimSpace(q.Expected) == "" {
			return nil, ErrLogicNeedsExpect
		}
		if strings.TrimSpace(q.Prompt) == "" {
			return nil, ErrNoQuestions
		}
		if q.TaskType != TaskTypeLogic && q.TaskType != TaskTypeRender {
			return nil, fmt.Errorf("modeldegradation: 未知题型 %q", q.TaskType)
		}
	}

	// ── 地址校验（SSRF 防护） ──
	parsed, err := ValidateBaseURL(ctx, req.BaseURL, s.Resolver)
	if err != nil {
		return nil, err
	}

	// ── 并发闸门 ──
	s.mu.Lock()
	if _, busy := s.inflight[userID]; busy {
		s.mu.Unlock()
		return nil, ErrConcurrencyLimit
	}
	// 先占位，防止两个并发请求同时通过检查。
	runCtx, cancel := context.WithCancel(context.Background())
	s.inflight[userID] = cancel
	s.mu.Unlock()

	// 从这里开始任何失败都要释放闸门。
	release := func() {
		s.mu.Lock()
		delete(s.inflight, userID)
		s.mu.Unlock()
		cancel()
	}

	// ── 建运行记录 ──
	run, err := s.Store.CreateRun(ctx, RunCreateInput{
		UserID:               userID,
		OwnerEmail:           ownerEmail,
		BaseURLDisplay:       SanitizeBaseURLForDisplay(parsed),
		ModelID:              strings.TrimSpace(req.ModelID),
		Protocol:             req.Protocol,
		ReasoningEffort:      normalizeEffort(req.Protocol, req.ReasoningEffort),
		RenderTimeoutSeconds: req.RenderTimeoutSeconds,
	})
	if err != nil {
		release()
		return nil, err
	}

	// ── 生成题目快照 ──
	items := make([]Item, 0, len(req.Questions))
	for i, q := range req.Questions {
		prompt := q.Prompt
		// render 题：即使题库里已含占位符，这里再替换一次保证每轮不同。
		if q.TaskType == TaskTypeRender {
			filled, _ := FillRenderTemplate(prompt)
			prompt = filled
		}
		key := q.Key
		if key == "" {
			key = CustomTaskKey
		}
		items = append(items, Item{
			RunID:     run.ID,
			Type:      q.TaskType,
			Key:       key,
			Prompt:    prompt,
			Expected:  q.Expected,
			Verdict:   ItemVerdictUnknown,
			SortOrder: i,
		})
	}

	if err := s.Store.InsertItems(ctx, run.ID, items); err != nil {
		release()
		return nil, err
	}

	// ── 后台执行 ──
	go func() {
		defer release()
		s.execute(runCtx, run.ID, parsed, req, items)
	}()

	// 立即返回 running 状态，前端开始轮询。
	detail, err := s.Store.GetRunDetail(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	return detail, nil
}

// execute 在后台跑完所有题目。
//
// 逐题串行：同一轮内并行会让「同一时刻、同一条件」这个前提失效 ——
// 上游可能因为并发把这些请求排队或限流，测出来的就不是模型能力了。
func (s *Service) execute(ctx context.Context, runID int64, base *url.URL, req StartRequest, items []Item) {
	started := s.Now()

	var (
		logicPassed  int
		logicTotal   int
		logicUnknown int
		renderTotal  int
		renderOK     int
		renderUnk    int

		firstErrorKind string
		firstErrorMsg  string
		cancelled      bool
	)

	for i := range items {
		select {
		case <-ctx.Done():
			cancelled = true
		default:
		}
		if cancelled {
			break
		}

		item := items[i]
		timeout := s.LogicTimeout
		if item.Type == TaskTypeRender && req.RenderTimeoutSeconds > 0 {
			timeout = time.Duration(req.RenderTimeoutSeconds) * time.Second
		}

		callCtx, cancel := context.WithTimeout(ctx, timeout)
		// 记录运行状态：开始执行这一题。
		caller := &UpstreamCaller{
			BaseURL:  base,
			APIKey:   req.APIKey,
			Protocol: req.Protocol,
			Model:    req.ModelID,
			Effort:   normalizeEffort(req.Protocol, req.ReasoningEffort),
			Timeout:  timeout,
		}
		res := caller.Call(callCtx, item.Prompt)
		cancel()

		// 上下文被取消 = 用户中止，不要把剩余题目算成失败。
		if ctx.Err() != nil {
			cancelled = true
			break
		}

		var result ItemResultInput
		switch item.Type {
		case TaskTypeLogic:
			logicTotal++
			jr := JudgeLogic(item.Expected, res.Text, res.ErrorKind, res.ErrorMessage)
			result = ItemResultInput{
				Response:   res.Text,
				Passed:     jr.Passed,
				Verdict:    jr.Verdict,
				Detail:     jr.Detail,
				DurationMS: res.DurationMS,
			}
			if jr.Verdict == ItemVerdictOK {
				logicPassed++
			}
			if jr.Verdict == ItemVerdictUnknown {
				logicUnknown++
			}

		case TaskTypeRender:
			renderTotal++
			rr := JudgeRender(res.Text, res.ErrorKind, res.ErrorMessage, extractSVG)
			result = ItemResultInput{
				Response:   res.Text,
				Passed:     rr.Passed,
				Verdict:    rr.Verdict,
				Detail:     rr.Detail,
				Artwork:    rr.Artwork,
				DurationMS: res.DurationMS,
			}
			if rr.Verdict == ItemVerdictOK {
				renderOK++
			}
			if rr.Verdict == ItemVerdictUnknown {
				renderUnk++
			}
		}

		// 记住第一个错误，作为整轮的 error_message 展示。
		if res.ErrorKind != "" && firstErrorKind == "" {
			firstErrorKind = res.ErrorKind
			firstErrorMsg = res.ErrorMessage
		}

		if err := s.Store.UpdateItem(ctx, item.ID, result); err != nil {
			s.Logger.Warn("modeldegradation: 更新单题结果失败",
				slog.Int64("run_id", runID), slog.Int64("item_id", item.ID), slog.String("error", err.Error()))
		}
	}

	// ── 聚合终态 ──
	finish := RunFinishInput{
		LogicPassed:   logicPassed,
		LogicTotal:    logicTotal,
		RenderVerdict: AggregateRenderVerdict(renderTotal, renderOK, renderUnk),
		DurationMS:    s.Now().Sub(started).Milliseconds(),
		CompletedAt:   s.Now(),
	}

	if cancelled {
		// 用户中止：已跑完的题目结果仍然有效，但整轮不算 degraded。
		finish.Status = StatusCancelled
		finish.Verdict = AggregateRunVerdict(logicPassed, logicTotal, logicUnknown)
	} else {
		finish.Status = StatusCompleted
		finish.Verdict = AggregateRunVerdict(logicPassed, logicTotal, logicUnknown)
		finish.ErrorKind = firstErrorKind
		finish.ErrorMessage = firstErrorMsg
	}

	if err := s.Store.FinishRun(context.Background(), runID, finish); err != nil {
		s.Logger.Error("modeldegradation: 落终态失败",
			slog.Int64("run_id", runID), slog.String("error", err.Error()))
	}
}

// Cancel 中止一次运行。
//
// 返回 ErrRunNotRunning 表示已经结束 —— 对应参考实现的 409，
// 前端据此把它当「已完成」而不是失败。
func (s *Service) Cancel(ctx context.Context, runID, userID int64) error {
	run, err := s.Store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status != StatusRunning {
		return ErrRunNotRunning
	}

	if ok, err := s.Store.CancelRun(ctx, runID, s.Now()); err != nil {
		return err
	} else if !ok {
		return ErrRunNotRunning
	}

	// 中断后台 goroutine。
	s.mu.Lock()
	if cancel, ok := s.inflight[userID]; ok {
		cancel()
		delete(s.inflight, userID)
	}
	s.mu.Unlock()

	return nil
}

// GetRunDetail 读取一次检测的完整详情。
func (s *Service) GetRunDetail(ctx context.Context, id, viewerUserID int64) (*RunDetail, error) {
	detail, err := s.Store.GetRunDetail(ctx, id)
	if err != nil {
		return nil, err
	}
	detail.IsOwner = detail.UserID != 0 && detail.UserID == viewerUserID
	return detail, nil
}

// ListRuns 读取检测历史。
func (s *Service) ListRuns(ctx context.Context, f RunFilter) ([]Run, int64, error) {
	return s.Store.ListRuns(ctx, f)
}

// Stats 读取顶部统计。
func (s *Service) Stats(ctx context.Context, f RunFilter) (Stats, error) {
	return s.Store.GetStats(ctx, f)
}

// TaskBank 读取题库，按题型分组。
func (s *Service) TaskBank(ctx context.Context) (*TaskBank, error) {
	tasks, err := s.Store.ListTasks(ctx, true)
	if err != nil {
		return nil, err
	}
	bank := &TaskBank{Logic: []Task{}, Render: []Task{}}
	for _, t := range tasks {
		switch t.Type {
		case TaskTypeLogic:
			bank.Logic = append(bank.Logic, t)
		case TaskTypeRender:
			bank.Render = append(bank.Render, t)
		}
	}
	return bank, nil
}

// normalizeEffort 在不支持思考等级的协议上把 effort 清空。
//
// 这不是「宽容」，而是必要的：Anthropic messages 协议收到 reasoning_effort
// 会返回 400，那个 400 会被错误归一化成 server_error，让用户以为上游挂了。
func normalizeEffort(protocol, effort string) string {
	if !SupportsReasoningEffort(protocol) {
		return ""
	}
	return effort
}

// contains 判断切片是否含某值。
func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
