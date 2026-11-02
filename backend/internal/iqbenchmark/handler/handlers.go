package handler

// Handlers 汇总本模块的 HTTP 处理器与运行期依赖。
//
// 上游 `handler.Handlers` 只持有它的一个指针字段（注册一行级别改动），
// 路由函数从该字段取用，从而避免上游 handler 包认识本模块的任何细节。
type Handlers struct {
	Admin *AdminHandler
	User  *UserHandler
	// UserTest 提供「用户自测」的写入接口。
	//
	// 它刻意与 User 分开：User 的定位是只读作品展，
	// 自测是写入操作，混进去会破坏那条不变量。
	UserTest *UserTestHandler
	Limiter  RateLimiter
	// IsEnabled 决定用户侧路由是否注册（enabled=false 时路径完全不存在）。
	IsEnabled func() bool
	// IsUserTestEnabled 决定自测路由是否注册。
	//
	// 与 IsEnabled 分开判断：自测有独立开关，关闭时同样**不注册**路由
	// （404 而不是 403），这样「功能存在但被禁止」这一事实不会外泄。
	IsUserTestEnabled func() bool
}

// RateLimiter 是限流中间件的能力集合。
//
// 用接口而不是具体类型：避免 handler 包 import ratelimit 包，
// 也便于路由层用替身做「未登录不计数」这类断言。
type RateLimiter interface {
	Middleware() HandlerFunc
	HeavyMiddleware() HandlerFunc
	ConcurrencyMiddleware() HandlerFunc
}

// Enabled 安全读取启用状态。
//
// fail-closed：IsEnabled 为 nil 或读取失败时按「未启用」处理。
// 宁可用户侧暂时 404，也不要在配置读取异常时把未完成的功能暴露出去。
func (h *Handlers) Enabled() bool {
	if h == nil || h.IsEnabled == nil {
		return false
	}
	return h.IsEnabled()
}

// UserTestEnabled 安全读取自测开关（同样 fail-closed）。
func (h *Handlers) UserTestEnabled() bool {
	if h == nil || h.IsUserTestEnabled == nil {
		return false
	}
	return h.IsUserTestEnabled()
}
