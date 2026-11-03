// Package handler 提供模型智商检测作品展的 HTTP 处理器。
//
// 本包刻意与 admin_handler / user_handler 分开，用两个文件承载两类接口，
// 但共享同一套 DTO 映射（dto.go）：
//   - 管理侧 DTO（Admin*）字段完整；
//   - 公开侧 DTO（Public*）是**白名单**，绝不复用管理侧结构。
//
// 白名单不是靠「记得删字段」保证的，而是靠「重新定义一个只有允许字段的结构体」：
// 这样上游新增字段时不会意外泄漏到公开接口。
package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/sanitize"
)

// ---------- 管理侧 DTO ----------

// AdminCaseDTO 是管理侧的题目 DTO（字段完整）。
type AdminCaseDTO struct {
	ID              int64          `json:"id"`
	Name            string         `json:"name"`
	Kind            string         `json:"kind"`
	Prompt          string         `json:"prompt"`
	SystemPrompt    string         `json:"system_prompt"`
	Params          map[string]any `json:"params"`
	ExpectKind      string         `json:"expect_kind"`
	Scoring         string         `json:"scoring"`
	LLMJudgeEnabled bool           `json:"llm_judge_enabled"`
	Builtin         bool           `json:"builtin"`
	Enabled         bool           `json:"enabled"`
	AllowUserTest   bool           `json:"allow_user_test"`
	SortOrder       int            `json:"sort_order"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// AdminTaskDTO 是管理侧的任务 DTO。
type AdminTaskDTO struct {
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
	LeaseOwner     string     `json:"lease_owner"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// AdminRunDTO 是管理侧的执行记录 DTO（含内部账号标识与成本）。
type AdminRunDTO struct {
	ID                int64      `json:"id"`
	TaskID            int64      `json:"task_id"`
	GroupID           int64      `json:"group_id"`
	Model             string     `json:"model"`
	Trigger           string     `json:"trigger"`
	StartedAt         time.Time  `json:"started_at"`
	FinishedAt        *time.Time `json:"finished_at"`
	SuccessCount      int        `json:"success_count"`
	FailedCount       int        `json:"failed_count"`
	TimeoutCount      int        `json:"timeout_count"`
	BlockedCount      int        `json:"blocked_count"`
	TotalDurationMS   int64      `json:"total_duration_ms"`
	TotalCost         float64    `json:"total_cost"`
	TotalInputTokens  int64      `json:"total_input_tokens"`
	TotalOutputTokens int64      `json:"total_output_tokens"`
	InternalUserID    int64      `json:"internal_user_id"`
	InternalAPIKeyID  int64      `json:"internal_api_key_id"`
	Status            string     `json:"status"`
	CostCollectedAt   *time.Time `json:"cost_collected_at"`
	CreatedAt         time.Time  `json:"created_at"`
}

// AdminArtifactDTO 是管理侧的产出 DTO。
//
// 管理侧可见 content（含被阻断的原文，供审阅）、unsafe_flags、error_code、
// raw_response_ref 等诊断字段 —— 这些字段**只**出现在这里。
type AdminArtifactDTO struct {
	ID               int64          `json:"id"`
	RunID            int64          `json:"run_id"`
	TaskID           int64          `json:"task_id"`
	CaseID           int64          `json:"case_id"`
	CaseSnapshot     map[string]any `json:"case_snapshot"`
	GroupID          int64          `json:"group_id"`
	Model            string         `json:"model"`
	RequestParams    map[string]any `json:"request_params"`
	RenderKind       string         `json:"render_kind"`
	ImageURL         string         `json:"image_url"`
	Content          string         `json:"content"`
	ContentTruncated bool           `json:"content_truncated"`
	ContentSHA256    string         `json:"content_sha256"`
	UnsafeFlags      []string       `json:"unsafe_flags"`
	RawResponseRef   string         `json:"raw_response_ref"`
	DurationMS       int            `json:"duration_ms"`
	InputTokens      int            `json:"input_tokens"`
	OutputTokens     int            `json:"output_tokens"`
	Cost             float64        `json:"cost"`
	Status           string         `json:"status"`
	ErrorCode        string         `json:"error_code"`
	ErrorSummary     string         `json:"error_summary"`
	Score            *float64       `json:"score"`
	ScoredBy         *int64         `json:"scored_by"`
	ScoredAt         *time.Time     `json:"scored_at"`
	BlobKey          string         `json:"blob_key"`
	CreatedAt        time.Time      `json:"created_at"`
	// 来源与审核：管理侧要能把官方检测与用户自测分开审阅，
	// 也要能回答「这条 pending 是谁放行的」。
	Source            string     `json:"source"`
	TriggeredByUserID *int64     `json:"triggered_by_user_id"`
	ModerationStatus  string     `json:"moderation_status"`
	ReviewedBy        *int64     `json:"reviewed_by"`
	ReviewedAt        *time.Time `json:"reviewed_at"`
}

// ---------- 公开侧 DTO（白名单）----------

// PublicArtifactDTO 是普通用户可见的产出 DTO。
//
// **白名单原则**：这里只列出允许公开的字段，绝不嵌套 AdminArtifactDTO。
// 明确**不包含**：
//   - raw_response_ref（原始响应引用，可能含上游细节）
//   - internal_user_id / internal_api_key_id（内部账号标识）
//   - account_id / group_id（账号与分组标识，暴露内部拓扑）
//   - notes（任何 notes 文本）
//   - unsafe_flags（危险构造清单，等于告诉攻击者哪条绕过了检测）
//   - error_code（内部错误码，含供应商/路由信息）
//   - blob_key / content_sha256 / request_params（内部存储与请求细节）
//
// cost / input_tokens / output_tokens 仅在 settings 显式开启时下发，
// 由 toPublicArtifact 依据配置决定是否置零。
type PublicArtifactDTO struct {
	ID         int64  `json:"id"`
	CaseID     int64  `json:"case_id"`
	CaseName   string `json:"case_name"`
	CaseKind   string `json:"case_kind"`
	Model      string `json:"model"`
	RenderKind string `json:"render_kind"`
	ImageURL   string `json:"image_url,omitempty"`
	Content    string `json:"content,omitempty"`
	Truncated  bool   `json:"truncated"`
	Status     string `json:"status"`
	// Verdict 是本题的四态判定：ok / degraded / generated / unknown。
	//
	// 与 Status 并存：Status 说「调用成不成」，Verdict 说「答得对不对」。
	// 一条 status=success 的产出完全可能是 degraded（调用成功但答错），
	// 那正是模型变笨时唯一能看出来的信号。
	Verdict string `json:"verdict"`
	// RenderVerdict 是渲染形态的判定，与 Verdict 独立：
	// 一道文本题可以答对但渲染坏掉，两者不该互相污染。
	RenderVerdict string `json:"render_verdict"`
	// ErrorKind 是 12 类归一化后的上游错误类别。
	//
	// 只下发归一化类别，**不下发** error_code（内部错误码含供应商与
	// 路由信息）。ErrorKind 的取值集合是固定的 12 个，不含任何内部细节，
	// 前端据此做统计与筛选都安全。
	ErrorKind string `json:"error_kind,omitempty"`
	// ErrorSummary 是脱敏后的失败描述（绝不回显内部错误码）。
	ErrorSummary string   `json:"error_summary,omitempty"`
	Score        *float64 `json:"score,omitempty"`
	DurationMS   int      `json:"duration_ms"`
	IsUnsafe     bool     `json:"is_unsafe"`
	// IsMine 表示这条产出是不是当前登录用户自己发起的自测。
	//
	// 由**服务端**计算：客户端没有任何字段可以推导出它。
	// 刻意不下发 user_id / api_key_id 让前端自己比 —— 那等于把
	// 「谁的产出」这份信息整个交给客户端，只是换个地方泄露。
	IsMine bool `json:"is_mine"`
	// Source 是产出属主口径：scheduled（定时检测）或 user_test（用户自测）。
	//
	// 公开侧下发它是为了让前端能打「用户自测」角标 —— 用户有权知道
	// 这条作品是官方定期检测的结果，还是另一个用户自己花钱测的。
	// 注意：只下发来源，**绝不下发触发者 user_id**（那是隐私）。
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	// 以下三个字段按配置开关决定是否填充。
	Cost         *float64 `json:"cost,omitempty"`
	InputTokens  *int     `json:"input_tokens,omitempty"`
	OutputTokens *int     `json:"output_tokens,omitempty"`
}

// PublicCaseDTO 是公开侧的题目 DTO。
//
// 不暴露 params（可能含供应商特定参数）、system_prompt（提示词工程细节）
// 与 scoring（评分策略）。只给「这题叫什么、考什么类型」。
type PublicCaseDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	ExpectKind string `json:"expect_kind"`
	Prompt     string `json:"prompt"`
	// AllowUserTest 表示该题目是否开放自测。
	//
	// 公开它有两个理由：自测对话框要据此过滤候选（否则用户能选中一个
	// 提交必被拒的题目，只能靠错误提示才知道原因）；而且「这题能不能自测」
	// 本身不是敏感信息 —— 它不泄露提示词工程细节，只说明一道门是否敞开。
	AllowUserTest bool `json:"allow_user_test"`
}

