package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/sanitize"
)

// ExecutionIdentity 是一次检测的「计费与归属身份」。
//
// 设计要点（这是两条链路切分的关键）：
// 执行器内部**只读这个结构**，不做任何「是不是内部账号」的判断。
// 定时检测与用户自测的差异全部在构造这个值时确定，执行器无从得知也不需要知道。
//
// 之所以能这样切分，是因为执行器内部**没有计费代码** ——
// 计费完全发生在网关侧（按 Authorization 里的 Key 归属扣）。
// 所以「用谁的 Key」就是唯一的切分点，不需要在服务层重复实现计费。
type ExecutionIdentity struct {
	// GatewayKey 是调用自身网关用的明文 Key（只在内存中，绝不落库/落日志）。
	GatewayKey string
	// UsageAPIKeyID 是 usage_logs 回采时用的 api_key_id（与 GatewayKey 对应的 Key）。
	UsageAPIKeyID int64
	// TriggeredByUserID 是产出归属的用户；定时检测为 0（表示 null）。
	TriggeredByUserID int64
	// Source 决定产出的来源标记与统计口径。
	Source string
}

// Valid 校验身份是否自洽：Key 必须有值，来源必须是合法的存储值。
//
// 所有失败都用本模块的通用哨兵（ErrGatewayKeyMissing 等），
// 刻意不用 ErrInternalAgentMissing —— 执行器不知道也不该知道「内部账号」的概念。
func (e ExecutionIdentity) Valid() error {
	if strings.TrimSpace(e.GatewayKey) == "" {
		return iqbenchmark.ErrGatewayKeyMissing
	}
	if !iqbenchmark.ValidStoredSource(e.Source) {
		return fmt.Errorf("iqbenchmark: invalid execution source %q", e.Source)
	}
	if e.Source == iqbenchmark.SourceUserTest && e.TriggeredByUserID <= 0 {
		// 与数据库的 iq_artifact_source_consistent 约束同一条不变量：
		// 自测产出必须知道是谁测的。在这里先挡一次，给出比 SQL 错误更清楚的返回。
		return errors.New("iqbenchmark: user_test identity requires a triggering user")
	}
	if e.Source == iqbenchmark.SourceScheduled && e.TriggeredByUserID != 0 {
		return errors.New("iqbenchmark: scheduled identity must not carry a triggering user")
	}
	return nil
}

// ModerationFor 决定该身份产出的审核初始状态。
func (e ExecutionIdentity) ModerationFor(requireReview bool) string {
	// 定时检测的产出由管理员配置的内部账号产生，可信，直接 approved。
	if e.Source != iqbenchmark.SourceUserTest {
		return iqbenchmark.ModerationApproved
	}
	if requireReview {
		return iqbenchmark.ModerationPending
	}
	return iqbenchmark.ModerationApproved
}

// ExecuteInput 是执行单个题目所需的全部输入。
type ExecuteInput struct {
	// RunID / TaskID 为 0 表示无关联（用户自测）。
	RunID  int64
	TaskID int64

	Identity ExecutionIdentity

	Case    *iqbenchmark.Case
	GroupID int64
	Model   string
}

// ExecuteResult 是单题执行的结果。
type ExecuteResult struct {
	Status     string
	DurationMS int64
	RequestID  string
	// ErrorCode 是内部错误码（成功时为空）。
	//
	// 用返回值而不是共享字段传递它，是因为共享字段在**并发任务**下会互相污染：
	// 任务 A 刚判定「配额耗尽」，任务 B 的一次成功就能把它覆盖成 false，
	// 于是 A 的剩余题目继续跑、继续扣失败次数。
	ErrorCode string
	// Verdict / RenderVerdict 是本题判定（判定口径），与 Status（调用口径）正交。
	//
	// 单列出来而不再从 Status 推导，是因为调用方要按它累计 run 级的
	// logic_total / logic_passed；若留在产出行里再回查，就等于把一次
	// 已经算过的判定重新读一遍，既慢又可能读到被审核改动后的值。
	Verdict       string
	RenderVerdict string
	// ErrorKind 是 12 类上游错误归一化，与 ErrorCode 并存（不替代）。
	ErrorKind string
	// ArtifactID 是本次写入的产出 ID（0 表示没有产出）。
	// 自测链路需要它把 user_tests.artifact_id 回填。
	ArtifactID int64
}

