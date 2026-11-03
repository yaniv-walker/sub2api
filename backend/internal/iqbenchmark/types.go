// Package iqbenchmark 实现「模型智商检测作品展」（Pelican Benchmark 风格）。
//
// 设计目标：
//   - 按「分组 × 模型」定期让模型执行同一道检测题，把产出收集成可浏览、
//     可横向对比的作品展；
//   - 检测流量由独立内部虚拟账号承担，零影响真实用户的余额与配额；
//   - 功能默认关闭（settings.enabled=false 时零副作用）；
//   - 模型输出按不可信内容处理：入库前静态扫描 + 前端消毒 + 优先 <img> 渲染。
//
// 隔离原则：本包只依赖 service 包暴露的接口（不反向 import repository），
// 所有对既有系统的改动都通过 wire 的「注册一行」完成。
package iqbenchmark

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// 题目类型：决定调用哪个网关端点。
const (
	// CaseKindImage 走 /v1/images/generations。
	CaseKindImage = "image"
	// CaseKindText 与 CaseKindCode 走 /v1/chat/completions。
	CaseKindText = "text"
	// CaseKindCode 与 CaseKindText 同端点，仅语义区分。
	CaseKindCode = "code"
)

// 期望输出形态。
const (
	ExpectKindImageURL = "image_url"
	ExpectKindSVG      = "svg"
	ExpectKindMarkdown = "markdown"
	ExpectKindText     = "text"
)

// 评分方式。首版不实现 LLM 裁判，llm_judge 恒不参与执行。
const (
	ScoringManual     = "manual"
	ScoringRule       = "rule"
	ScoringManualRule = "manual_rule"
	ScoringLLMJudge   = "llm_judge"
)

// 任务运行状态。
const (
	TaskStatusIdle            = "idle"
	TaskStatusRunning         = "running"
	TaskStatusQuotaExhausted  = "quota_exhausted"
	TaskStatusBudgetExhausted = "budget_exhausted"
	TaskStatusAccountDisabled = "account_disabled"
	TaskStatusPausedByAdmin   = "paused_by_admin"
	// TaskStatusInvalidTarget 表示任务的「分组 + 模型」目标已失效
	// （分组被禁用/删除，或模型已从该分组的可用集合中下线）。
	//
	// 与既有状态的区别：它是**配置失效**而非配额/账号问题，恢复必须靠管理员
	// 改配置（或重新启用任务），因此调度器不会自动重试，而是清空 next_run_at 停下来。
	TaskStatusInvalidTarget = "invalid_target"
)

// TaskStatusNeedsManualRecovery 判定某个任务状态是否只能靠管理员手动恢复。
//
// 用于 SetTaskEnabled 的「重新启用时清熔断状态」逻辑：这些状态都是「停下来等人工」，
// 一旦任务被重新启用，就应回到 idle 重新排队。
func TaskStatusNeedsManualRecovery(status string) bool {
	switch status {
	case TaskStatusQuotaExhausted, TaskStatusBudgetExhausted,
		TaskStatusAccountDisabled, TaskStatusInvalidTarget:
		return true
	default:
		return false
	}
}

// 执行触发方式。
const (
	RunTriggerSchedule = "schedule"
	RunTriggerManual   = "manual"
	RunTriggerRetry    = "retry"
)

// 产出来源。决定统计口径与作品展上的「用户自测」标记。
const (
	// SourceScheduled 是定时检测链路（内部账号）。
	SourceScheduled = "scheduled"
	// SourceUserTest 是用户用自己本站 API Key 发起的单次自测。
	SourceUserTest = "user_test"
	// SourceAll 只用于**查询参数**表示「不过滤」，不是合法的存储值。
	SourceAll = "all"
)

// ValidStoredSource 判断某个来源是否可以作为**存储值**。
//
// 刻意排除 SourceAll：它只在查询参数里有意义，若被写进数据库，
// 按来源统计将无法区分真实链路。
func ValidStoredSource(source string) bool {
	switch source {
	case SourceScheduled, SourceUserTest:
		return true
	default:
		return false
	}
}

