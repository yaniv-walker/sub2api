package handler

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/modeldegradation"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
)

// Handler 提供降智检测的全部接口。
//
// 用户侧与会话身份强绑定：所有读写都带上当前用户 ID，
// 「我的检测」按它过滤，历史墙的 is_owner 也由它决定。
type Handler struct {
	Service *modeldegradation.Service
	// Resolver 用于校验用户填写的上游地址（测试可注入）。
	Resolver *net.Resolver
}

// New 构造 handler。
//
// Resolver 留空 = 用系统 DNS。单独留一个字段是为了让单测能注入假解析器，
// 否则测 SSRF 拦截就得依赖真实 DNS，测试会变成不确定的。
func New(svc *modeldegradation.Service) *Handler {
	return &Handler{Service: svc}
}

// UserIDFromContext 读取当前登录用户 ID。
//
// 必须走 middleware.GetAuthSubjectFromContext，不能自己 c.Get("user_id")。
// jwt 中间件往上下文里放的是 ContextKeyUser → AuthSubject 结构体，
// 从来没有 "user_id" 这个键 —— 自己拼键名会永远拿到 0，
// 表现就是「带着有效 token 也一律 401」，而且编译期完全不报错。
func UserIDFromContext(c *gin.Context) int64 {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		return 0
	}
	return subject.UserID
}

// ── 题库 ──────────────────────────────────────────────────────────────

// GetTasks 返回题库，按题型分组。
func (h *Handler) GetTasks(c *gin.Context) {
	bank, err := h.Service.TaskBank(c.Request.Context())
	if err != nil {
		response.InternalError(c, "读取题库失败")
		return
	}
	response.Success(c, toTaskBankDTO(bank))
}

// ── 模型列表 ──────────────────────────────────────────────────────────

// ListModels 用用户提供的凭据拉取上游模型列表。
//
// 失败返回空列表 + 200，而不是 4xx：这是「帮用户填名字」的便利功能，
// 拉不到就该让他手输，不该把整个表单卡住。真正的凭据错误会在
// 发起检测时以 auth_failed 的形式明确报出来。
func (h *Handler) ListModels(c *gin.Context) {
	var req ModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求格式不正确")
		return
	}
	if strings.TrimSpace(req.APIKey) == "" || strings.TrimSpace(req.BaseURL) == "" {
		// 前端在缺凭据时会先自己拦（modelNeedCredentials），
		// 走到这里说明它没拦，返回空列表让前端显示「先填凭据」。
		response.Success(c, gin.H{"models": []string{}})
		return
	}
	if !modeldegradation.IsValidProtocol(req.Protocol) {
		response.BadRequest(c, "未知协议")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	base, err := modeldegradation.ValidateBaseURL(ctx, req.BaseURL, h.Resolver)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	caller := &modeldegradation.UpstreamCaller{
		BaseURL:  base,
		APIKey:   req.APIKey,
		Protocol: req.Protocol,
		Timeout:  25 * time.Second,
	}
	models, err := caller.ListModels(ctx)
	if err != nil {
		// 明确告知失败原因，但仍然返回 200 + 空列表，
		// 让前端可以继续手输模型名。
		response.Success(c, gin.H{"models": []string{}, "error": err.Error()})
		return
	}
	response.Success(c, gin.H{"models": models})
}

// ── 发起检测 ──────────────────────────────────────────────────────────

