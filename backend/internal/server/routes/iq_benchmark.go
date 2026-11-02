package routes

import (
	"github.com/gin-gonic/gin"

	iqhandler "github.com/Wei-Shaw/sub2api/internal/iqbenchmark/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
)

// RegisterIQBenchmarkRoutes 注册「模型智商检测作品展」的路由。
//
// 挂载策略（关键）：
//   - 管理侧：始终注册在 /admin/iq-benchmark 下，但除 settings 外的接口在
//     功能未启用时由 handler 返回 503 —— 这样管理员随时能读/写配置来开启功能；
//   - 用户侧：仅当功能已启用时**才注册**。未启用时路径完全不存在（404），
//     而不是返回 403/503：不暴露"这里有个被隐藏的功能"这一事实。
//
// 该文件是纯新增，不修改既有 routes 的任何逻辑；调用方（router.go）
// 只需追加一行 RegisterIQBenchmarkRoutes(...)。
func RegisterIQBenchmarkRoutes(
	v1 *gin.RouterGroup,
	handlers *iqhandler.Handlers,
	adminAuth middleware.AdminAuthMiddleware,
	jwtAuth middleware.JWTAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
) {
	if handlers == nil || handlers.Admin == nil {
		return
	}

	// ---- 管理侧 ----
	admin := v1.Group("/admin/iq-benchmark")
	admin.Use(gin.HandlerFunc(adminAuth))
	admin.Use(gin.HandlerFunc(auditLog))
	{
		// 配置读写不受 enabled 限制（否则关闭后无法再开启）。
		admin.GET("/settings", handlers.Admin.GetSettings)
		admin.PUT("/settings", handlers.Admin.UpdateSettings)

		cases := admin.Group("/cases")
		{
			cases.GET("", handlers.Admin.ListCases)
			cases.POST("", handlers.Admin.CreateCase)
			cases.PUT("/:id", handlers.Admin.UpdateCase)
			cases.DELETE("/:id", handlers.Admin.DeleteCase)
		}

		tasks := admin.Group("/tasks")
		{
			tasks.GET("", handlers.Admin.ListTasks)
			tasks.POST("", handlers.Admin.CreateTask)
			tasks.PUT("/:id", handlers.Admin.UpdateTask)
			tasks.DELETE("/:id", handlers.Admin.DeleteTask)
			tasks.PUT("/:id/enabled", handlers.Admin.SetTaskEnabled)
			tasks.POST("/:id/run-now", handlers.Admin.RunTaskNow)
			tasks.GET("/:id/runs", handlers.Admin.ListRuns)
		}

		artifacts := admin.Group("/artifacts")
		{
			artifacts.GET("", handlers.Admin.ListArtifacts)
			artifacts.GET("/:id", handlers.Admin.GetArtifact)
			artifacts.DELETE("/:id", handlers.Admin.DeleteArtifact)
			artifacts.PUT("/:id/score", handlers.Admin.ScoreArtifact)
		}

		admin.GET("/overview", handlers.Admin.GetOverview)
	}

	// ---- 用户侧（只读，且仅在启用时注册）----
	if handlers.User == nil || !handlers.Enabled() {
		return
	}

	gallery := v1.Group("/iq-gallery")
	gallery.Use(gin.HandlerFunc(jwtAuth))
	if handlers.Limiter != nil {
		gallery.Use(handlers.Limiter.Middleware())
	}
	{
		gallery.GET("/overview", handlers.User.GetOverview)
		gallery.GET("/cases", handlers.User.ListCases)

		artifacts := gallery.Group("/artifacts")
		{
			artifacts.GET("", handlers.User.ListArtifacts)
			// 对比接口单独加重档 + 并发双保险：一次最多取 8 条完整产出。
			heavy := artifacts.Group("/compare")
			if handlers.Limiter != nil {
				heavy.Use(handlers.Limiter.HeavyMiddleware())
				heavy.Use(handlers.Limiter.ConcurrencyMiddleware())
			}
			heavy.GET("", handlers.User.Compare)

			artifacts.GET("/:id", handlers.User.GetArtifact)
		}
	}
}
