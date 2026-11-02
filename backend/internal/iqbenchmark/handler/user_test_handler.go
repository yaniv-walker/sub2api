package handler

import (
	"errors"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/ratelimit"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/service"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
)

// contentAuditor 是「提交前内容审核」的最小抽象。
//
// 为什么要抽象成接口而不是直接调 handler 包内的 checkSecurityAuditBeforeSubmit：
// 那个函数依赖 *gin.Context 上的中间件状态，把它硬编码进来会让本模块
// 与上游 handler 包产生循环依赖。用一个函数类型的钩子，装配时把审核器注入进来：
//   - 生产装配注入真实的审核器（拿到 gin.Context 时同步完成）；
//   - 单测注入 nil（或一个放行的假实现）。
type contentAuditor func(c *gin.Context, prompt string) error

// UserTestHandler 提供「用户用自己本站 API Key 一键自测」的三个接口。
//
// 与 UserHandler 分开的原因：那个 handler 的定位是**只读**（注释里写死了
// 「不存在任何写入方法」）。自测是写入操作，混进去会破坏那条不变量。
type UserTestHandler struct {
	Tests    *service.UserTestService
	Settings *iqbenchmark.SettingsProvider
	// Audit 为 nil 时跳过审核（未装配审核器的部署不应该因此完全不可用）。
	Audit contentAuditor
}

// UserTestSubmitRequest 是提交自测的请求体。
type UserTestSubmitRequest struct {
	APIKeyID int64  `json:"api_key_id"`
	CaseID   int64  `json:"case_id"`
	GroupID  int64  `json:"group_id"`
	Model    string `json:"model"`
}

