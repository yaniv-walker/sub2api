// Package ratelimit 实现作品展专用的两档限流。
//
// 为什么独立一套而不是复用 panel 限流：
//   - panel 限流按「面板接口」统一计量，作品展是**公开浏览**型流量（翻页、
//     切筛选、看大图），特征与面板写操作完全不同，混在一起会把面板额度吃掉；
//   - 独立命名空间（`iqgallery:` 前缀）可以单独调参、单独观测，
//     且与上游 middleware 零合并冲突。
package ratelimit

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
)

// UserIDFromContext 读取已认证用户 ID。
//
// 复用 middleware 的会话约定（ContextKeyUser → AuthSubject），
// 而不是在本模块内硬编码 gin context 键名：键名一旦改动只有一个来源。
// 未认证时返回 0，调用方据此跳过计数。
func UserIDFromContext(c *gin.Context) int64 {
	if c == nil {
		return 0
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		return 0
	}
	return subject.UserID
}

// 默认档位（可由管理端配置覆盖）。
const (
	// DefaultReadLimitPerMinute 是浏览类接口（列表、详情、概览）的每分钟上限。
	DefaultReadLimitPerMinute = 120
	// DefaultHeavyLimitPerMinute 是重操作（对比、跑批）的每分钟上限。
	DefaultHeavyLimitPerMinute = 60
	// DefaultMaxConcurrent 是同用户同时在途的重操作上限。
	DefaultMaxConcurrent = 4
	// window 是固定窗口长度。
	window = time.Minute
	// concurrencyTTL 是并发计数键的存活时间（防泄漏）。
	concurrencyTTL = 90 * time.Second
	// failOpenLogInterval 限制 fail-open 日志频率，避免 Redis 挂掉时刷爆日志。
	failOpenLogInterval = 30 * time.Second
)

// Tier 是限流档位。
type Tier string

const (
	// TierRead 浏览类。
	TierRead Tier = "read"
	// TierHeavy 重操作类。
	TierHeavy Tier = "heavy"
)

// AllowResult 是一次限流判定结果。
type AllowResult struct {
	Allowed    bool
	Remaining  int
	RetryAfter time.Duration
}

// RedisClient 是限流所需的最小 Redis 能力（便于单测替换）。
//
// 刻意只用最基础的 Get / Set / Delete 语义封装，不直接依赖 go-redis，
// 避免在本模块内违反 depguard 的 `service-no-repository` 规则。
type RedisClient interface {
	// IncrWithTTL 原子自增并在首次写入时设置过期时间，返回自增后的值。
	IncrWithTTL(ctx context.Context, key string, ttl time.Duration) (int64, error)
	// Decr 减一（不小于 0），返回减一后的值。
	Decr(ctx context.Context, key string) (int64, error)
	// SetNX 键不存在时写入，返回是否写入成功。
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	// Delete 删除键。
	Delete(ctx context.Context, key string) error
}

// SettingsProvider 提供限流参数（60s 缓存由 iqbenchmark 主包负责）。
type SettingsProvider interface {
	Get(ctx context.Context) iqbenchmark.Settings
}

// SettingsProviderFunc 便于把函数适配成 SettingsProvider。
type SettingsProviderFunc func(ctx context.Context) iqbenchmark.Settings

// Get 实现 SettingsProvider。
func (f SettingsProviderFunc) Get(ctx context.Context) iqbenchmark.Settings { return f(ctx) }

// GalleryRateLimiter 是作品展限流器。
//
// 设计要点：
//   - 两档**叠加**：重操作同时消耗 heavy 与 read 额度（重操作本身也是浏览行为），
//     因此单走 heavy 档无法绕过 read 总额；
//   - fail-open：Redis 异常时放行并告警 —— 限流是保护措施，不该成为
//     可用性的单点故障；
//   - 未登录不计数（路由层已 requiresAuth，此处仅作为防御）。
type GalleryRateLimiter struct {
	redis    RedisClient
	settings SettingsProvider
	logger   *slog.Logger

	mu          sync.Mutex
	lastFailLog time.Time

	// local 是 Redis 不可用时的进程内兜底计数（尽力而为）。
	localMu sync.Mutex
	local   map[string]localBucket
}

