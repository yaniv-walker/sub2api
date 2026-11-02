//go:build unit

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	iqhandler "github.com/Wei-Shaw/sub2api/internal/iqbenchmark/handler"
)

// nopMiddleware 是满足中间件签名的直通实现。
func nopMiddleware(c *gin.Context) { c.Next() }

// buildRouter 构造只含本模块路由的测试引擎。
func buildRouter(handlers *iqhandler.Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	RegisterIQBenchmarkRoutes(engine.Group("/api/v1"), handlers, nopMiddleware, nopMiddleware, nopMiddleware)
	return engine
}

// stubbedHandlers 构造带替身 handler 的 Handlers。
//
// AdminHandler / UserHandler 是具体类型，方法集无法用假结构体顶替；
// 本测试只关心**路由注册结果**（路径是否存在），因此直接传入空实例，
// 不触发任何 handler 逻辑。
func stubbedHandlers(enabled bool) *iqhandler.Handlers {
	return &iqhandler.Handlers{
		Admin:     &iqhandler.AdminHandler{},
		User:      &iqhandler.UserHandler{},
		IsEnabled: func() bool { return enabled },
	}
}

// routePaths 返回引擎上已注册的全部路径。
func routePaths(engine *gin.Engine) map[string]bool {
	paths := make(map[string]bool)
	for _, route := range engine.Routes() {
		paths[route.Path] = true
	}
	return paths
}

// TestUserRoutesUnregisteredWhenDisabled 断言功能关闭时用户侧路径完全不存在。
//
// 必须是「不注册」而不是「注册后返回 403/503」：后者等于告诉全世界
// 这里藏着一个功能，且会诱导反复试探。
func TestUserRoutesUnregisteredWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := buildRouter(stubbedHandlers(false))
	paths := routePaths(engine)

	for _, path := range []string{
		"/api/v1/iq-gallery/overview",
		"/api/v1/iq-gallery/cases",
		"/api/v1/iq-gallery/artifacts",
		"/api/v1/iq-gallery/artifacts/compare",
		"/api/v1/iq-gallery/artifacts/:id",
	} {
		require.False(t, paths[path], "功能关闭时不得注册 %s", path)
	}

	// 确认真的 404（而不是被某个 catch-all 接住）。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/iq-gallery/overview", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

// TestAdminSettingsReachableEvenWhenDisabled 断言关闭时管理侧配置仍可读写。
//
// 这是「可恢复性」要求：如果 settings 也一并不可达，功能一旦关闭
// 就只能改数据库才能重新打开。
func TestAdminSettingsReachableEvenWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := buildRouter(stubbedHandlers(false))
	paths := routePaths(engine)

	require.True(t, paths["/api/v1/admin/iq-benchmark/settings"],
		"settings 路由必须始终注册，否则关闭后无法再开启")

	// 关闭时其余管理接口仍注册（由 handler 返回 503），便于前端展示状态。
	require.True(t, paths["/api/v1/admin/iq-benchmark/cases"])
	require.True(t, paths["/api/v1/admin/iq-benchmark/tasks"])
	require.True(t, paths["/api/v1/admin/iq-benchmark/artifacts"])
	require.True(t, paths["/api/v1/admin/iq-benchmark/overview"])
}

// TestUserRoutesRegisteredWhenEnabled 断言启用后用户侧 5 条只读路由都存在。
func TestUserRoutesRegisteredWhenEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := buildRouter(stubbedHandlers(true))
	paths := routePaths(engine)

	for _, path := range []string{
		"/api/v1/iq-gallery/overview",
		"/api/v1/iq-gallery/cases",
		"/api/v1/iq-gallery/artifacts",
		"/api/v1/iq-gallery/artifacts/compare",
		"/api/v1/iq-gallery/artifacts/:id",
	} {
		require.True(t, paths[path], "启用后必须注册 %s", path)
	}
}

// TestUserRoutesAreReadOnly 断言用户侧不存在任何写操作。
//
// 普通用户只能看，不能建/改/删：所有写路径都必须在 /admin 下。
func TestUserRoutesAreReadOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := buildRouter(stubbedHandlers(true))

	for _, route := range engine.Routes() {
		if len(route.Path) < 14 || route.Path[:14] != "/api/v1/iq-gal" {
			continue
		}
		require.Equal(t, http.MethodGet, route.Method,
			"用户侧路由 %s 必须是只读 GET，实际为 %s", route.Path, route.Method)
	}
}

// TestAllAdminWriteRoutesAreUnderAdminPrefix 断言所有写路由都在管理前缀下。
func TestAllAdminWriteRoutesAreUnderAdminPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := buildRouter(stubbedHandlers(true))

	const adminPrefix = "/api/v1/admin/iq-benchmark"
	for _, route := range engine.Routes() {
		if route.Method == http.MethodGet || route.Method == http.MethodHead {
			continue
		}
		require.Regexp(t, "^"+adminPrefix, route.Path,
			"写路由 %s %s 必须位于管理前缀下", route.Method, route.Path)
	}
}

// TestRegisterWithNilHandlersIsNoop 断言 nil 入参不会 panic。
//
// Wire 在功能完全未配置时可能传入 nil；注册函数必须安全短路，
// 否则整个服务启动失败。
func TestRegisterWithNilHandlersIsNoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	require.NotPanics(t, func() {
		engine := gin.New()
		RegisterIQBenchmarkRoutes(engine.Group("/api/v1"), nil, nopMiddleware, nopMiddleware, nopMiddleware)
		require.Empty(t, engine.Routes())
	})
}

// TestRegisterWithNilAdminIsNoop 断言只有 User 而无 Admin 时也短路。
//
// 管理接口是功能的前置条件（无法配置就无法使用），因此 Admin 缺失时
// 整体不注册，避免出现「只能看不能管」的半残状态。
func TestRegisterWithNilAdminIsNoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handlers := &iqhandler.Handlers{
		User:      &iqhandler.UserHandler{},
		IsEnabled: func() bool { return true },
	}
	RegisterIQBenchmarkRoutes(engine.Group("/api/v1"), handlers, nopMiddleware, nopMiddleware, nopMiddleware)
	require.Empty(t, engine.Routes())
}

// TestEnabledIsFailClosed 断言 IsEnabled 缺失时按关闭处理。
//
// fail-closed：宁可暂时 404，也不要在配置读取异常时把半成品暴露出去。
func TestEnabledIsFailClosed(t *testing.T) {
	require.False(t, (&iqhandler.Handlers{}).Enabled(), "nil IsEnabled 必须视为关闭")
	require.False(t, (*iqhandler.Handlers)(nil).Enabled(), "nil Handlers 必须视为关闭")
	require.True(t, (&iqhandler.Handlers{IsEnabled: func() bool { return true }}).Enabled())
}