// UserTestDTO 是自测记录的公开 DTO。
//
// 刻意不下发 dedupe_key（内部去重键）与 estimated_cost（内部估算），
// 只给用户能理解的状态与结果。
type UserTestDTO struct {
	ID         int64     `json:"id"`
	CaseID     int64     `json:"case_id"`
	GroupID    int64     `json:"group_id"`
	Model      string    `json:"model"`
	Status     string    `json:"status"`
	ErrorCode  string    `json:"error_code,omitempty"`
	ArtifactID *int64    `json:"artifact_id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Submit 提交一次自测。
//
// 校验顺序：功能开关（路由层已挡）→ 请求体 → 内容审核 → service 预检。
// 内容审核放在 service 之前：审核不过的内容不应该占用任何配额。
func (h *UserTestHandler) Submit(c *gin.Context) {
	if !h.enabled(c) {
		writeError(c, iqbenchmark.ErrUserTestDisabled)
		return
	}
	userID := ratelimit.UserIDFromContext(c)
	if userID <= 0 {
		response.Unauthorized(c, "authentication required")
		return
	}
	var req UserTestSubmitRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	if req.CaseID <= 0 || req.GroupID <= 0 || req.APIKeyID <= 0 || req.Model == "" {
		response.BadRequest(c, "api_key_id, case_id, group_id and model are required")
		return
	}

	// 审核：拿题目 prompt 作为审核对象（模型真正会看到的正是它）。
	if h.Audit != nil {
		prompt, err := h.promptOf(c, req.CaseID)
		if err != nil {
			writeError(c, err)
			return
		}
		if err := h.Audit(c, prompt); err != nil {
			// 审核拒绝由审核器自己写响应；这里只保证不再往下走。
			return
		}
	}

	settings := h.settings(c)
	record, err := h.Tests.Submit(c.Request.Context(), iqbenchmark.UserTestSubmitInput{
		UserID:        userID,
		APIKeyID:      req.APIKeyID,
		CaseID:        req.CaseID,
		GroupID:       req.GroupID,
		Model:         req.Model,
		RequireReview: settings.UserTestRequireReview,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, toUserTest(record))
}

// promptOf 读取题目的 prompt（审核对象）。
func (h *UserTestHandler) promptOf(c *gin.Context, caseID int64) (string, error) {
	cases, err := h.Tests.Store.ListCasesByIDs(c.Request.Context(), []int64{caseID})
	if err != nil {
		return "", err
	}
	item, ok := cases[caseID]
	if !ok {
		return "", iqbenchmark.ErrCaseNotFound
	}
	return item.Prompt, nil
}

// List 返回当前用户的自测历史。
func (h *UserTestHandler) List(c *gin.Context) {
	if !h.enabled(c) {
		writeError(c, iqbenchmark.ErrUserTestDisabled)
		return
	}
	userID := ratelimit.UserIDFromContext(c)
	if userID <= 0 {
		response.Unauthorized(c, "authentication required")
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.Tests.List(c.Request.Context(), userID, page, pageSize)
	if err != nil {
		writeError(c, err)
		return
	}
	out := make([]UserTestDTO, 0, len(items))
	for i := range items {
		out = append(out, toUserTest(&items[i]))
	}
	response.Success(c, gin.H{
		"items":     out,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// Get 返回单条自测记录。
func (h *UserTestHandler) Get(c *gin.Context) {
	if !h.enabled(c) {
		writeError(c, iqbenchmark.ErrUserTestDisabled)
		return
	}
	userID := ratelimit.UserIDFromContext(c)
	if userID <= 0 {
		response.Unauthorized(c, "authentication required")
		return
	}
	id := parseOptionalInt64(c.Param("id"))
	if id == nil {
		response.BadRequest(c, "invalid id")
		return
	}
	record, err := h.Tests.Get(c.Request.Context(), userID, *id)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, toUserTest(record))
}

// enabled 判断功能与自测子开关是否都打开。
func (h *UserTestHandler) enabled(c *gin.Context) bool {
	return h.settings(c).UserTestEnabled
}

func (h *UserTestHandler) settings(c *gin.Context) iqbenchmark.Settings {
	if h.Settings == nil {
		return iqbenchmark.DefaultSettings()
	}
	return h.Settings.Get(c.Request.Context())
}

// toUserTest 映射为公开 DTO。
func toUserTest(t *iqbenchmark.UserTest) UserTestDTO {
	if t == nil {
		return UserTestDTO{}
	}
	dto := UserTestDTO{
		ID:        t.ID,
		CaseID:    t.CaseID,
		GroupID:   t.GroupID,
		Model:     t.Model,
		Status:    t.Status,
		ErrorCode: t.ErrorCode,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
	if t.ArtifactID > 0 {
		id := t.ArtifactID
		dto.ArtifactID = &id
	}
	return dto
}

// statusOfUserTestError 把哨兵映射为 HTTP 状态（供 writeError 使用）。
func statusOfUserTestError(err error) (int, string, bool) {
	switch {
	case errors.Is(err, iqbenchmark.ErrUserTestDisabled):
		return 403, "USER_TEST_DISABLED", true
	case errors.Is(err, iqbenchmark.ErrCaseNotOpenForUserTest):
		return 403, "CASE_NOT_OPEN_FOR_USER_TEST", true
	case errors.Is(err, iqbenchmark.ErrUserNotActive):
		return 403, "USER_NOT_ACTIVE", true
	case errors.Is(err, iqbenchmark.ErrAPIKeyNotOwned), errors.Is(err, iqbenchmark.ErrUserTestNotFound):
		// 两者的共同点：都不能泄露「这条记录/这个 Key 是否存在」。
		return 404, "NOT_FOUND", true
	case errors.Is(err, iqbenchmark.ErrAPIKeyInactive):
		return 403, "API_KEY_INACTIVE", true
	case errors.Is(err, iqbenchmark.ErrAPIKeyExpired):
		return 403, "API_KEY_EXPIRED", true
	case errors.Is(err, iqbenchmark.ErrAPIKeyQuotaExhausted):
		return 402, "INSUFFICIENT_QUOTA", true
	case errors.Is(err, iqbenchmark.ErrGroupImageGenerationDisabled):
		return 403, "GROUP_IMAGE_GENERATION_DISABLED", true
	case errors.Is(err, iqbenchmark.ErrTargetGroupUnavailable):
		return 400, "TARGET_GROUP_UNAVAILABLE", true
	case errors.Is(err, iqbenchmark.ErrTargetModelUnavailable):
		return 400, "MODEL_NOT_AVAILABLE", true
	case errors.Is(err, iqbenchmark.ErrTargetCapabilityMismatch):
		return 400, "MODEL_CAPABILITY_MISMATCH", true
	case errors.Is(err, iqbenchmark.ErrUserTestDailyLimit):
		return 429, "USER_TEST_DAILY_LIMIT", true
	case errors.Is(err, iqbenchmark.ErrUserTestConcurrent):
		return 429, "USER_TEST_CONCURRENT", true
	case errors.Is(err, iqbenchmark.ErrUserTestDuplicate):
		return 429, "USER_TEST_DUPLICATE", true
	case errors.Is(err, iqbenchmark.ErrUserTestBudgetExceeded):
		return 429, "USER_TEST_BUDGET_EXCEEDED", true
	}
	return 0, "", false
}