type localBucket struct {
	count   int
	expires time.Time
}

// New 构造限流器。redis 为 nil 时退化为纯进程内限流（仅用于测试/单机）。
func New(redis RedisClient, settings SettingsProvider, logger *slog.Logger) *GalleryRateLimiter {
	if logger == nil {
		logger = slog.Default()
	}
	return &GalleryRateLimiter{
		redis:    redis,
		settings: settings,
		logger:   logger,
		local:    make(map[string]localBucket),
	}
}

// UserKey 生成限流键。
//
// 命名空间固定为 `iqgallery:`，绝不与 panel 的 `panel:` 前缀混用。
func UserKey(tier Tier, userID int64) string {
	return "iqgallery:user:" + string(tier) + ":" + strconv.FormatInt(userID, 10)
}

// concurrencyKey 生成并发计数键。
func concurrencyKey(userID int64) string {
	return "iqgallery:user:inflight:" + strconv.FormatInt(userID, 10)
}

// limits 读取当前档位参数（带默认值兜底）。
func (l *GalleryRateLimiter) limits(ctx context.Context) (readLimit, heavyLimit, maxConcurrent int) {
	readLimit = DefaultReadLimitPerMinute
	heavyLimit = DefaultHeavyLimitPerMinute
	maxConcurrent = DefaultMaxConcurrent
	if l.settings == nil {
		return readLimit, heavyLimit, maxConcurrent
	}
	rl := l.settings.Get(ctx).RateLimit
	if rl.ReadLimitPerMinute > 0 {
		readLimit = rl.ReadLimitPerMinute
	}
	if rl.HeavyLimitPerMinute > 0 {
		heavyLimit = rl.HeavyLimitPerMinute
	}
	if rl.MaxConcurrent > 0 {
		maxConcurrent = rl.MaxConcurrent
	}
	return readLimit, heavyLimit, maxConcurrent
}

// Allow 判定一次请求是否放行。
//
// 顺序固定：先 heavy 档（更严格），再 read 档。
// 若先扣 read 再发现 heavy 超限，read 额度已被无谓消耗。
func (l *GalleryRateLimiter) Allow(ctx context.Context, userID int64, tier Tier) AllowResult {
	if userID <= 0 {
		// 未登录不计数（防御性；正常路径不会到这里）。
		return AllowResult{Allowed: true, Remaining: 0}
	}
	readLimit, heavyLimit, _ := l.limits(ctx)

	if tier == TierHeavy {
		res := l.consume(ctx, UserKey(TierHeavy, userID), heavyLimit)
		if !res.Allowed {
			return res
		}
	}
	return l.consume(ctx, UserKey(TierRead, userID), readLimit)
}

// consume 对单个键做固定窗口计数。
func (l *GalleryRateLimiter) consume(ctx context.Context, key string, limit int) AllowResult {
	if l.redis == nil {
		return l.consumeLocal(key, limit)
	}
	count, err := l.redis.IncrWithTTL(ctx, key, window)
	if err != nil {
		l.logFailOpen(err)
		return AllowResult{Allowed: true, Remaining: limit}
	}
	if count > int64(limit) {
		return AllowResult{Allowed: false, RetryAfter: window}
	}
	return AllowResult{Allowed: true, Remaining: limit - int(count)}
}

// consumeLocal 是 Redis 不可用时的进程内兜底。
func (l *GalleryRateLimiter) consumeLocal(key string, limit int) AllowResult {
	now := time.Now()
	l.localMu.Lock()
	defer l.localMu.Unlock()

	bucket, ok := l.local[key]
	if !ok || now.After(bucket.expires) {
		bucket = localBucket{count: 0, expires: now.Add(window)}
	}
	bucket.count++
	l.local[key] = bucket

	if bucket.count > limit {
		return AllowResult{Allowed: false, RetryAfter: time.Until(bucket.expires)}
	}
	return AllowResult{Allowed: true, Remaining: limit - bucket.count}
}

