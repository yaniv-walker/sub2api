package routes

import (
	"github.com/gin-gonic/gin"

	mdhandler "github.com/Wei-Shaw/sub2api/internal/modeldegradation/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
)

// RegisterModelDegradationRoutes 注册「模型降智检测」的路由。
//
// 挂载策略与 iqbenchmark 的差异（有意为之）：
//   - 用户侧**不需要开关**：这个功能不产生平台费用（用户自带凭据），
//     没有「悄悄烧钱」的风险，因此常开。
//   - 管理侧的可见性配置需要 adminAuth，因为它影响所有用户看到的内容。
//
// 该文件是纯新增，不修改既有 routes 的任何逻辑；调用方（router.go）
// 只需追加一行 RegisterModelDegradationRoutes(...)。
func RegisterModelDegradationRoutes(
	v1 *gin.RouterGroup,
	h *mdhandler.Handler,
	adminAuth middleware.AdminAuthMiddleware,
	jwtAuth middleware.JWTAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
) {
	if h == nil {
		return
	}

	// ---- 管理侧：历史可见性 ----
	//
	// 隐藏某个分组/主机后，普通用户的检测历史里就看不到这一节，
	// 管理员仍然可见。这个开关会影响所有人看到的内容，因此必须限管理员。
	admin := v1.Group("/admin/model-degradation")
	admin.Use(gin.HandlerFunc(adminAuth))
	admin.Use(gin.HandlerFunc(auditLog))
	{
		vis := admin.Group("/history-visibility")
		{
			vis.GET("", h.GetVisibility)
			vis.PUT("/groups/:id", h.SetGroupHidden)
			vis.PUT("/hosts", h.SetHostHidden)
		}
	}

	// ---- 用户侧 ----
	//
	// 全部需要登录：这个功能会带着用户填的 api_key 出站，
	// 匿名开放等于给了一个免费的 SSRF/凭据试探跳板。
	runs := v1.Group("/model-degradation")
	runs.Use(gin.HandlerFunc(jwtAuth))
	{
		// 题库只读。
		runs.GET("/tasks", h.GetTasks)

		// 拉上游模型列表：需要用户凭据，但只是一次 GET。
		runs.POST("/models", h.ListModels)

		// 发起检测。这是最重的操作：会真的去调上游、产生用户自己的费用。
		runs.POST("/runs", h.StartRun)

		// 公共墙 + 我的检测（mine=1 切换）。
		runs.GET("/runs", h.ListRuns)

		// 顶部统计（通过率 / 最近耗时 / 检测次数）。
		runs.GET("/stats", h.GetStats)

		// 详情与中止。
		runs.GET("/runs/:id", h.GetRun)
		runs.POST("/runs/:id/cancel", h.CancelRun)
	}
}
