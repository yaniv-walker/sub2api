package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/service"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
)

// AdminHandler 提供管理侧接口（全部 requireAdmin）。
type AdminHandler struct {
	Gallery  *service.GalleryService
	Run      *service.RunService
	Artifact *service.ArtifactService
	Settings *iqbenchmark.SettingsProvider
	Agent    *iqbenchmark.ActiveDirectory
	// Targets 解析「分组 + 模型」候选集合，供新建/编辑任务时的级联下拉使用。
	// 允许为 nil（既有单测无需构造），此时 ListTargets 返回空集合。
	Targets *service.TargetsService
}

// NewAdminHandler 构造管理侧 handler。
func NewAdminHandler(
	gallery *service.GalleryService,
	run *service.RunService,
	artifact *service.ArtifactService,
	settings *iqbenchmark.SettingsProvider,
	agent *iqbenchmark.ActiveDirectory,
) *AdminHandler {
	return &AdminHandler{Gallery: gallery, Run: run, Artifact: artifact, Settings: settings, Agent: agent}
}

// WithTargets 注入目标解析器（返回自身，便于链式构造）。
//
// 用可选注入而不是改 NewAdminHandler 签名：既有单测无需构造目标解析器。
func (h *AdminHandler) WithTargets(targets *service.TargetsService) *AdminHandler {
	if h != nil {
		h.Targets = targets
	}
	return h
}

// ---------- 题目 ----------

// ListCases 列出全部题目（含停用）。
func (h *AdminHandler) ListCases(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	items, err := h.Gallery.ListCases(c.Request.Context(), false)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{"items": toAdminCases(items)})
}

// CreateCase 新建题目。
func (h *AdminHandler) CreateCase(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	var req CaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.Gallery.CreateCase(c.Request.Context(), req.toCreateInput())
	if err != nil {
		writeError(c, err)
		return
	}
	middleware.SetAuditAction(c, "admin.iq_benchmark.case.create")
	response.Created(c, toAdminCase(item))
}

// UpdateCase 更新题目。
func (h *AdminHandler) UpdateCase(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req CaseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.Gallery.UpdateCase(c.Request.Context(), id, req.toUpdateInput())
	if err != nil {
		writeError(c, err)
		return
	}
	middleware.SetAuditAction(c, "admin.iq_benchmark.case.update")
	response.Success(c, toAdminCase(item))
}

// DeleteCase 删除题目。
func (h *AdminHandler) DeleteCase(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.Gallery.DeleteCase(c.Request.Context(), id); err != nil {
		writeError(c, err)
		return
	}
	middleware.SetAuditAction(c, "admin.iq_benchmark.case.delete")
	response.Success(c, gin.H{"deleted": true})
}

// ---------- 任务 ----------

// ListTasks 列出全部任务。
func (h *AdminHandler) ListTasks(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	items, err := h.Gallery.ListTasks(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, gin.H{"items": toAdminTasks(items)})
}

// CreateTask 新建任务。
func (h *AdminHandler) CreateTask(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	var req TaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.Gallery.CreateTask(c.Request.Context(), req.toCreateInput())
	if err != nil {
		writeError(c, err)
		return
	}
	middleware.SetAuditAction(c, "admin.iq_benchmark.task.create")
	response.Created(c, toAdminTask(item))
}

// UpdateTask 更新任务。
func (h *AdminHandler) UpdateTask(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req TaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.Gallery.UpdateTask(c.Request.Context(), id, req.toUpdateInput())
	if err != nil {
		writeError(c, err)
		return
	}
	middleware.SetAuditAction(c, "admin.iq_benchmark.task.update")
	response.Success(c, toAdminTask(item))
}

// DeleteTask 删除任务。
func (h *AdminHandler) DeleteTask(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.Gallery.DeleteTask(c.Request.Context(), id); err != nil {
		writeError(c, err)
		return
	}
	middleware.SetAuditAction(c, "admin.iq_benchmark.task.delete")
	response.Success(c, gin.H{"deleted": true})
}