// AcquireConcurrency 尝试占用一个并发槽位。
//
// 返回 release 函数（必须调用，通常 defer）。获取失败返回 (nil, result)。
// 语义为 fail-open：Redis 异常时允许通过，不阻塞正常浏览。
func (l *GalleryRateLimiter) AcquireConcurrency(ctx context.Context, userID int64) (func(), AllowResult) {
	if userID <= 0 {
		return func() {}, AllowResult{Allowed: true}
	}
	_, _, maxConcurrent := l.limits(ctx)
	key := concurrencyKey(userID)

	if l.redis == nil {
		// 无 Redis：不做并发限制（仅测试路径）。
		return func() {}, AllowResult{Allowed: true}
	}

	count, err := l.redis.IncrWithTTL(ctx, key, concurrencyTTL)
	if err != nil {
		l.logFailOpen(err)
		return func() {}, AllowResult{Allowed: true}
	}
	if count > int64(maxConcurrent) {
		// 立即归还刚占用的那一格，避免把超限请求永久计入。
		if _, decrErr := l.redis.Decr(ctx, key); decrErr != nil {
			l.logger.Warn("iqbenchmark: release concurrency slot failed", "error", decrErr)
		}
		return nil, AllowResult{Allowed: false, RetryAfter: concurrencyTTL}
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			relCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, err := l.redis.Decr(relCtx, key); err != nil {
				l.logger.Warn("iqbenchmark: release concurrency slot failed", "error", err)
			}
		})
	}
	return release, AllowResult{Allowed: true, Remaining: maxConcurrent - int(count)}
}

// logFailOpen 限频记录 fail-open 事件。
func (l *GalleryRateLimiter) logFailOpen(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if time.Since(l.lastFailLog) < failOpenLogInterval {
		return
	}
	l.lastFailLog = time.Now()
	l.logger.Warn("iqbenchmark: rate limiter backend unavailable, allowing request (fail-open)",
		"error", err)
}

// ---------- Gin 中间件 ----------

// Middleware 返回浏览类限流的 Gin 中间件。
func (l *GalleryRateLimiter) Middleware() gin.HandlerFunc {
	return l.middlewareFor(TierRead)
}

// HeavyMiddleware 返回重操作限流的 Gin 中间件。
func (l *GalleryRateLimiter) HeavyMiddleware() gin.HandlerFunc {
	return l.middlewareFor(TierHeavy)
}

func (l *GalleryRateLimiter) middlewareFor(tier Tier) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := UserIDFromContext(c)
		if userID <= 0 {
			// 未登录：交给后续认证中间件处理，不消耗额度。
			c.Next()
			return
		}
		res := l.Allow(c.Request.Context(), userID, tier)
		if !res.Allowed {
			abortRateLimited(c, res.RetryAfter)
			return
		}
		c.Header("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))
		c.Next()
	}
}

// ConcurrencyMiddleware 返回并发限制中间件（用于重操作）。
func (l *GalleryRateLimiter) ConcurrencyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := UserIDFromContext(c)
		if userID <= 0 {
			c.Next()
			return
		}
		release, res := l.AcquireConcurrency(c.Request.Context(), userID)
		if release == nil {
			abortRateLimited(c, res.RetryAfter)
			return
		}
		defer release()
		c.Next()
	}
}

// abortRateLimited 输出与既有 panel 限流**逐字节一致**的 429 响应。
//
// 不能调用 middleware.abortPanelRateLimited（未导出），因此这里本地复刻
// 同样的响应体与 Retry-After 语义；一致性由 routes 层的对拍测试守住。
func abortRateLimited(c *gin.Context, retryAfter time.Duration) {
	seconds := int(retryAfter / time.Second)
	if seconds <= 0 {
		seconds = 60
	}
	c.Header("Retry-After", strconv.Itoa(seconds))
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
		"code":    "RATE_LIMITED",
		"message": "Too many requests, please slow down and try again later",
	})
}