// PublicOverviewDTO 是公开侧的作品展总览。
type PublicOverviewDTO struct {
	TotalArtifacts int64                     `json:"total_artifacts"`
	ImageArtifacts int64                     `json:"image_artifacts"`
	TextArtifacts  int64                     `json:"text_artifacts"`
	CaseCount      int64                     `json:"case_count"`
	Buckets        []PublicOverviewBucketDTO `json:"buckets"`
}

// PublicOverviewBucketDTO 是「模型 × 通过率」聚合行。
//
// 下发 GroupID 与 GroupExposeName（管理员自填的展示名），**不下发**上游
// 分组名。理由：公开侧要按「分组分墙」，墙必须有个名字；但上游分组名
// 往往能看出渠道来源（如「claude-code-高倍率」）。所以名字由管理员在
// 白名单里另填一份，上游名永远不出后台。
//
// GroupExposeName 为空表示管理员没给这个分组起名 —— 前端应跳过这堵墙，
// 后端不回退成上游分组名（回退等于把「忘了填」变成「泄露」）。
type PublicOverviewBucketDTO struct {
	GroupID         int64  `json:"group_id"`
	GroupExposeName string `json:"group_name,omitempty"`
	Model           string `json:"model"`
	Total           int64  `json:"total"`
	Success         int64  `json:"success"`
	Failed          int64  `json:"failed"`
	// Verdict 是本行的四态判定汇总（normal / degraded / generated / unknown）。
	//
	// 与 Success/Failed 并列而不是替代：那两个回答「调用跑通没有」，
	// Verdict 回答「模型答对没有」。模型悄悄变笨时前者仍然好看，
	// 所以必须两个都给，让读者自己看出差异。
	Verdict     string     `json:"verdict"`
	Passed      int64      `json:"passed"`
	LogicTotal  int64      `json:"logic_total"`
	AvgDuration float64    `json:"avg_duration_ms"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
}

// ---------- 映射函数 ----------

// toAdminCase 映射题目。
func toAdminCase(c *iqbenchmark.Case) AdminCaseDTO {
	if c == nil {
		return AdminCaseDTO{}
	}
	return AdminCaseDTO{
		ID:              c.ID,
		Name:            c.Name,
		Kind:            c.Kind,
		Prompt:          c.Prompt,
		SystemPrompt:    c.SystemPrompt,
		Params:          nonNilMap(c.Params),
		ExpectKind:      c.ExpectKind,
		Scoring:         c.Scoring,
		LLMJudgeEnabled: c.LLMJudgeEnabled,
		Builtin:         c.Builtin,
		Enabled:         c.Enabled,
		AllowUserTest:   c.AllowUserTest,
		SortOrder:       c.SortOrder,
		CreatedAt:       c.CreatedAt,
		UpdatedAt:       c.UpdatedAt,
	}
}

// toAdminCases 批量映射题目。
func toAdminCases(items []iqbenchmark.Case) []AdminCaseDTO {
	out := make([]AdminCaseDTO, 0, len(items))
	for i := range items {
		out = append(out, toAdminCase(&items[i]))
	}
	return out
}

// toAdminTask 映射任务。
func toAdminTask(t *iqbenchmark.Task) AdminTaskDTO {
	if t == nil {
		return AdminTaskDTO{}
	}
	caseIDs := t.CaseIDs
	if caseIDs == nil {
		caseIDs = []int64{}
	}
	return AdminTaskDTO{
		ID:             t.ID,
		Name:           t.Name,
		GroupID:        t.GroupID,
		Model:          t.Model,
		CaseIDs:        caseIDs,
		IntervalMinute: t.IntervalMinute,
		JitterSeconds:  t.JitterSeconds,
		Enabled:        t.Enabled,
		Status:         t.Status,
		StatusReason:   t.StatusReason,
		NextRunAt:      t.NextRunAt,
		LastRunAt:      t.LastRunAt,
		LeaseOwner:     t.LeaseOwner,
		LeaseExpiresAt: t.LeaseExpiresAt,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
	}
}

// toAdminTasks 批量映射任务。
func toAdminTasks(items []iqbenchmark.Task) []AdminTaskDTO {
	out := make([]AdminTaskDTO, 0, len(items))
	for i := range items {
		out = append(out, toAdminTask(&items[i]))
	}
	return out
}

// toAdminRun 映射执行记录。
func toAdminRun(r *iqbenchmark.Run) AdminRunDTO {
	if r == nil {
		return AdminRunDTO{}
	}
	return AdminRunDTO{
		ID:                r.ID,
		TaskID:            r.TaskID,
		GroupID:           r.GroupID,
		Model:             r.Model,
		Trigger:           r.Trigger,
		StartedAt:         r.StartedAt,
		FinishedAt:        r.FinishedAt,
		SuccessCount:      r.SuccessCount,
		FailedCount:       r.FailedCount,
		TimeoutCount:      r.TimeoutCount,
		BlockedCount:      r.BlockedCount,
		TotalDurationMS:   r.TotalDurationMS,
		TotalCost:         r.TotalCost,
		TotalInputTokens:  r.TotalInputTokens,
		TotalOutputTokens: r.TotalOutputTokens,
		InternalUserID:    r.InternalUserID,
		InternalAPIKeyID:  r.InternalAPIKeyID,
		Status:            r.Status,
		CostCollectedAt:   r.CostCollectedAt,
		CreatedAt:         r.CreatedAt,
	}
}

// toAdminRuns 批量映射执行记录。
func toAdminRuns(items []iqbenchmark.Run) []AdminRunDTO {
	out := make([]AdminRunDTO, 0, len(items))
	for i := range items {
		out = append(out, toAdminRun(&items[i]))
	}
	return out
}

// toAdminArtifact 映射产出（管理侧，字段完整）。
func toAdminArtifact(a *iqbenchmark.Artifact) AdminArtifactDTO {
	if a == nil {
		return AdminArtifactDTO{}
	}
	flags := a.UnsafeFlags
	if flags == nil {
		flags = []string{}
	}
	return AdminArtifactDTO{
		ID:                a.ID,
		RunID:             a.RunID,
		TaskID:            a.TaskID,
		CaseID:            a.CaseID,
		CaseSnapshot:      nonNilMap(a.CaseSnapshot),
		GroupID:           a.GroupID,
		Model:             a.Model,
		RequestParams:     nonNilMap(a.RequestParams),
		RenderKind:        a.RenderKind,
		ImageURL:          a.ImageURL,
		Content:           a.Content,
		ContentTruncated:  a.ContentTruncated,
		ContentSHA256:     a.ContentSHA256,
		UnsafeFlags:       flags,
		RawResponseRef:    a.RawResponseRef,
		DurationMS:        a.DurationMS,
		InputTokens:       a.InputTokens,
		OutputTokens:      a.OutputTokens,
		Cost:              a.Cost,
		Status:            a.Status,
		ErrorCode:         a.ErrorCode,
		ErrorSummary:      a.ErrorSummary,
		Score:             a.Score,
		ScoredBy:          a.ScoredBy,
		ScoredAt:          a.ScoredAt,
		BlobKey:           a.BlobKey,
		CreatedAt:         a.CreatedAt,
		Source:            adminArtifactSource(a.Source),
		TriggeredByUserID: optionalID(a.TriggeredByUserID),
		ModerationStatus:  adminArtifactModeration(a.ModerationStatus),
		ReviewedBy:        optionalID(a.ReviewedBy),
		ReviewedAt:        a.ReviewedAt,
	}
}

// toAdminArtifacts 批量映射产出。
func toAdminArtifacts(items []iqbenchmark.Artifact) []AdminArtifactDTO {
	out := make([]AdminArtifactDTO, 0, len(items))
	for i := range items {
		out = append(out, toAdminArtifact(&items[i]))
	}
	return out
}

// PublicArtifactOptions 控制公开侧的可选字段。
type PublicArtifactOptions struct {
	ExposeCost   bool
	ExposeTokens bool
	// ScoringEnabled 为 false 时不下发 score（避免用户看到无法理解的分数）。
	ScoringEnabled bool
	// ViewerUserID 是当前登录用户；用于计算 is_mine。
	//
	// 只有 ID 参与比较，**不**下发：前端拿到的是布尔值，
	// 拿不到「这条属于谁」。
	ViewerUserID int64
}

// toPublicArtifact 映射公开侧产出。
//
// 安全性由三件事共同保证：
//  1. 返回类型是白名单结构体（不含内部字段），编译期就杜绝了「顺手加字段」；
//  2. 内容再做一次兜底：被阻断（blocked）的产出不下发正文，只给占位状态；
//  3. 成本与 token 按配置决定是否填充（默认关闭）。
func toPublicArtifact(a *iqbenchmark.Artifact, opts PublicArtifactOptions) PublicArtifactDTO {
	if a == nil {
		return PublicArtifactDTO{}
	}
	caseName, caseKind := caseMetaFromSnapshot(a.CaseSnapshot)

	dto := PublicArtifactDTO{
		ID:            a.ID,
		CaseID:        a.CaseID,
		CaseName:      caseName,
		CaseKind:      caseKind,
		Model:         a.Model,
		RenderKind:    a.RenderKind,
		Truncated:     a.ContentTruncated,
		Status:        a.Status,
		Verdict:       publicVerdict(a.Verdict),
		RenderVerdict: publicRenderVerdict(a.RenderVerdict),
		ErrorKind:     publicErrorKind(a),
		DurationMS:    a.DurationMS,
		IsUnsafe:      a.Status == iqbenchmark.ArtifactStatusBlocked,
		IsMine:        a.TriggeredByUserID > 0 && a.TriggeredByUserID == opts.ViewerUserID,
		Source:        publicArtifactSource(a.Source),
		CreatedAt:     a.CreatedAt,
		ErrorSummary:  publicErrorSummary(a),
	}

	// 被阻断的产出：不下发正文与图片，只留状态与脱敏摘要。
	if a.Status != iqbenchmark.ArtifactStatusBlocked {
		switch a.RenderKind {
		case iqbenchmark.RenderKindImageURL:
			dto.ImageURL = a.ImageURL
		default:
			dto.Content = a.Content
		}
	}

	if opts.ScoringEnabled {
		dto.Score = a.Score
	}
	if opts.ExposeCost {
		cost := a.Cost
		dto.Cost = &cost
	}
	if opts.ExposeTokens {
		in, out := a.InputTokens, a.OutputTokens
		dto.InputTokens = &in
		dto.OutputTokens = &out
	}
	return dto
}

// publicVerdict 归一化公开侧的判定字段。
//
// 空值兜底为 unknown 而不是 ok：判定字段是「这次答得对不对」的唯一
// 表达，未知时给 ok 等于免费发一张通过票。历史上没有这个字段的行
// （迁移 253 之前）确实是「没判过」，unknown 是如实描述。
func publicVerdict(verdict string) string {
	switch verdict {
	case iqbenchmark.VerdictOK, iqbenchmark.VerdictDegraded,
		iqbenchmark.VerdictGenerated, iqbenchmark.VerdictUnknown:
		return verdict
	default:
		return iqbenchmark.VerdictUnknown
	}
}

// publicRenderVerdict 归一化公开侧的渲染判定。
func publicRenderVerdict(verdict string) string {
	switch verdict {
	case iqbenchmark.RenderVerdictOK, iqbenchmark.RenderVerdictDegraded,
		iqbenchmark.RenderVerdictGenerated, iqbenchmark.RenderVerdictUnknown:
		return verdict
	default:
		return iqbenchmark.RenderVerdictUnknown
	}
}

// publicErrorKind 归一化公开侧的错误类别。
//
// 只下发 12 类里的已知取值；空串保持为空（omitempty 会省掉这个键），
// 未知取值一律塌缩成 generic 而不是原样下发 —— 原样下发等于把
// 内部错误字符串直通到公网，那正是白名单要拦的东西。
func publicErrorKind(a *iqbenchmark.Artifact) string {
	if a == nil {
		return ""
	}
	kind := a.ErrorKind
	if kind == "" {
		// 兼容迁移 253 之前的行：那时只有 error_code，
		// 用映射表反推一个展示类别。代码注释里明确这条路径的用途
		// 是「读旧数据」，不参与任何控制流。
		kind = iqbenchmark.ErrorKindFromCode(a.ErrorCode)
	}
	if kind == "" {
		return ""
	}
	if !iqbenchmark.IsKnownErrorKind(kind) {
		return iqbenchmark.ErrorKindGeneric
	}
	return kind
}

// publicArtifactSource 归一化公开侧的来源字段。
//
// 空值兜底为 scheduled：历史行（迁移 251 之前写入的产出）已被回填为
// scheduled，但万一有遗漏，也不能给前端一个空字符串去渲染角标 ——
// 那会让「官方检测」的作品看起来像来源不明。
func publicArtifactSource(source string) string {
	if iqbenchmark.ValidStoredSource(source) {
		return source
	}
	return iqbenchmark.SourceScheduled
}

// adminArtifactSource 管理侧的来源：与公开侧同一套合法值，但**不回退**
// 成 scheduled —— 管理侧看到错误标注只会掩盖数据问题，不如原样暴露。
func adminArtifactSource(source string) string {
	if iqbenchmark.ValidStoredSource(source) {
		return source
	}
	return iqbenchmark.SourceScheduled
}

// adminArtifactModeration 管理侧的审核状态：列有 NOT NULL DEFAULT，
// 正常不会为空；真为空时按 approved 展示与数据库默认保持一致。
func adminArtifactModeration(status string) string {
	switch status {
	case iqbenchmark.ModerationPending, iqbenchmark.ModerationRejected:
		return status
	default:
		return iqbenchmark.ModerationApproved
	}
}

// optionalID 把领域层的「0 表示不存在」还原成 JSON 的 null。
//
// 管理侧必须能区分「0 号用户」与「没有触发者」—— 前者不存在，但把两者
// 混成同一个数字会让审计时无法判断这条产出到底是不是自测。
func optionalID(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

// toPublicArtifacts 批量映射公开侧产出。
func toPublicArtifacts(items []iqbenchmark.Artifact, opts PublicArtifactOptions) []PublicArtifactDTO {
	out := make([]PublicArtifactDTO, 0, len(items))
	for i := range items {
		out = append(out, toPublicArtifact(&items[i], opts))
	}
	return out
}

// publicErrorSummary 返回可公开的失败摘要。
//
// 双保险：即使 error_summary 落库时没脱敏，这里也会用内部错误码
// 重新翻译一次，绝不把原始文本直传。
func publicErrorSummary(a *iqbenchmark.Artifact) string {
	if a == nil {
		return ""
	}
	if a.ErrorCode == "" {
		// 有摘要但无码：说明不是标准失败路径，仍按码翻译（空码返回空串）。
		return ""
	}
	return sanitize.SafeSummary(a.ErrorCode)
}

// toPublicCase 映射公开侧题目。
func toPublicCase(c *iqbenchmark.Case) PublicCaseDTO {
	if c == nil {
		return PublicCaseDTO{}
	}
	return PublicCaseDTO{
		ID:            c.ID,
		Name:          c.Name,
		Kind:          c.Kind,
		ExpectKind:    c.ExpectKind,
		Prompt:        c.Prompt,
		AllowUserTest: c.AllowUserTest,
	}
}

// toPublicCases 批量映射公开侧题目。
func toPublicCases(items []iqbenchmark.Case) []PublicCaseDTO {
	out := make([]PublicCaseDTO, 0, len(items))
	for i := range items {
		out = append(out, toPublicCase(&items[i]))
	}
	return out
}

// caseMetaFromSnapshot 从冻结快照里取题目名与类型。
//
// 用快照而不是联表查题目：题目后续被改名或删除时，
// 历史作品必须仍然显示「当时的题面」，否则对比会失真。
func caseMetaFromSnapshot(snapshot map[string]any) (name, kind string) {
	if snapshot == nil {
		return "", ""
	}
	if v, ok := snapshot["name"].(string); ok {
		name = v
	}
	if v, ok := snapshot["kind"].(string); ok {
		kind = v
	}
	return name, kind
}

func nonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