// SetTaskEnabled 启停任务。
func (h *AdminHandler) SetTaskEnabled(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.Gallery.SetTaskEnabled(c.Request.Context(), id, req.Enabled)
	if err != nil {
		writeError(c, err)
		return
	}
	middleware.SetAuditAction(c, "admin.iq_benchmark.task.toggle")
	response.Success(c, toAdminTask(item))
}

// RunTaskNow 立即执行一次任务。
func (h *AdminHandler) RunTaskNow(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	agent, ok := h.Agent.Snapshot()
	if !ok || !agent.Active() {
		writeError(c, iqbenchmark.ErrInternalAgentMissing)
		return
	}
	run, err := h.Gallery.RunTaskNow(c.Request.Context(), h.Run, id, agent)
	if err != nil {
		writeError(c, err)
		return
	}
	middleware.SetAuditAction(c, "admin.iq_benchmark.task.run_now")
	middleware.SetAuditExtra(c, map[string]any{"result": "accepted"})
	response.Accepted(c, toAdminRun(run))
}

// ---------- 执行记录 ----------

// ListRuns 分页列出执行记录。
func (h *AdminHandler) ListRuns(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	taskID, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.Gallery.ListRuns(c.Request.Context(), taskID, page, pageSize)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Paginated(c, toAdminRuns(items), total, page, pageSize)
}

// ---------- 产出 ----------