// QuotaExhausted 判断本次执行是否因配额/限流而失败（需要中止整轮）。
func (r ExecuteResult) QuotaExhausted() bool {
	return r.ErrorCode == iqbenchmark.ErrCodeInsufficientBalance ||
		r.ErrorCode == iqbenchmark.ErrCodeRateLimited
}

// Executor 执行「一个题目」并落产出。
//
// 它从 RunService 中拆出，唯一目的是让「用户自测」能复用同一套执行逻辑
// 而不必伪造一个 task/run。差异只剩两处：
//   - identity（用谁的 Key、算谁的产出、初始审核状态）；
//   - 产出如何关联（定时链路挂 run_id/task_id；自测两者为 NULL）。
type Executor struct {
	Gateway   *GatewayClient
	Artifacts *ArtifactService
	Logger    *slog.Logger
	Now       func() time.Time
}

// NewExecutor 构造执行器。它与 RunService 共享同一批依赖实例。
func NewExecutor(gateway *GatewayClient, artifacts *ArtifactService, logger *slog.Logger) *Executor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Executor{
		Gateway:   gateway,
		Artifacts: artifacts,
		Logger:    logger,
		Now:       time.Now,
	}
}

func (e *Executor) now() time.Time {
	if e == nil || e.Now == nil {
		return time.Now()
	}
	return e.Now()
}

// Execute 执行单个题目并写入产出。
//
// 单题失败不返回 error（失败本身也是一种产出，作品展以占位卡片展示）；
// 只有「连产出都写不进去」或输入非法才返回 error。
func (e *Executor) Execute(ctx context.Context, in ExecuteInput, requireReview bool) (ExecuteResult, error) {
	if in.Case == nil {
		return ExecuteResult{}, iqbenchmark.ErrCaseNotFound
	}
	if err := in.Identity.Valid(); err != nil {
		return ExecuteResult{}, err
	}
	if e.Gateway == nil {
		return ExecuteResult{}, iqbenchmark.ErrNoTargetGroup
	}
	return e.executeCase(ctx, in, requireReview), nil
}

// executeCase 执行单个题目并写入产出。
func (e *Executor) executeCase(ctx context.Context, in ExecuteInput, requireReview bool) ExecuteResult {
	c := in.Case
	started := e.now()

	base := ArtifactCreateInput{
		RunID:             in.RunID,
		TaskID:            in.TaskID,
		CaseID:            c.ID,
		CaseSnapshot:      iqbenchmark.CaseSnapshot(c),
		GroupID:           in.GroupID,
		Model:             in.Model,
		RequestParams:     DecodeSafeJSON(c.Params),
		Source:            in.Identity.Source,
		TriggeredByUserID: in.Identity.TriggeredByUserID,
		ModerationStatus:  in.Identity.ModerationFor(requireReview),
	}
	if iqbenchmark.IsImageCase(c.Kind) {
		return e.executeImageCase(ctx, in.Identity, base, c, started)
	}
	return e.executeTextCase(ctx, in.Identity, base, c, started)
}