// NormalizeSourceFilter 把查询参数归一化为三值词汇：scheduled / user_test / all。
//
// 空值与非法值都归一化为 scheduled（默认口径、fail-closed）：
// 旧链接里可能带着过期参数，让它静默降级比给用户 400 更友好，
// 但**绝不会**降级成全量 —— 那会让统计口径被意外放大。
//
// 注意返回值不是「仓储层的过滤值」：all 需要由调用方翻译成空字符串
// （空 = 不过滤）。刻意不在这里折叠成 ""，否则「没传参」与「传了 all」
// 将无法区分，而它们的语义正好相反。
func NormalizeSourceFilter(source string) string {
	switch strings.TrimSpace(strings.ToLower(source)) {
	case SourceUserTest:
		return SourceUserTest
	case SourceAll:
		return SourceAll
	default:
		return SourceScheduled
	}
}

// SourceFilterToRepo 把三值词汇翻译成仓储层的过滤值：all -> ""（不过滤）。
func SourceFilterToRepo(source string) string {
	if NormalizeSourceFilter(source) == SourceAll {
		return ""
	}
	return NormalizeSourceFilter(source)
}

// 内容审核状态。定时检测默认 approved（内部账号可信），
// 自测按配置写 pending。
const (
	ModerationApproved = "approved"
	ModerationPending  = "pending"
	ModerationRejected = "rejected"
)

// 自测请求状态。
const (
	UserTestStatusPending  = "pending"
	UserTestStatusRunning  = "running"
	UserTestStatusSuccess  = "success"
	UserTestStatusFailed   = "failed"
	UserTestStatusBlocked  = "blocked"
	UserTestStatusRejected = "rejected"
)

// Run 状态。
const (
	RunStatusRunning = "running"
	RunStatusSuccess = "success"
	RunStatusPartial = "partial"
	RunStatusFailed  = "failed"
)

// 产出状态。失败 / 超时 / 被拦截的产出同样入库并在作品展展示为占位卡片。
const (
	ArtifactStatusSuccess = "success"
	ArtifactStatusFailed  = "failed"
	ArtifactStatusTimeout = "timeout"
	ArtifactStatusBlocked = "blocked"
)

// 渲染形态。
const (
	RenderKindImageURL = "image_url"
	RenderKindSVG      = "svg"
	RenderKindMarkdown = "markdown"
	RenderKindText     = "text"
)

// 产出判定（item verdict）。与 Status 正交：
//
//	Status  说「这次调用跑成没跑成」；
//	Verdict 说「模型答得对不对」。
//
// 二者必须分开。一次调用可以既 success 又 degraded（返回 200、有正文、
// 但答案与 expected 不符）—— 旧模型把这两种信息压在 status 一个字段里，
// 结果是通过率只能表达「调用成功率」，无法表达「模型变笨了」。
const (
	// VerdictOK 判分通过。
	VerdictOK = "ok"
	// VerdictDegraded 有判定依据且判分未通过，或根本没有产出。
	VerdictDegraded = "degraded"
	// VerdictGenerated 有产出，但本题没有判定依据（图片题 / 无 expected）。
	// 它是「不能判定」，不是「判定为好」—— 进分母，不进分子。
	VerdictGenerated = "generated"
	// VerdictUnknown 连产出都没有，无从判定。
	VerdictUnknown = "unknown"
)

// Run 级判定（四态）。由该 run 下所有产出的 verdict 汇总得出。
const (
	// RunVerdictNormal 全部可判题目都通过。
	RunVerdictNormal = "normal"
	// RunVerdictDegraded 存在判分未通过或调用失败的题目。
	RunVerdictDegraded = "degraded"
	// RunVerdictUnknown 没有一道题可判（例如题目全被删除）。
	RunVerdictUnknown = "unknown"
	// RunVerdictGenerated 纯生成模式：本次只出图不判分（logic_total == 0）。
	RunVerdictGenerated = "generated"
)

