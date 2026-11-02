package handler

// Handlers 汇总本模块的 HTTP 处理器与运行期依赖。
//
// 上游 `handler.Handlers` 只持有它的一个指针字段（注册一行级别改动），
// 路由函数从该字段取用，从而避免上游 handler 包认识本模块的任何细节。
type Handlers struct {
	Admin   *AdminHandler
	User    *UserHandler
	Limiter RateLimiter
	// IsEnabled 决定用户侧路由是否注册（enabled=false 时路径完全不存在）。
	IsEnabled func() bool
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