// StartRun 发起一次检测，立即返回 running 状态。
func (h *Handler) StartRun(c *gin.Context) {
	userID := UserIDFromContext(c)
	if userID <= 0 {
		response.Unauthorized(c, "请先登录")
		return
	}

	var req modeldegradation.StartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求格式不正确")
		return
	}

	email := ""
	if v, ok := c.Get("user_email"); ok {
		if s, ok := v.(string); ok {
			email = s
		}
	}

	detail, err := h.Service.Start(c.Request.Context(), userID, email, req)
	if err != nil {
		switch {
		case errors.Is(err, modeldegradation.ErrNoQuestions),
			errors.Is(err, modeldegradation.ErrNoTasks),
			errors.Is(err, modeldegradation.ErrLogicNeedsExpect),
			errors.Is(err, modeldegradation.ErrRenderTimeout),
			errors.Is(err, modeldegradation.ErrModelMissing),
			errors.Is(err, modeldegradation.ErrAPIKeyMissing),
			// 协议 / 思考等级 / 题型取值不合法 —— 都是用户填错，回 400。
			errors.Is(err, modeldegradation.ErrInvalidInput):
			response.BadRequest(c, err.Error())
		case errors.Is(err, modeldegradation.ErrConcurrencyLimit):
			// 409 而不是 429：这不是限流，是「已经有一个在跑」。
			c.JSON(409, gin.H{"code": 409, "message": err.Error()})
		case errors.Is(err, modeldegradation.ErrBaseURLBlockedHost),
			errors.Is(err, modeldegradation.ErrBaseURLBlockedScheme),
			errors.Is(err, modeldegradation.ErrBaseURLInvalid):
			response.BadRequest(c, err.Error())
		default:
			response.InternalError(c, "发起检测失败")
		}
		return
	}

	detail.IsOwner = true
	response.Success(c, toRunDetailDTO(detail))
}

// ── 读取详情 / 列表 ───────────────────────────────────────────────────

// GetRun 读取一次检测的详情。
func (h *Handler) GetRun(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "检测 ID 不合法")
		return
	}

	userID := UserIDFromContext(c)
	detail, err := h.Service.GetRunDetail(c.Request.Context(), id, userID)
	if err != nil {
		if errors.Is(err, modeldegradation.ErrRunNotFound) {
			response.NotFound(c, "检测记录不存在")
			return
		}
		response.InternalError(c, "读取检测记录失败")
		return
	}
	response.Success(c, toRunDetailDTO(detail))
}

// ListRuns 读取检测历史（公共墙）。
//
// 默认排除管理员隐藏的分组与主机 —— 过滤在 SQL 层做，
// 不是查完再筛，否则分页数量会莫名其妙地少。
func (h *Handler) ListRuns(c *gin.Context) {
	limit := 20
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	offset := 0
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	filter := modeldegradation.RunFilter{
		Limit:    limit,
		Offset:   offset,
		Verdict:  c.Query("verdict"),
		TaskType: c.Query("task_type"),
	}

	// 「我的检测」用 mine=1 切换。
	if c.Query("mine") == "1" {
		userID := UserIDFromContext(c)
		if userID <= 0 {
			response.Unauthorized(c, "请先登录")
			return
		}
		filter.UserID = userID
	} else {
		// 公共墙：排除隐藏项。
		vis, err := h.Service.Store.GetVisibility(c.Request.Context())
		if err == nil && vis != nil {
			filter.ExcludeGroupIDs = vis.HiddenGroupIDs
			filter.ExcludeHosts = vis.HiddenHosts
		}
	}

	runs, total, err := h.Service.ListRuns(c.Request.Context(), filter)
	if err != nil {
		response.InternalError(c, "读取检测记录失败")
		return
	}

	userID := UserIDFromContext(c)
	items := make([]RunDTO, 0, len(runs))
	for _, r := range runs {
		dto := toRunDTO(r)
		dto.IsOwner = r.UserID != 0 && r.UserID == userID
		items = append(items, dto)
	}
	response.Success(c, RunListDTO{Items: items, Total: total})
}