// ListArtifacts 列出产出（管理侧字段完整）。
func (h *AdminHandler) ListArtifacts(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	result, err := h.Gallery.ListArtifacts(c.Request.Context(), adminListQuery(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Paginated(c, toAdminArtifacts(result.Items), result.Total, result.Page, result.PageSize)
}

// GetArtifact 读取单个产出。
func (h *AdminHandler) GetArtifact(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	item, err := h.Gallery.GetArtifact(c.Request.Context(), id)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, toAdminArtifact(item))
}

// DeleteArtifact 软删除产出。
func (h *AdminHandler) DeleteArtifact(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.Artifact.Delete(c.Request.Context(), id); err != nil {
		writeError(c, err)
		return
	}
	middleware.SetAuditAction(c, "admin.iq_benchmark.artifact.delete")
	response.Success(c, gin.H{"deleted": true})
}

// ScoreArtifact 人工评分。
func (h *AdminHandler) ScoreArtifact(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req struct {
		Score float64 `json:"score"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	scoredBy := currentUserID(c)
	if err := h.Artifact.Score(c.Request.Context(), id, req.Score, scoredBy); err != nil {
		writeError(c, err)
		return
	}
	middleware.SetAuditAction(c, "admin.iq_benchmark.artifact.score")
	response.Success(c, gin.H{"scored": true})
}

// ModerateArtifact 审核一件产出（通过 / 拒绝）。
//
// 公开作品展只返回 approved，所以 `require_review` 打开后，**没有这个动作
// 待审产出就永远进不了公开页** —— 用户花钱测完却什么都看不到。
// 只允许在三种状态间显式迁移，不提供「清空审核状态」：一条产出必须
// 明确是通过还是拒绝，含糊的中间态会让公开页的过滤条件失去意义。
func (h *AdminHandler) ModerateArtifact(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	// 白名单校验在 handler 做：让非法值得到明确的 400，
	// 而不是「更新了 0 行却返回成功」。
	switch strings.TrimSpace(req.Status) {
	case iqbenchmark.ModerationApproved, iqbenchmark.ModerationRejected, iqbenchmark.ModerationPending:
	default:
		response.BadRequest(c, "status must be one of approved, pending, rejected")
		return
	}
	reviewedBy := currentUserID(c)
	if err := h.Artifact.Moderate(c.Request.Context(), id, strings.TrimSpace(req.Status), reviewedBy); err != nil {
		writeError(c, err)
		return
	}
	middleware.SetAuditAction(c, "admin.iq_benchmark.artifact.moderate")
	response.Success(c, gin.H{"moderated": true})
}

// ---------- 总览 ----------

// GetOverview 返回总览（管理侧含分组维度）。
//
// 管理侧默认**全量**口径（source=all），因为管理员看的是「系统整体状况」，
// 把用户自测藏起来反而会让数字对不上。
// 显式 ?source=scheduled|user_test 可单看某条链路。
func (h *AdminHandler) GetOverview(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	overview, err := h.Gallery.GetOverview(c.Request.Context(), 50, adminSource(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, overview)
}

// adminSource 解析管理侧的来源口径：缺席即全量（显式传 all）。
//
// 管理员看的是系统整体，把用户自测藏起来会让数字对不上；
// 归一到 all 而不是留空，是为了让「漏传参数」在服务层也落到全量，
// 与公开侧默认收紧的口径明确区分。
func adminSource(c *gin.Context) string {
	raw := strings.TrimSpace(strings.ToLower(c.Query("source")))
	if raw == "" {
		return iqbenchmark.SourceAll
	}
	return iqbenchmark.NormalizeSourceFilter(raw)
}

// ---------- 任务目标（级联下拉） ----------

// ListTargets 返回「分组 + 模型」候选集合，供新建/编辑任务页的级联下拉使用。
//
// 只读：不写 usage、不计费、不触发调度。
// `?refresh=1` 强制失效上游 modelsListCache 后重取（管理员刚配好账号/映射时用）。
//
// 与 GET /v1/models 保持同一口径：模型集合必须等于「这个 Key 实际能调哪些」，
// 否则会出现「下拉选得到、一跑就报错」的错配。
func (h *AdminHandler) ListTargets(c *gin.Context) {
	if !h.ensureEnabled(c) {
		return
	}
	if h.Targets == nil {
		// 依赖缺失时不报 500：返回空集合，让前端显示「暂不可用」而不是白屏。
		response.Success(c, &service.Targets{Groups: []service.TargetGroup{}})
		return
	}
	refresh := c.Query("refresh") == "1" || c.Query("refresh") == "true"
	targets, err := h.Targets.ListTargets(c.Request.Context(), refresh)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, targets)
}

// ---------- 配置 ----------

// GetSettings 读取配置。
//
// **不**受 enabled 开关限制：功能关闭时管理员仍须能读配置才能开启它。
func (h *AdminHandler) GetSettings(c *gin.Context) {
	if h.Settings == nil {
		response.Success(c, iqbenchmark.DefaultSettings())
		return
	}
	response.Success(c, h.Settings.Get(c.Request.Context()))
}

// UpdateSettings 更新配置。
//
// 同样不受 enabled 限制（否则功能一旦关闭就再也开不回来）。
func (h *AdminHandler) UpdateSettings(c *gin.Context) {
	if h.Settings == nil {
		response.InternalError(c, "settings store is not available")
		return
	}
	var req iqbenchmark.Settings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	updated, err := h.Settings.Update(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}
	middleware.SetAuditAction(c, "admin.iq_benchmark.settings.update")
	middleware.SetAuditExtra(c, map[string]any{"enabled": updated.Enabled})
	response.Success(c, updated)
}

// ---------- 内部工具 ----------

// ensureEnabled 在功能未启用时返回 503。
func (h *AdminHandler) ensureEnabled(c *gin.Context) bool {
	if h.Settings == nil {
		return true
	}
	if h.Settings.Get(c.Request.Context()).Enabled {
		return true
	}
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, response.Response{
		Code:    http.StatusServiceUnavailable,
		Message: "IQ benchmark gallery is disabled",
		Reason:  "IQ_BENCHMARK_DISABLED",
	})
	return false
}

// ---------- 请求体 ----------

// CaseRequest 是题目的 HTTP 请求体。
type CaseRequest struct {
	Name            string         `json:"name"`
	Kind            string         `json:"kind"`
	Prompt          string         `json:"prompt"`
	SystemPrompt    string         `json:"system_prompt"`
	Params          map[string]any `json:"params"`
	ExpectKind      string         `json:"expect_kind"`
	Scoring         string         `json:"scoring"`
	LLMJudgeEnabled bool           `json:"llm_judge_enabled"`
	Enabled         bool           `json:"enabled"`
	AllowUserTest   bool           `json:"allow_user_test"`
	SortOrder       int            `json:"sort_order"`
}

// toCreateInput 映射为新建入参。
func (r CaseRequest) toCreateInput() service.CaseCreateInput {
	return service.CaseCreateInput{
		Name:            r.Name,
		Kind:            r.Kind,
		Prompt:          r.Prompt,
		SystemPrompt:    r.SystemPrompt,
		Params:          r.Params,
		ExpectKind:      r.ExpectKind,
		Scoring:         r.Scoring,
		LLMJudgeEnabled: r.LLMJudgeEnabled,
		Enabled:         r.Enabled,
		AllowUserTest:   r.AllowUserTest,
		SortOrder:       r.SortOrder,
	}
}

// toUpdateInput 映射为更新入参（builtin 不可改）。
func (r CaseRequest) toUpdateInput() service.CaseUpdateInput {
	return service.CaseUpdateInput{
		Name:            r.Name,
		Kind:            r.Kind,
		Prompt:          r.Prompt,
		SystemPrompt:    r.SystemPrompt,
		Params:          r.Params,
		ExpectKind:      r.ExpectKind,
		Scoring:         r.Scoring,
		LLMJudgeEnabled: r.LLMJudgeEnabled,
		Enabled:         r.Enabled,
		AllowUserTest:   r.AllowUserTest,
		SortOrder:       r.SortOrder,
	}
}

// TaskRequest 是任务的 HTTP 请求体。
type TaskRequest struct {
	Name            string  `json:"name"`
	GroupID         int64   `json:"group_id"`
	Model           string  `json:"model"`
	CaseIDs         []int64 `json:"case_ids"`
	IntervalMinutes int     `json:"interval_minutes"`
	JitterSeconds   int     `json:"jitter_seconds"`
	Enabled         bool    `json:"enabled"`
}

// toCreateInput 映射为新建入参。
func (r TaskRequest) toCreateInput() service.TaskCreateInput {
	return service.TaskCreateInput{
		Name:            r.Name,
		GroupID:         r.GroupID,
		Model:           r.Model,
		CaseIDs:         r.CaseIDs,
		IntervalMinutes: r.IntervalMinutes,
		JitterSeconds:   r.JitterSeconds,
		Enabled:         r.Enabled,
	}
}

// toUpdateInput 映射为更新入参。
func (r TaskRequest) toUpdateInput() service.TaskUpdateInput {
	return service.TaskUpdateInput{
		Name:            r.Name,
		GroupID:         r.GroupID,
		Model:           r.Model,
		CaseIDs:         r.CaseIDs,
		IntervalMinutes: r.IntervalMinutes,
		JitterSeconds:   r.JitterSeconds,
		Enabled:         r.Enabled,
	}
}

// adminListQuery 解析管理侧列表查询。
func adminListQuery(c *gin.Context) service.ArtifactListQuery {
	page, pageSize := response.ParsePagination(c)
	// 管理侧允许更大的单页（内部审计需要），但仍设硬上限。
	if pageSize > 100 {
		pageSize = 100
	}
	// 与公开侧同一套时间参数口径（unix 秒），避免同一个模块出现两种格式。
	var since, until *time.Time
	if v := parseOptionalInt64(c.Query("since")); v != nil {
		t := time.Unix(*v, 0)
		since = &t
	}
	if v := parseOptionalInt64(c.Query("until")); v != nil {
		t := time.Unix(*v, 0)
		until = &t
	}
	return service.ArtifactListQuery{
		Model:    c.Query("model"),
		CaseID:   parseOptionalInt64(c.Query("case_id")),
		Status:   c.Query("status"),
		Kind:     c.Query("kind"),
		Source:   strings.TrimSpace(c.Query("source")),
		Since:    since,
		Until:    until,
		Page:     page,
		PageSize: pageSize,
		// 审核队列：?moderation=pending 列出待审产出。
		//
		// 管理侧**不**用 ModeratedOnly（那是公开侧的口径），而是把
		// moderation_status 当成一个可选筛选维度：不传则看全部，
		// 传 pending 就是审核队列。缺了这条，require_review 打开后
		// 待审产出既不出现在公开页、也没有任何入口能看到，等于黑洞。
		ModerationStatus: strings.TrimSpace(c.Query("moderation")),
	}
}

// ---------- 共享工具 ----------

// parseIDParam 解析路径参数中的 ID。
func parseIDParam(c *gin.Context, name string) (int64, bool) {
	raw := c.Param(name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid "+name)
		return 0, false
	}
	return id, true
}

// parseOptionalInt64 解析可选整数查询参数。
func parseOptionalInt64(raw string) *int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return nil
	}
	return &v
}

// currentUserID 读取当前登录用户 ID（审计用）。
func currentUserID(c *gin.Context) int64 {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		return 0
	}
	return subject.UserID
}

// writeError 把领域错误映射为 HTTP 响应。
//
// 映射表刻意显式：每个错误码都对应一个确定的状态码，
// 避免 fallthrough 把「题目不存在」写成 500。
func writeError(c *gin.Context, err error) {
	// 自测相关的哨兵先交给专门的映射（它有自己的状态码语义：
	// 402 额度、429 配额、403 开关/状态、404 不泄露存在性）。
	if status, reason, ok := statusOfUserTestError(err); ok {
		c.AbortWithStatusJSON(status, response.Response{
			Code:    status,
			Message: err.Error(),
			Reason:  reason,
		})
		return
	}
	switch {
	case err == nil:
		return
	case errors.Is(err, iqbenchmark.ErrFeatureDisabled):
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, response.Response{
			Code:    http.StatusServiceUnavailable,
			Message: "IQ benchmark gallery is disabled",
			Reason:  "IQ_BENCHMARK_DISABLED",
		})
	case errors.Is(err, iqbenchmark.ErrCaseNotFound),
		errors.Is(err, iqbenchmark.ErrTaskNotFound),
		errors.Is(err, iqbenchmark.ErrRunNotFound),
		errors.Is(err, iqbenchmark.ErrArtifactNotFound):
		response.NotFound(c, err.Error())
	case errors.Is(err, iqbenchmark.ErrCaseBuiltinUndeletable),
		errors.Is(err, iqbenchmark.ErrCaseInUse),
		errors.Is(err, iqbenchmark.ErrTaskDuplicateEnabled),
		errors.Is(err, iqbenchmark.ErrCapacityExceeded),
		errors.Is(err, iqbenchmark.ErrAccountDisabled),
		errors.Is(err, iqbenchmark.ErrInternalAgentMissing),
		errors.Is(err, iqbenchmark.ErrBudgetExhausted):
		response.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, iqbenchmark.ErrPageSizeTooLarge),
		errors.Is(err, service.ErrInvalidKind),
		errors.Is(err, service.ErrInvalidExpectKind),
		errors.Is(err, service.ErrInvalidScoring),
		errors.Is(err, service.ErrEmptyName),
		errors.Is(err, service.ErrEmptyPrompt),
		errors.Is(err, service.ErrKindExpectMismatch),
		errors.Is(err, service.ErrIntervalTooSmall),
		errors.Is(err, service.ErrIntervalTooLarge),
		errors.Is(err, service.ErrJitterNegative),
		errors.Is(err, service.ErrJitterTooLarge),
		errors.Is(err, iqbenchmark.ErrTaskNoCases),
		errors.Is(err, iqbenchmark.ErrTaskGroupMissing),
		errors.Is(err, iqbenchmark.ErrCaseInvalid),
		errors.Is(err, iqbenchmark.ErrTaskInvalid),
		// 目标（分组 + 模型）不可用属于「用户可改的输入错误」，返回 400 让前端
		// 直接把错误挂在对应下拉上；不返回 404，因为分组可能只是暂时不可调度。
		errors.Is(err, iqbenchmark.ErrTargetGroupUnavailable),
		errors.Is(err, iqbenchmark.ErrTargetModelUnavailable),
		errors.Is(err, iqbenchmark.ErrTargetCapabilityMismatch):
		response.BadRequest(c, err.Error())
	default:
		response.InternalError(c, "internal error")
	}
}
