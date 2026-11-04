// Package modeldegradation 实现「模型降智检测」。
//
// 与 iqbenchmark 是两套独立能力，刻意不合并：
//
//	iqbenchmark        定时任务 + 内部账号 + 产出进画廊 + 事后审核
//	modeldegradation   用户手动触发 + 用户自带上游凭据 + 结果即时返回
//
// 两者的数据主体都不同（artifacts vs runs），共用表只会让两边的语义互相
// 污染。唯一复用的是错误分类（12 类 ErrorKind）与 SVG 渲染口径。
//
// 安全前提（本包最重要的一条）：
//
//	用户填写的 base_url / api_key 不落库、不写日志、不回显。
//	base_url 只保存脱敏展示形态；api_key 只在单次请求生命周期内存在于内存。
//	详见 credentials.go 与 executor.go 的注释。
package modeldegradation

import (
	"time"
)

// 执行状态。三态，不含「排队」：本功能是用户当场发起，
// 没有调度中心的排队概念，发起即进入 running。
const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusCancelled = "cancelled"
)

// 运行级四态判定。
//
// generated 单独成一态而不是并入 normal：纯生成模式（只选了生成题）下
// logic_total=0，一道逻辑题都没跑，此时说「正常」或「降智」都没有依据。
// 把它判成 normal 会让通过率统计凭空变好。
const (
	VerdictNormal    = "normal"
	VerdictDegraded  = "degraded"
	VerdictUnknown   = "unknown"
	VerdictGenerated = "generated"
)

// 生成题判定，独立于逻辑题。
//
// 逻辑题问的是「答对没有」，生成题问的是「画出来没有」，两者不能混算。
// 空串表示本次没有跑生成题（与「跑了但没画出来」的 unknown 不同）。
const (
	RenderVerdictNone      = ""
	RenderVerdictOK        = "ok"
	RenderVerdictDegraded  = "degraded"
	RenderVerdictUnknown   = "unknown"
	RenderVerdictGenerated = "generated"
)

// 单题判定。
const (
	ItemVerdictOK        = "ok"
	ItemVerdictDegraded  = "degraded"
	ItemVerdictGenerated = "generated"
	ItemVerdictUnknown   = "unknown"
)

// 题目类型。
const (
	TaskTypeLogic  = "logic"
	TaskTypeRender = "render"
)

// 协议。三者的路径、认证头与思考等级字段都不同，见 protocol.go。
const (
	ProtocolChatCompletions = "chat_completions"
	ProtocolResponses       = "responses"
	ProtocolMessages        = "messages"
)

// 思考等级。空串 = 不指定。
const (
	EffortNone   = ""
	EffortLow    = "low"
	EffortMedium = "medium"
	EffortHigh   = "high"
	EffortXHigh  = "xhigh"
	EffortMax    = "max"
)

// 可见性配置的两种维度。
const (
	VisibilityKindGroup = "group"
	VisibilityKindHost  = "host"
)

// 内联自定义题在单题结果里的 task_key。
const CustomTaskKey = "custom"

// Protocols 是全部合法协议，顺序与前端下拉一致。
var Protocols = []string{ProtocolChatCompletions, ProtocolResponses, ProtocolMessages}

// ReasoningEfforts 是全部合法思考等级（不含空串）。
var ReasoningEfforts = []string{EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}

// RenderTimeoutChoices 是前端下拉提供的分钟档位。
// 服务端不限制只能取这些值，只校验区间（见 MinRenderTimeoutMinutes）。
var RenderTimeoutChoices = []int{5, 10, 15, 20, 30, 45, 60}

// 生成题超时（分钟）的合法区间。
const (
	MinRenderTimeoutMinutes = 1
	MaxRenderTimeoutMinutes = 60
	// DefaultRenderTimeoutMinutes 是前端默认档位。生成一幅带动画的 SVG
	// 往往要跑几分钟，太短会把「画得慢」误判成「没画出来」。
	DefaultRenderTimeoutMinutes = 15
)

// IsValidProtocol 判断协议是否合法。
func IsValidProtocol(p string) bool {
	switch p {
	case ProtocolChatCompletions, ProtocolResponses, ProtocolMessages:
		return true
	}
	return false
}

// IsValidReasoningEffort 判断思考等级是否合法（空串合法 = 不指定）。
func IsValidReasoningEffort(e string) bool {
	switch e {
	case EffortNone, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax:
		return true
	}
	return false
}

// SupportsReasoningEffort 判断某协议是否接受思考等级。
//
// Anthropic messages 协议没有 reasoning_effort 这个概念，传了也会被忽略。
// 这里显式判断而不是「你传我就发」，是为了让服务层能明确地不发这个字段，
// 避免上游因为多余字段返回 400 —— 那种失败会被错误归一化误判成
// forbidden/server_error，看起来像上游降智，实际是我们发错了。
func SupportsReasoningEffort(protocol string) bool {
	return protocol == ProtocolChatCompletions || protocol == ProtocolResponses
}

// SupportsRender 判断某协议是否适合跑生成题。
//
// chat_completions 是非流式请求：生成一幅 SVG 要跑几分钟，等待期间连接
// 上没有数据流动，容易被中间网关判成超时（表现为一直转圈、后台查不到用量）。
// 所以生成题不禁止但会给出建议，判定侧不因此扣分。
func SupportsRender(protocol string) bool {
	return protocol == ProtocolResponses || protocol == ProtocolMessages
}