// 渲染判定（render_verdict）。与 verdict 独立：
// 答案对但渲染形态坏掉（例如该出 SVG 却吐了纯文本），是两类不同的降级。
const (
	RenderVerdictOK        = VerdictOK
	RenderVerdictDegraded  = VerdictDegraded
	RenderVerdictGenerated = VerdictGenerated
	RenderVerdictUnknown   = VerdictUnknown
)

// 错误归类（error_kind）：面向「上游错误形态」的 12 类归一化。
//
// 与既有的 error_code 是**两个维度**，并存而不替代：
//
//	error_code 是「我们自己的失败原因」，被 QuotaExhausted 用来驱动熔断；
//	error_kind 是「上游怎么坏的」，只用于展示与统计。
//
// 之所以不能合并：把 error_kind 塞进 error_code 会改动
// ExecuteResult.QuotaExhausted 的判定，进而改掉限流/熔断语义。
// 存成两列，等于只增不改。
const (
	ErrorKindAuthFailed         = "auth_failed"
	ErrorKindForbidden          = "forbidden"
	ErrorKindNotFound           = "not_found"
	ErrorKindTimeout            = "timeout"
	ErrorKindRateLimited        = "rate_limited"
	ErrorKindGatewayTimeout     = "gateway_timeout"
	ErrorKindServerError        = "server_error"
	ErrorKindGeneric            = "generic"
	ErrorKindStreamBroken       = "stream_broken"
	ErrorKindResponseIncomplete = "response_incomplete"
	ErrorKindQuotaExceeded      = "quota_exceeded"
	ErrorKindEmptyResponse      = "empty_response"
)

// ErrorKinds 是全部 12 类的稳定清单。
//
// 单测用它断言「列出来的都被处理、没列出来的不会出现」，
// 避免 switch 里悄悄多出一个类而迁移的 CHECK 拦不住。
var ErrorKinds = []string{
	ErrorKindAuthFailed,
	ErrorKindForbidden,
	ErrorKindNotFound,
	ErrorKindTimeout,
	ErrorKindRateLimited,
	ErrorKindGatewayTimeout,
	ErrorKindServerError,
	ErrorKindGeneric,
	ErrorKindStreamBroken,
	ErrorKindResponseIncomplete,
	ErrorKindQuotaExceeded,
	ErrorKindEmptyResponse,
}

// 内部错误码。这些码只在管理端可见，公开侧只下发脱敏后的 error_summary。
const (
	ErrCodeNoCases             = "no_cases"
	ErrCodeGroupMissing        = "group_missing"
	ErrCodeUpstreamError       = "upstream_error"
	ErrCodeRateLimited         = "rate_limited"
	ErrCodeTimeout             = "timeout"
	ErrCodeContentBlocked      = "content_blocked"
	ErrCodeInsufficientBalance = "insufficient_balance"
	ErrCodeUnsafeContent       = "unsafe_content"
	ErrCodeNoArtifact          = "no_artifact"
	ErrCodeStorageDisabled     = "storage_disabled"
	ErrCodeTruncated           = "truncated"
	ErrCodeGatewayUnreachable  = "gateway_unreachable"
	ErrCodeCapabilityMissing   = "capability_missing"
)

// 检测调用的固定 User-Agent：用于在既有 usage_log.user_agent 上做二次识别，
// 与内部账号 user_id / 专用 api_key_id 一起构成检测流量的精确标识。
const DetectionUserAgent = "sub2api-iq-benchmark/1.0"