// executeTextCase 执行文本 / 代码类题目。
func (e *Executor) executeTextCase(ctx context.Context, identity ExecutionIdentity, base ArtifactCreateInput, c *iqbenchmark.Case, started time.Time) ExecuteResult {
	resp, err := e.Gateway.Chat(ctx, ChatRequest{
		Model:        base.Model,
		Prompt:       c.Prompt,
		SystemPrompt: c.SystemPrompt,
		Params:       c.Params,
		APIKey:       identity.GatewayKey,
	})
	duration := e.now().Sub(started).Milliseconds()
	base.DurationMS = int(duration)

	if err != nil {
		status, code := classifyInvokeError(err)
		base.ErrorCode = code
		// error_kind 与 error_code 是两个维度：前者描述上游怎么坏的，
		// 后者是我们自己的失败原因（驱动熔断）。这里只补前者，不动后者。
		base.ErrorKind = iqbenchmark.ClassifyErrorKind(err, gatewayStatusCode(err))
		base.Verdict = iqbenchmark.VerdictForCase(status, c, "", false)
		base.RenderVerdict = iqbenchmark.VerdictForRender(status, c.ExpectKind, "", false)
		if _, saveErr := e.Artifacts.SaveFailedArtifact(ctx, base, status, code); saveErr != nil {
			e.Logger.Error("iqbenchmark: persist failed artifact failed",
				"run_id", base.RunID, "case_id", base.CaseID, "error", saveErr)
		}
		return ExecuteResult{Status: status, DurationMS: duration, ErrorCode: code,
			Verdict: base.Verdict, RenderVerdict: base.RenderVerdict, ErrorKind: base.ErrorKind}
	}
	base.InputTokens = resp.InputTokens
	base.OutputTokens = resp.OutputTokens

	renderKind := sanitize.Classify(sanitize.ClassifyInput{Text: resp.Text, ExpectKind: c.ExpectKind})
	base.RequestParams = withRequestID(base.RequestParams, resp.RequestID)
	// 判定必须在落库前算好：产出写入是唯一一次能同时看到
	// 「模型原文」与「题目 expected」的时机。
	hasArtifact := strings.TrimSpace(resp.Text) != ""
	base.Verdict = iqbenchmark.VerdictForCase(iqbenchmark.ArtifactStatusSuccess, c, resp.Text, hasArtifact)
	base.RenderVerdict = iqbenchmark.VerdictForRender(
		iqbenchmark.ArtifactStatusSuccess, c.ExpectKind, renderKind, hasArtifact)
	// 调用成功但内容可疑（空/被截断）时补 error_kind：这类问题拿不到状态码，
	// 只能看内容。它不改变 Status —— 有产出就是 success。
	base.ErrorKind = iqbenchmark.ErrorKindFromText(resp.Text, base.ContentTruncated)
	artifact, saveErr := e.Artifacts.SaveTextArtifact(ctx, base, renderKind, resp.Text)
	if saveErr != nil {
		e.Logger.Error("iqbenchmark: persist artifact failed",
			"run_id", base.RunID, "case_id", base.CaseID, "error", saveErr)
		return ExecuteResult{Status: iqbenchmark.ArtifactStatusFailed, DurationMS: duration, RequestID: resp.RequestID,
			Verdict: iqbenchmark.VerdictDegraded, RenderVerdict: iqbenchmark.RenderVerdictUnknown}
	}
	return ExecuteResult{Status: artifact.Status, DurationMS: duration, RequestID: resp.RequestID,
		Verdict: base.Verdict, RenderVerdict: base.RenderVerdict, ErrorKind: base.ErrorKind,
		ArtifactID: artifact.ID}
}