// Task 是题库里的一道题。
type Task struct {
	ID        int64
	Type      string
	Key       string
	Title     string
	Prompt    string // 已是替换过占位符的最终 prompt
	Expected  string
	SortOrder int
}

// Run 是一次检测的完整记录（不含单题明细）。
type Run struct {
	ID         int64
	UserID     int64
	OwnerEmail string

	BaseURLDisplay       string
	ModelID              string
	Protocol             string
	ReasoningEffort      string
	RenderTimeoutSeconds int

	Status        string
	Verdict       string
	LogicPassed   int
	LogicTotal    int
	RenderVerdict string

	ErrorMessage string
	ErrorKind    string

	DurationMS  int64
	CreatedAt   time.Time
	CompletedAt *time.Time
	Retrying    bool

	GroupID   int64
	GroupName string

	IsOwner bool

	// ArtworkPreview 是本次检测首个生成题的作品源码，仅供历史墙缩略图使用。
	//
	// 只在列表查询里填充；过大的作品不会带上，避免单条记录把列表响应拖爆。
	ArtworkPreview string
}

// IsRunning 表示检测仍在进行。
func (r Run) IsRunning() bool {
	return r.Status == StatusRunning
}

// IsPureGeneration 表示本次只跑了生成题，没跑逻辑题。
//
// 判定口径的关键分支：这种情况 verdict 必须是 generated，不能是
// normal/degraded。前端据此显示「已生成」而不是「通过」。
func (r Run) IsPureGeneration() bool {
	return r.Verdict == VerdictGenerated && r.LogicTotal == 0
}

// Item 是一次检测里的单题结果。
type Item struct {
	ID    int64
	RunID int64
	Type  string
	Key   string

	Prompt   string
	Expected string
	Response string

	Passed  bool
	Verdict string
	Detail  string

	// Artwork 是抽出来的 SVG 源码。logic 题为空。
	Artwork string

	DurationMS int64
	SortOrder  int
}

// RunDetail 是 Run 加上单题明细，对应 GET /runs/:id。
type RunDetail struct {
	Run
	Items []Item
}

// StartRequest 是 POST /runs 的请求体。
//
// 注意 APIKey / BaseURL 是唯一会接触用户凭据的结构，且它们只在
// 服务层的单次调用内使用，不进入任何持久化结构（Run 里只有脱敏 display）。
type StartRequest struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	ModelID string `json:"model_id"`

	Protocol             string `json:"protocol"`
	ReasoningEffort      string `json:"reasoning_effort,omitempty"`
	RenderTimeoutSeconds int    `json:"render_timeout_seconds,omitempty"`

	// Tasks 是要跑的题型集合，取值 logic / render。
	Tasks []string `json:"tasks"`

	// Questions 是本次要跑的题目。可以是题库题（带 key），
	// 也可以是内联自定义题（task_type + prompt + expected）。
	Questions []Question `json:"questions"`
}

// Question 是一道待跑题目。来自题库时 Key 非空；内联自定义题 Key 为空。
//
// 自定义题允许不落库直接跑 —— 这是本功能与 iqbenchmark 的关键差异：
// 后者要求题目先入表（为了定时任务的横向可比性），而这里是一次性检测，
// 用户想临时问一道题是很自然的用法。
type Question struct {
	TaskType string `json:"task_type"`
	Key      string `json:"key,omitempty"`
	Title    string `json:"title,omitempty"`
	Prompt   string `json:"prompt"`
	Expected string `json:"expected,omitempty"`
}

// IsCustom 表示这是一道内联自定义题（没有题库 key）。
func (q Question) IsCustom() bool {
	return q.Key == ""
}

// TaskBank 是 GET /tasks 的响应体：两个题型分组。
type TaskBank struct {
	Logic  []Task `json:"logic"`
	Render []Task `json:"render"`
}

// Visibility 是管理员可见性配置的聚合视图。
type Visibility struct {
	HiddenGroupIDs []int64     `json:"hidden_group_ids"`
	HiddenHosts    []string    `json:"hidden_hosts"`
	Hosts          []HostUsage `json:"hosts"`
}

// HostUsage 是「未命中任何平台分组」的上游主机及其记录数。
type HostUsage struct {
	Host     string `json:"host"`
	RunCount int64  `json:"run_count"`
}

// RunFilter 是列表查询条件。
type RunFilter struct {
	Limit  int
	Offset int
	// UserID 非 0 时只看该用户的记录（「我的检测」）。
	UserID int64
	// Verdict / TaskType 为空表示不过滤。
	Verdict  string
	TaskType string
	// ExcludeGroupIDs / ExcludeHosts 是管理员隐藏的维度，公开侧必须排除。
	ExcludeGroupIDs []int64
	ExcludeHosts    []string
	// IncludeHidden 为 true 时不过滤隐藏项（管理侧使用）。
	IncludeHidden bool
}

// Stats 是页面顶部的三项统计。
type Stats struct {
	// PassRate 是「normal 数 ÷ 已完成数」，0~1。完全没有已完成记录时为 0。
	PassRate float64
	// LastDurationMS 是最近一次已完成检测的耗时。
	LastDurationMS int64
	// RunCount 是检测总次数。
	RunCount int64
}