// GetStats 读取顶部统计（通过率 / 最近耗时 / 检测次数）。
func (h *Handler) GetStats(c *gin.Context) {
	filter := modeldegradation.RunFilter{}
	if c.Query("mine") == "1" {
		userID := UserIDFromContext(c)
		if userID <= 0 {
			response.Unauthorized(c, "请先登录")
			return
		}
		filter.UserID = userID
	} else {
		vis, err := h.Service.Store.GetVisibility(c.Request.Context())
		if err == nil && vis != nil {
			filter.ExcludeGroupIDs = vis.HiddenGroupIDs
			filter.ExcludeHosts = vis.HiddenHosts
		}
	}

	stats, err := h.Service.Stats(c.Request.Context(), filter)
	if err != nil {
		response.InternalError(c, "读取统计失败")
		return
	}
	response.Success(c, StatsDTO{
		PassRate:       stats.PassRate,
		LastDurationMS: stats.LastDurationMS,
		RunCount:       stats.RunCount,
	})
}

// ── 中止 ──────────────────────────────────────────────────────────────

// CancelRun 中止一次检测。
//
// 已经结束时返回 409 —— 前端据此把它当「已完成」而不是失败，
// 这与参考实现的行为一致。
func (h *Handler) CancelRun(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "检测 ID 不合法")
		return
	}

	userID := UserIDFromContext(c)
	if userID <= 0 {
		response.Unauthorized(c, "请先登录")
		return
	}

	if err := h.Service.Cancel(c.Request.Context(), id, userID); err != nil {
		if errors.Is(err, modeldegradation.ErrRunNotRunning) {
			c.JSON(409, gin.H{"code": 409, "message": "该检测已经结束"})
			return
		}
		if errors.Is(err, modeldegradation.ErrRunNotFound) {
			response.NotFound(c, "检测记录不存在")
			return
		}
		response.InternalError(c, "中止检测失败")
		return
	}
	response.Success(c, gin.H{"cancelled": true})
}

// ── 管理端：历史可见性 ────────────────────────────────────────────────

// GetVisibility 读取可见性配置。
func (h *Handler) GetVisibility(c *gin.Context) {
	ctx := c.Request.Context()
	vis, err := h.Service.Store.GetVisibility(ctx)
	if err != nil {
		response.InternalError(c, "读取可见性配置失败")
		return
	}
	hosts, err := h.Service.Store.ListUnmatchedHosts(ctx)
	if err != nil {
		response.InternalError(c, "读取上游主机列表失败")
		return
	}

	out := VisibilityDTO{
		HiddenGroupIDs: vis.HiddenGroupIDs,
		HiddenHosts:    vis.HiddenHosts,
		Hosts:          make([]HostDTO, 0, len(hosts)),
	}
	if out.HiddenGroupIDs == nil {
		out.HiddenGroupIDs = []int64{}
	}
	if out.HiddenHosts == nil {
		out.HiddenHosts = []string{}
	}
	for _, hst := range hosts {
		out.Hosts = append(out.Hosts, HostDTO{Host: hst.Host, RunCount: hst.RunCount})
	}
	response.Success(c, out)
}

// SetGroupHidden 切换某个分组的可见性。
func (h *Handler) SetGroupHidden(c *gin.Context) {
	groupID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || groupID <= 0 {
		response.BadRequest(c, "分组 ID 不合法")
		return
	}
	var req SetGroupHiddenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求格式不正确")
		return
	}
	if err := h.Service.Store.SetGroupHidden(c.Request.Context(), groupID, req.Hidden); err != nil {
		response.InternalError(c, "保存可见性配置失败")
		return
	}
	response.Success(c, gin.H{"group_id": groupID, "hidden": req.Hidden})
}

// SetHostHidden 切换某个上游主机的可见性。
func (h *Handler) SetHostHidden(c *gin.Context) {
	var req SetHostHiddenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求格式不正确")
		return
	}
	host := strings.ToLower(strings.TrimSpace(req.Host))
	if host == "" {
		response.BadRequest(c, "主机名不能为空")
		return
	}
	if err := h.Service.Store.SetHostHidden(c.Request.Context(), host, req.Hidden); err != nil {
		response.InternalError(c, "保存可见性配置失败")
		return
	}
	response.Success(c, gin.H{"host": host, "hidden": req.Hidden})
}