// 领域错误：全部为稳定错误码，handler 层映射为 HTTP 状态码与业务码。
var (
	// ErrCaseNotFound 题目不存在。
	ErrCaseNotFound = errors.New("iqbenchmark: case not found")
	// ErrCaseBuiltinUndeletable 内置题不可删除。
	ErrCaseBuiltinUndeletable = errors.New("iqbenchmark: builtin case cannot be deleted")
	// ErrCaseInUse 题目被启用中的任务引用。
	ErrCaseInUse = errors.New("iqbenchmark: case is referenced by an enabled task")
	// ErrCaseInvalid 题目字段非法。
	ErrCaseInvalid = errors.New("iqbenchmark: invalid case")

	// ErrTaskNotFound 任务不存在。
	ErrTaskNotFound = errors.New("iqbenchmark: task not found")
	// ErrTaskInvalid 任务字段非法。
	ErrTaskInvalid = errors.New("iqbenchmark: invalid task")
	// ErrTaskNoCases 任务没有关联任何题目。
	ErrTaskNoCases = errors.New("iqbenchmark: task must reference at least one case")
	// ErrTaskGroupMissing 任务未指定目标分组。
	ErrTaskGroupMissing = errors.New("iqbenchmark: task must reference an existing group")
	// ErrTaskDuplicateEnabled 同「分组 × 模型」已存在启用中的任务。
	ErrTaskDuplicateEnabled = errors.New("iqbenchmark: an enabled task already exists for this group and model")

	// ErrTargetGroupUnavailable 目标分组不存在或不是 active 状态。
	ErrTargetGroupUnavailable = errors.New("iqbenchmark: target group is missing or inactive")
	// ErrTargetModelUnavailable 所选模型不在该分组当前可调用的模型集合内。
	ErrTargetModelUnavailable = errors.New("iqbenchmark: target model is not available for the selected group")
	// ErrTargetCapabilityMismatch 所选目标不具备任务题目类型所需的能力（例如含图片题但分组禁止生图）。
	ErrTargetCapabilityMismatch = errors.New("iqbenchmark: target does not support the capabilities required by the selected cases")

	// ErrRunNotFound 执行记录不存在。
	ErrRunNotFound = errors.New("iqbenchmark: run not found")
	// ErrArtifactNotFound 产出不存在。
	ErrArtifactNotFound = errors.New("iqbenchmark: artifact not found")

	// ErrFeatureDisabled 功能未启用。
	ErrFeatureDisabled = errors.New("iqbenchmark: feature is not enabled")
	// ErrNoTargetGroup 未配置可用的目标分组，内部账号无法建 Key。
	ErrNoTargetGroup = errors.New("iqbenchmark: no target group configured for internal agent")

	// ErrPageSizeTooLarge 单页条数超过上限。
	ErrPageSizeTooLarge = errors.New("iqbenchmark: page_size exceeds the maximum allowed")
	// ErrLeaseLost 租约已被其他实例接管，本轮必须中止。
	ErrLeaseLost = errors.New("iqbenchmark: task lease was lost to another instance")
	// ErrCapacityExceeded 产出总量超过容量硬上限，拒绝写入。
	ErrCapacityExceeded = errors.New("iqbenchmark: artifact capacity exceeded")

	// ErrDeadlineExceeded 单次调用超时（产出记 status=timeout）。
	ErrDeadlineExceeded = errors.New("iqbenchmark: model call timed out")
	// ErrGatewayUnreachable 网关不可达（记为暂不可用，不触发熔断）。
	ErrGatewayUnreachable = errors.New("iqbenchmark: gateway unreachable")
	// ErrAccountDisabled 内部账号已不可用（被风控封禁或被管理员禁用）。
	ErrAccountDisabled = errors.New("iqbenchmark: internal agent is disabled")
	// ErrBudgetExhausted 当日成本预算已用尽。
	ErrBudgetExhausted = errors.New("iqbenchmark: daily cost budget exhausted")

	// ErrGatewayKeyMissing 执行身份缺少调用网关所需的 Key。
	//
	// 刻意与 ErrInternalAgentMissing 分开：那个错误表达的是「内部账号没初始化」，
	// 属于调度链路的装配问题；这个表达的是「这次执行没拿到凭据」，
	// 两条链路都可能遇到。执行器只会返回后者 —— 它不认识「内部账号」这个概念。
	ErrGatewayKeyMissing = errors.New("iqbenchmark: gateway key is missing for this execution")

	// ---- 用户自测 ----

	// ErrUserTestDisabled 自测开关未开启（与功能总开关分开，便于分别提示）。
	ErrUserTestDisabled = errors.New("iqbenchmark: user self-test is not enabled")
	// ErrCaseNotOpenForUserTest 该题目未开放自测。
	ErrCaseNotOpenForUserTest = errors.New("iqbenchmark: this case is not open for user self-test")
	// ErrUserNotActive 用户状态不是 active。
	ErrUserNotActive = errors.New("iqbenchmark: user is not active")
	// ErrAPIKeyNotOwned Key 不存在或不属于当前用户。
	//
	// 刻意不区分「不存在」与「不属于你」：区分会泄露他人 Key 的存在性。
	ErrAPIKeyNotOwned = errors.New("iqbenchmark: api key not found")
	// ErrAPIKeyInactive Key 已被禁用。
	ErrAPIKeyInactive = errors.New("iqbenchmark: api key is inactive")
	// ErrAPIKeyExpired Key 已过期。
	ErrAPIKeyExpired = errors.New("iqbenchmark: api key has expired")
	// ErrAPIKeyQuotaExhausted Key 自身额度已用尽。
	ErrAPIKeyQuotaExhausted = errors.New("iqbenchmark: api key quota is exhausted")
	// ErrGroupImageGenerationDisabled 该分组未开放图片生成，无法跑图片题。
	ErrGroupImageGenerationDisabled = errors.New("iqbenchmark: image generation is not allowed for this group")
	// ErrUserTestDailyLimit 当日自测次数已达上限。
	ErrUserTestDailyLimit = errors.New("iqbenchmark: daily self-test limit reached")
	// ErrUserTestConcurrent 在途自测数量已达上限。
	ErrUserTestConcurrent = errors.New("iqbenchmark: too many self-tests in flight")
	// ErrUserTestDuplicate 去重窗口内已有完全相同的请求。
	ErrUserTestDuplicate = errors.New("iqbenchmark: an identical self-test was submitted recently")
	// ErrUserTestBudgetExceeded 当日自测成本预算已用尽。
	ErrUserTestBudgetExceeded = errors.New("iqbenchmark: daily self-test cost budget exhausted")
	// ErrUserTestNotFound 自测记录不存在或不属于当前用户。
	ErrUserTestNotFound = errors.New("iqbenchmark: self-test not found")
)