// executeImageCase 执行图片类题目。
//
// 图片字节由调用方注入的下载/上传链路处理（见 image_uploader 适配）；
// 这里只把网关返回的 URL / b64 记录为产出引用。
func (e *Executor) executeImageCase(ctx context.Context, identity ExecutionIdentity, base ArtifactCreateInput, c *iqbenchmark.Case, started time.Time) ExecuteResult {
	resp, err := e.Gateway.Image(ctx, ImageRequest{
		Model:  base.Model,
		Prompt: c.Prompt,
		Params: c.Params,
		APIKey: identity.GatewayKey,
	})
	duration := e.now().Sub(started).Milliseconds()
	base.DurationMS = int(duration)

	if err != nil {
		status, code := classifyInvokeError(err)
		base.ErrorCode = code
		base.ErrorKind = iqbenchmark.ClassifyErrorKind(err, gatewayStatusCode(err))
		base.Verdict = iqbenchmark.VerdictForCase(status, c, "", false)
		base.RenderVerdict = iqbenchmark.VerdictForRender(status, c.ExpectKind, "", false)
		if _, saveErr := e.Artifacts.SaveFailedArtifact(ctx, base, status, code); saveErr != nil {
			e.Logger.Error("iqbenchmark: persist failed image artifact failed",
				"run_id", base.RunID, "case_id", base.CaseID, "error", saveErr)
		}
		return ExecuteResult{Status: status, DurationMS: duration, ErrorCode: code,
			Verdict: base.Verdict, RenderVerdict: base.RenderVerdict, ErrorKind: base.ErrorKind}
	}

	base.RequestParams = withRequestID(base.RequestParams, resp.RequestID)
	if len(resp.URLs) == 0 && len(resp.B64) == 0 {
		// 模型没给任何图片：记为 no_artifact 失败（不是成功空产出）。
		code := iqbenchmark.ErrCodeNoArtifact
		base.ErrorCode = code
		base.ErrorKind = iqbenchmark.ErrorKindEmptyResponse
		base.Verdict = iqbenchmark.VerdictDegraded
		base.RenderVerdict = iqbenchmark.RenderVerdictUnknown
		if _, saveErr := e.Artifacts.SaveFailedArtifact(ctx, base, iqbenchmark.ArtifactStatusFailed, code); saveErr != nil {
			e.Logger.Error("iqbenchmark: persist no-artifact failure failed", "run_id", base.RunID, "error", saveErr)
		}
		return ExecuteResult{Status: iqbenchmark.ArtifactStatusFailed, DurationMS: duration, RequestID: resp.RequestID,
			ErrorCode: code, Verdict: base.Verdict, RenderVerdict: base.RenderVerdict, ErrorKind: base.ErrorKind}
	}

	// 首版只用 URL 直链（b64 需经 ImageResultUploader 转存，见 known limitations）。
	if len(resp.URLs) > 0 {
		// 图片题天然没有 expected：VerdictForCase 会给出 generated
		// （有产出但不判分），这正是「纯生成模式」的构成单元。
		base.Verdict = iqbenchmark.VerdictForCase(iqbenchmark.ArtifactStatusSuccess, c, "", true)
		base.RenderVerdict = iqbenchmark.VerdictForRender(
			iqbenchmark.ArtifactStatusSuccess, c.ExpectKind, iqbenchmark.RenderKindImageURL, true)
		saved, saveErr := e.Artifacts.SaveImageArtifact(ctx, base, resp.URLs[0], "")
		if saveErr != nil {
			e.Logger.Error("iqbenchmark: persist image artifact failed", "run_id", base.RunID, "error", saveErr)
			return ExecuteResult{Status: iqbenchmark.ArtifactStatusFailed, DurationMS: duration, RequestID: resp.RequestID,
				Verdict: iqbenchmark.VerdictDegraded, RenderVerdict: iqbenchmark.RenderVerdictUnknown}
		}
		return ExecuteResult{Status: iqbenchmark.ArtifactStatusSuccess, DurationMS: duration,
			RequestID: resp.RequestID, Verdict: base.Verdict, RenderVerdict: base.RenderVerdict,
			ArtifactID: saved.ID}
	}

	// 只有 b64：无对象存储时降级为 storage_disabled。
	code := iqbenchmark.ErrCodeStorageDisabled
	base.ErrorCode = code
	base.ErrorKind = iqbenchmark.ErrorKindGeneric
	base.Verdict = iqbenchmark.VerdictDegraded
	base.RenderVerdict = iqbenchmark.RenderVerdictUnknown
	if _, saveErr := e.Artifacts.SaveFailedArtifact(ctx, base, iqbenchmark.ArtifactStatusFailed, code); saveErr != nil {
		e.Logger.Error("iqbenchmark: persist storage-disabled failure failed", "run_id", base.RunID, "error", saveErr)
	}
	return ExecuteResult{Status: iqbenchmark.ArtifactStatusFailed, DurationMS: duration, RequestID: resp.RequestID,
		ErrorCode: code, Verdict: base.Verdict, RenderVerdict: base.RenderVerdict, ErrorKind: base.ErrorKind}
}

// RecordCaseFailure 记录一条失败产出（容量不足等前置失败）。
func (e *Executor) RecordCaseFailure(ctx context.Context, in ExecuteInput, status, code string, requireReview bool) {
	c := in.Case
	if c == nil {
		return
	}
	base := ArtifactCreateInput{
		RunID:             in.RunID,
		TaskID:            in.TaskID,
		CaseID:            c.ID,
		CaseSnapshot:      iqbenchmark.CaseSnapshot(c),
		GroupID:           in.GroupID,
		Model:             in.Model,
		RequestParams:     DecodeSafeJSON(c.Params),
		Source:            in.Identity.Source,
		TriggeredByUserID: in.Identity.TriggeredByUserID,
		ModerationStatus:  in.Identity.ModerationFor(requireReview),
	}
	if _, err := e.Artifacts.SaveFailedArtifact(ctx, base, status, code); err != nil {
		e.Logger.Error("iqbenchmark: record case failure failed",
			"run_id", in.RunID, "case_id", c.ID, "error", err)
	}
}

// classifyInvokeError 把调用错误映射为产出状态 + 稳定错误码。
//
// 这个函数是**熔断语义的唯一入口**（QuotaExhausted 依赖它给出的 code），
// 因此本轮改造刻意一行未改。新增的 12 类归一化走 classifyInvokeErrorKind，
// 两条路径互不影响 —— 这是「并存而不替代」的具体落点。
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

// gatewayStatusCode 从调用错误里取上游 HTTP 状态码（取不到返回 0）。
//
// 单独抽出来是因为 classifyInvokeError 与 ClassifyErrorKind 都需要它，
// 但二者的用途不同：前者决定熔断，后者只决定展示归类。
func gatewayStatusCode(err error) int {
	var non2xx *ErrGatewayNon2xx
	if errors.As(err, &non2xx) {
		return non2xx.Status
	}
	return 0
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
