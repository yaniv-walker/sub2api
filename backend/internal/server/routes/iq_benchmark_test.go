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

// TestUserRoutesAreReadOnly 断言用户侧**作品展**路由中不存在任何写操作。
//
// 普通用户对作品展只能看，不能建/改/删：所有针对作品展的写路径都必须在
// /admin 下。
//
// 自测前缀（/iq-gallery/user-tests）是显式例外：它的 POST 是用户给自己
// 发起一次检测，不修改任何他人的数据。这里必须**显式排除**它，而不是靠
// 「stubbedHandlers 恰好没挂 UserTest」让断言悄悄通过 —— 那是个真实盲点：
// 一旦有人给 stubbedHandlers 补上 UserTest，这条断言会以一条看不懂的报错
// 失败；更糟的情况是有人顺手把它放宽到失去意义。
func TestUserRoutesAreReadOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := buildRouter(stubbedHandlersWithUserTest(true, true))

	const selfTestPrefix = "/api/v1/iq-gallery/user-tests"
	checked := 0
	for _, route := range engine.Routes() {
		if len(route.Path) < 14 || route.Path[:14] != "/api/v1/iq-gal" {
			continue
		}
		if len(route.Path) >= len(selfTestPrefix) && route.Path[:len(selfTestPrefix)] == selfTestPrefix {
			continue
		}
		checked++
		require.Equal(t, http.MethodGet, route.Method,
			"用户侧路由 %s 必须是只读 GET，实际为 %s", route.Path, route.Method)
	}
	require.NotZero(t, checked, "断言必须真的扫到了路由，否则是空扫描")
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

// ---------- 用户自测路由（阶段 8）----------
//
// stubbedHandlers 刻意不带 UserTest，上面「用户侧只读」的断言因此
// 从来看不到自测那条 POST —— 这是个真实盲点。下面把它显式补上。

// stubbedHandlersWithUserTest 在 stubbedHandlers 之上挂一个空的 UserTest handler。
func stubbedHandlersWithUserTest(enabled, userTestEnabled bool) *iqhandler.Handlers {
	h := stubbedHandlers(enabled)
	h.UserTest = &iqhandler.UserTestHandler{}
	h.IsUserTestEnabled = func() bool { return userTestEnabled }
	return h
}

// TestUserTestRoutesUnregisteredWhenDisabled 断言自测关闭时**连 /targets 都不存在**。
//
// 必须包含 /targets：它虽是只读，但会暴露「哪些分组可自测」这一业务事实，
// 功能关闭时同样不该可达。这是此前已知缺口 3 的直接覆盖。
func TestUserTestRoutesUnregisteredWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := buildRouter(stubbedHandlersWithUserTest(true, false))
	paths := routePaths(engine)

	for _, path := range []string{
		"/api/v1/iq-gallery/user-tests",
		"/api/v1/iq-gallery/user-tests/targets",
		"/api/v1/iq-gallery/user-tests/:id",
	} {
		require.False(t, paths[path], "自测关闭时不得注册 %s", path)
	}

	// 确认真的 404，而不是被某个 catch-all 接住。
	req := httptest.NewRequest(http.MethodGet, "/api/v1/iq-gallery/user-tests/targets", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)

	// 作品展主体不受影响：自测是附加功能，关掉它不该连带关掉浏览。
	require.True(t, paths["/api/v1/iq-gallery/artifacts"])
}

// TestUserTestRoutesRegisteredWhenEnabled 断言自测开启后三条路由齐备。
func TestUserTestRoutesRegisteredWhenEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := buildRouter(stubbedHandlersWithUserTest(true, true))
	paths := routePaths(engine)

	for _, path := range []string{
		"/api/v1/iq-gallery/user-tests",
		"/api/v1/iq-gallery/user-tests/targets",
		"/api/v1/iq-gallery/user-tests/:id",
	} {
		require.True(t, paths[path], "自测开启后必须注册 %s", path)
	}
}

// TestUserTestRequiresMainFeatureEnabled 断言主开关关闭时自测也一并消失。
//
// 两个开关是「与」的关系：主功能整体关闭时，自测的路由不该成为例外。
func TestUserTestRequiresMainFeatureEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := buildRouter(stubbedHandlersWithUserTest(false, true))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/iq-gallery/user-tests/targets", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code,
		"主功能关闭时自测路由不得存活，即使 UserTestEnabled 为真")
}