// UsageSnapshotByRequestIDs 是按 request_id 汇总的用量快照。
type UsageSnapshotByRequestIDs struct {
	Cost         float64
	InputTokens  int64
	OutputTokens int64
	Matched      int64
}

// Case 是检测题目。
type Case struct {
	ID           int64          `json:"id"`
	Name         string         `json:"name"`
	Kind         string         `json:"kind"`
	Prompt       string         `json:"prompt"`
	SystemPrompt string         `json:"system_prompt"`
	Params       map[string]any `json:"params"`
	ExpectKind   string         `json:"expect_kind"`
	// Expected 是 logic 类题目的参考答案。
	//
	// 空字符串表示「本题不可判分」（图片题、开放式生成题都是这种）。
	// 非空时执行侧按归一化后的等值比较给出 verdict；
	// 自定义 logic 题必须非空，否则判不了，见 validateCase。
	Expected        string `json:"expected"`
	Scoring         string `json:"scoring"`
	LLMJudgeEnabled bool   `json:"llm_judge_enabled"`
	Builtin         bool   `json:"builtin"`
	Enabled         bool   `json:"enabled"`
	// AllowUserTest 决定该题是否开放给用户自测。
	//
	// 默认 false：开放是逐个题目的显式决定，因为自测消耗的是**用户自己的额度**，
	// 而图片题这类昂贵题目的单次成本远高于文本题。
	AllowUserTest bool      `json:"allow_user_test"`
	SortOrder     int       `json:"sort_order"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Task 是按「分组 × 模型」定期执行的检测任务。
type Task struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	GroupID        int64      `json:"group_id"`
	Model          string     `json:"model"`
	CaseIDs        []int64    `json:"case_ids"`
	IntervalMinute int        `json:"interval_minutes"`
	JitterSeconds  int        `json:"jitter_seconds"`
	Enabled        bool       `json:"enabled"`
	Status         string     `json:"status"`
	StatusReason   string     `json:"status_reason"`
	NextRunAt      *time.Time `json:"next_run_at"`
	LastRunAt      *time.Time `json:"last_run_at"`
	LeaseOwner     string     `json:"-"`
	LeaseExpiresAt *time.Time `json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Run 是检测任务的一次执行记录。
type Run struct {
	ID                int64      `json:"id"`
	TaskID            int64      `json:"task_id"`
	GroupID           int64      `json:"-"`
	Model             string     `json:"model"`
	Trigger           string     `json:"trigger"`
	StartedAt         time.Time  `json:"started_at"`
	FinishedAt        *time.Time `json:"finished_at"`
	SuccessCount      int        `json:"success_count"`
	FailedCount       int        `json:"failed_count"`
	TimeoutCount      int        `json:"timeout_count"`
	BlockedCount      int        `json:"blocked_count"`
	TotalDurationMS   int64      `json:"total_duration_ms"`
	TotalCost         float64    `json:"-"`
	TotalInputTokens  int64      `json:"-"`
	TotalOutputTokens int64      `json:"-"`
	InternalUserID    int64      `json:"-"`
	InternalAPIKeyID  int64      `json:"-"`
	Status            string     `json:"status"`
	// Verdict 是本次运行的汇总判定（normal / degraded / unknown / generated）。
	Verdict string `json:"verdict"`
	// LogicTotal / LogicPassed 是「可判题目」的分母与分子。
	//
	// 与 SuccessCount 等的区别：后者数的是调用成败，
	// 这里数的是判定结果。纯生成模式（只出图）下 LogicTotal 为 0。
	LogicTotal      int        `json:"logic_total"`
	LogicPassed     int        `json:"logic_passed"`
	CostCollectedAt *time.Time `json:"-"`
	CreatedAt       time.Time  `json:"created_at"`
}

// Artifact 是单次执行的产出。
type Artifact struct {
	ID int64 `json:"id"`
	// RunID / TaskID 对用户自测为 0（对应数据库 NULL）：
	// 自测既不是定时任务，也没有 run。用 0 而不是伪造值表达「不存在」。
	RunID            int64          `json:"run_id"`
	TaskID           int64          `json:"task_id"`
	CaseID           int64          `json:"case_id"`
	CaseSnapshot     map[string]any `json:"-"`
	GroupID          int64          `json:"-"`
	Model            string         `json:"model"`
	RequestParams    map[string]any `json:"-"`
	RenderKind       string         `json:"render_kind"`
	ImageURL         string         `json:"image_url"`
	Content          string         `json:"content"`
	ContentTruncated bool           `json:"content_truncated"`
	ContentSHA256    string         `json:"-"`
	UnsafeFlags      []string       `json:"-"`
	RawResponseRef   string         `json:"-"`
	DurationMS       int            `json:"duration_ms"`
	InputTokens      int            `json:"-"`
	OutputTokens     int            `json:"-"`
	Cost             float64        `json:"-"`
	Status           string         `json:"status"`
	// Verdict 是本题判定（ok / degraded / generated / unknown）。
	Verdict string `json:"verdict"`
	// RenderVerdict 是渲染形态判定，与 Verdict 独立。
	RenderVerdict string `json:"render_verdict"`
	// ErrorKind 是面向上游错误形态的 12 类归一化，与 ErrorCode 并存。
	ErrorKind    string     `json:"error_kind"`
	ErrorCode    string     `json:"-"`
	ErrorSummary string     `json:"-"`
	Score        *float64   `json:"score"`
	ScoredBy     *int64     `json:"-"`
	ScoredAt     *time.Time `json:"-"`
	BlobKey      string     `json:"-"`
	CreatedAt    time.Time  `json:"created_at"`
	// Source 是来源标记（scheduled / user_test）。
	Source string `json:"source"`
	// TriggeredByUserID 是触发自测的用户；定时检测为 0（对应 NULL）。
	TriggeredByUserID int64  `json:"-"`
	ModerationStatus  string `json:"-"`
	// ReviewedBy / ReviewedAt 是审核留痕；从未人工审核时分别为 0 / nil。
	//
	// 只走管理侧 DTO，公开侧不下发（审核人身份属于管理信息）。
	ReviewedBy int64      `json:"-"`
	ReviewedAt *time.Time `json:"-"`
}

// ArtifactListFilter 是作品列表的筛选条件。
type ArtifactListFilter struct {
	GroupID *int64
	Model   string
	CaseID  *int64
	Status  string
	Kind    string
	Since   *time.Time
	Until   *time.Time
	// Source 为空表示不过滤（对应查询参数 source=all）。
	Source string
	// ModerationStatus 为空表示不过滤；公开侧传 approved。
	ModerationStatus string
	Page             int
	PageSize         int
}

// ArtifactListResult 是作品列表的分页结果。
type ArtifactListResult struct {
	Items    []Artifact `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

// OverviewBucket 是「分组 × 模型」维度的聚合行。
type OverviewBucket struct {
	GroupID      int64      `json:"group_id"`
	GroupName    string     `json:"group_name"`
	Model        string     `json:"model"`
	Total        int64      `json:"total"`
	SuccessCount int64      `json:"success_count"`
	FailedCount  int64      `json:"failed_count"`
	AvgDurationM float64    `json:"avg_duration_ms"`
	LastRunAt    *time.Time `json:"last_run_at"`
}

// Overview 是作品展总览。
type Overview struct {
	TotalArtifacts int64            `json:"total_artifacts"`
	ImageArtifacts int64            `json:"image_artifacts"`
	TextArtifacts  int64            `json:"text_artifacts"`
	CaseCount      int64            `json:"case_count"`
	TaskCount      int64            `json:"task_count"`
	RunningTasks   int64            `json:"running_tasks"`
	Buckets        []OverviewBucket `json:"buckets"`
}

// CaseSnapshot 把题目冻结成快照，写入每条产出行。
//
// 这是「同题多模型对比」正确性的前提：题目后续被编辑或删除，
// 历史产出仍可按当时题面回溯。
func CaseSnapshot(c *Case) map[string]any {
	if c == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":                c.ID,
		"name":              c.Name,
		"kind":              c.Kind,
		"prompt":            c.Prompt,
		"system_prompt":     c.SystemPrompt,
		"params":            c.Params,
		"expect_kind":       c.ExpectKind,
		"scoring":           c.Scoring,
		"llm_judge_enabled": false, // 首版恒为关：快照不承诺裁判能力
	}
}

// IsImageCase 判断题目是否走图片端点。
func IsImageCase(kind string) bool {
	return kind == CaseKindImage
}

// DecodeCaseIDs 解析 JSONB 数组形式的题目 ID 列表。
// 原生 SQL 扫描出来的 jsonb 是原始字节流，需在仓储层解成 []int64。
func DecodeCaseIDs(raw []byte) ([]int64, error) {
	if len(raw) == 0 {
		return []int64{}, nil
	}
	var ids []int64
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, fmt.Errorf("iqbenchmark: decode case_ids: %w", err)
	}
	if ids == nil {
		return []int64{}, nil
	}
	return ids, nil
}

// ValidCaseKind 判断题目类型是否合法。
func ValidCaseKind(kind string) bool {
	switch kind {
	case CaseKindImage, CaseKindText, CaseKindCode:
		return true
	default:
		return false
	}
}

// ValidExpectKind 判断期望输出形态是否合法。
func ValidExpectKind(kind string) bool {
	switch kind {
	case ExpectKindImageURL, ExpectKindSVG, ExpectKindMarkdown, ExpectKindText:
		return true
	default:
		return false
	}
}

// ValidScoring 判断评分方式是否合法。
func ValidScoring(scoring string) bool {
	switch scoring {
	case ScoringManual, ScoringRule, ScoringManualRule, ScoringLLMJudge:
		return true
	default:
		return false
	}
}

// ValidRenderKind 判断渲染形态是否合法。
func ValidRenderKind(kind string) bool {
	switch kind {
	case RenderKindImageURL, RenderKindSVG, RenderKindMarkdown, RenderKindText:
		return true
	default:
		return false
	}
}
