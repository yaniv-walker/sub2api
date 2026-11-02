//go:build unit

package ratelimit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
)

// fakeRedis 是内存版 RedisClient。
//
// 刻意实现**真实**的固定窗口语义（自增 + 过期），而不是简单的计数器：
// 这样「窗口过期后恢复」这类行为也能被测试覆盖。
type fakeRedis struct {
	mu     sync.Mutex
	counts map[string]int64
	expiry map[string]time.Time
	// err 非空时所有操作返回该错误，用于测试 fail-open。
	err error
	// incrCalls 记录自增次数，用于断言「未登录不计数」。
	incrCalls int
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{counts: map[string]int64{}, expiry: map[string]time.Time{}}
}

func (f *fakeRedis) expired(key string, now time.Time) bool {
	at, ok := f.expiry[key]
	return ok && now.After(at)
}

func (f *fakeRedis) IncrWithTTL(_ context.Context, key string, ttl time.Duration) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	f.incrCalls++
	now := time.Now()
	if f.expired(key, now) {
		delete(f.counts, key)
		delete(f.expiry, key)
	}
	f.counts[key]++
	if _, ok := f.expiry[key]; !ok {
		f.expiry[key] = now.Add(ttl)
	}
	return f.counts[key], nil
}

func (f *fakeRedis) Decr(_ context.Context, key string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	if f.counts[key] <= 0 {
		delete(f.counts, key)
		return 0, nil
	}
	f.counts[key]--
	return f.counts[key], nil
}

func (f *fakeRedis) SetNX(_ context.Context, key, _ string, ttl time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	if _, ok := f.counts[key]; ok {
		return false, nil
	}
	f.counts[key] = 1
	f.expiry[key] = time.Now().Add(ttl)
	return true, nil
}

func (f *fakeRedis) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	delete(f.counts, key)
	delete(f.expiry, key)
	return nil
}

func (f *fakeRedis) value(key string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[key]
}

func (f *fakeRedis) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.incrCalls
}

// fakeSettings 返回可控的限流参数。
func fakeSettings(read, heavy, maxConcurrent int) SettingsProviderFunc {
	return func(context.Context) iqbenchmark.Settings {
		s := iqbenchmark.DefaultSettings()
		s.RateLimit.ReadLimitPerMinute = read
		s.RateLimit.HeavyLimitPerMinute = heavy
		s.RateLimit.MaxConcurrent = maxConcurrent
		return s
	}
}

// TestUserKeyUsesOwnNamespace 断言键前缀与 panel 限流完全隔离。
//
// 一旦前缀撞车，作品展的浏览流量会吃掉面板额度，且无法单独调参。
func TestUserKeyUsesOwnNamespace(t *testing.T) {
	require.Equal(t, "iqgallery:user:read:42", UserKey(TierRead, 42))
	require.Equal(t, "iqgallery:user:heavy:42", UserKey(TierHeavy, 42))
	require.Equal(t, "iqgallery:user:inflight:42", concurrencyKey(42))

	for _, key := range []string{UserKey(TierRead, 1), UserKey(TierHeavy, 1), concurrencyKey(1)} {
		require.NotContains(t, key, "panel:", "不得复用 panel 命名空间")
		require.NotContains(t, key, "panel_rate", "不得复用 panel 命名空间")
	}
}

// TestAllowEnforcesReadLimit 断言浏览档按配置上限拦截。
func TestAllowEnforcesReadLimit(t *testing.T) {
	limiter := New(newFakeRedis(), fakeSettings(3, 100, 4), nil)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		res := limiter.Allow(ctx, 1, TierRead)
		require.True(t, res.Allowed, "第 %d 次应放行", i+1)
		require.Equal(t, 3-(i+1), res.Remaining)
	}

	res := limiter.Allow(ctx, 1, TierRead)
	require.False(t, res.Allowed, "超出 read 上限后必须拦截")
	require.Equal(t, window, res.RetryAfter)
}

// TestHeavyTierConsumesBothBuckets 断言重操作同时消耗 heavy 与 read 额度。
//
// 这是「两档叠加」的核心断言：只走 heavy 档不能绕过 read 总额，
// 否则用户可以用对比接口无限翻页。
func TestHeavyTierConsumesBothBuckets(t *testing.T) {
	redis := newFakeRedis()
	limiter := New(redis, fakeSettings(10, 2, 4), nil)
	ctx := context.Background()

	// 两次重操作：heavy 用满 2/2，read 同时被记 2 次。
	require.True(t, limiter.Allow(ctx, 7, TierHeavy).Allowed)
	require.True(t, limiter.Allow(ctx, 7, TierHeavy).Allowed)
	require.EqualValues(t, 2, redis.value(UserKey(TierHeavy, 7)))
	require.EqualValues(t, 2, redis.value(UserKey(TierRead, 7)),
		"重操作必须同时计入 read 额度")

	// 第三次重操作被 heavy 档拦下。
	require.False(t, limiter.Allow(ctx, 7, TierHeavy).Allowed)

	// heavy 被拦时不应再消耗 read。
	require.EqualValues(t, 2, redis.value(UserKey(TierRead, 7)),
		"heavy 超限时应提前返回，不再扣 read")
}

// TestHeavyTierBlockedByReadExhaustion 断言 read 先耗尽时重操作也被拦。
func TestHeavyTierBlockedByReadExhaustion(t *testing.T) {
	limiter := New(newFakeRedis(), fakeSettings(2, 100, 4), nil)
	ctx := context.Background()

	require.True(t, limiter.Allow(ctx, 5, TierRead).Allowed)
	require.True(t, limiter.Allow(ctx, 5, TierRead).Allowed)
	require.False(t, limiter.Allow(ctx, 5, TierRead).Allowed)

	// read 已耗尽：重操作第三步扣 read 时失败。
	require.False(t, limiter.Allow(ctx, 5, TierHeavy).Allowed,
		"read 额度耗尽后重操作也必须被拦")
}

// TestLimitsArePerUser 断言额度按用户独立，不互相影响。
func TestLimitsArePerUser(t *testing.T) {
	limiter := New(newFakeRedis(), fakeSettings(1, 1, 4), nil)
	ctx := context.Background()

	require.True(t, limiter.Allow(ctx, 1, TierRead).Allowed)
	require.False(t, limiter.Allow(ctx, 1, TierRead).Allowed, "用户 1 已用尽")

	// 用户 2 仍有完整额度：否则一个人刷爆就会影响所有人。
	require.True(t, limiter.Allow(ctx, 2, TierRead).Allowed)
}

// TestUnauthenticatedIsNotCounted 断言未登录请求不消耗任何额度。
func TestUnauthenticatedIsNotCounted(t *testing.T) {
	redis := newFakeRedis()
	limiter := New(redis, fakeSettings(1, 1, 4), nil)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		res := limiter.Allow(ctx, 0, TierRead)
		require.True(t, res.Allowed, "未登录请求不参与限流")
	}
	require.Zero(t, redis.calls(), "userID<=0 时不得触碰 Redis")
}

// TestFailOpenWhenRedisErrors 断言 Redis 异常时放行而不是拒绝。
//
// 限流是保护措施，不该成为可用性单点：Redis 挂掉时宁可暂时不限流。
func TestFailOpenWhenRedisErrors(t *testing.T) {
	redis := newFakeRedis()
	redis.err = errors.New("redis: connection refused")
	limiter := New(redis, fakeSettings(1, 1, 4), nil)
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		res := limiter.Allow(ctx, 3, TierRead)
		require.True(t, res.Allowed, "Redis 故障时必须 fail-open")
	}
}

// TestLocalFallbackEnforcesLimit 断言无 Redis 时仍有进程内兜底。
func TestLocalFallbackEnforcesLimit(t *testing.T) {
	limiter := New(nil, fakeSettings(2, 100, 4), nil)
	ctx := context.Background()

	require.True(t, limiter.Allow(ctx, 1, TierRead).Allowed)
	require.True(t, limiter.Allow(ctx, 1, TierRead).Allowed)
	require.False(t, limiter.Allow(ctx, 1, TierRead).Allowed, "进程内兜底也必须有上限")
}

// TestLocalWindowExpires 断言进程内兜底的窗口会过期。
func TestLocalWindowExpires(t *testing.T) {
	limiter := New(nil, fakeSettings(1, 100, 4), nil)
	ctx := context.Background()

	require.True(t, limiter.Allow(ctx, 1, TierRead).Allowed)
	require.False(t, limiter.Allow(ctx, 1, TierRead).Allowed)

	// 手动把窗口推到过去，模拟一分钟后。
	limiter.localMu.Lock()
	bucket := limiter.local[UserKey(TierRead, 1)]
	bucket.expires = time.Now().Add(-time.Second)
	limiter.local[UserKey(TierRead, 1)] = bucket
	limiter.localMu.Unlock()

	require.True(t, limiter.Allow(ctx, 1, TierRead).Allowed, "窗口过期后应恢复额度")
}

// TestAcquireConcurrencyEnforcesMax 断言并发槽位按上限拦截。
func TestAcquireConcurrencyEnforcesMax(t *testing.T) {
	redis := newFakeRedis()
	limiter := New(redis, fakeSettings(100, 100, 2), nil)
	ctx := context.Background()

	rel1, res1 := limiter.AcquireConcurrency(ctx, 1)
	require.NotNil(t, rel1)
	require.True(t, res1.Allowed)

	rel2, res2 := limiter.AcquireConcurrency(ctx, 1)
	require.NotNil(t, rel2)
	require.True(t, res2.Allowed)

	rel3, res3 := limiter.AcquireConcurrency(ctx, 1)
	require.Nil(t, rel3, "超出并发上限必须拒绝")
	require.False(t, res3.Allowed)

	// 超限请求不得把计数永久留在高位。
	require.EqualValues(t, 2, redis.value(concurrencyKey(1)),
		"被拒的请求必须立即归还刚占用的那一格")

	rel1()
	rel2()

	// 归还后可以再次获取。
	rel4, res4 := limiter.AcquireConcurrency(ctx, 1)
	require.NotNil(t, rel4)
	require.True(t, res4.Allowed)
	rel4()
}

// TestConcurrencyReleaseIsIdempotent 断言 release 可安全重复调用。
//
// 中间件里既有 defer release 又可能在错误路径再调用一次，
// 重复释放会把他人的槽位一起减掉。
func TestConcurrencyReleaseIsIdempotent(t *testing.T) {
	redis := newFakeRedis()
	limiter := New(redis, fakeSettings(100, 100, 4), nil)
	ctx := context.Background()

	release, _ := limiter.AcquireConcurrency(ctx, 9)
	require.NotNil(t, release)
	require.EqualValues(t, 1, redis.value(concurrencyKey(9)))

	release()
	release()
	release()

	require.EqualValues(t, 0, redis.value(concurrencyKey(9)), "重复释放不得减成负数")
}

// TestAcquireConcurrencyFailOpen 断言并发限流在 Redis 故障时 fail-open。
func TestAcquireConcurrencyFailOpen(t *testing.T) {
	redis := newFakeRedis()
	redis.err = errors.New("redis: timeout")
	limiter := New(redis, fakeSettings(100, 100, 1), nil)

	for i := 0; i < 5; i++ {
		release, res := limiter.AcquireConcurrency(context.Background(), 1)
		require.True(t, res.Allowed, "Redis 故障时并发限制应 fail-open")
		require.NotNil(t, release)
		release()
	}
}

// TestAcquireConcurrencyWithoutRedisIsUnlimited 断言无 Redis 时不阻塞（单机兜底）。
func TestAcquireConcurrencyWithoutRedisIsUnlimited(t *testing.T) {
	limiter := New(nil, fakeSettings(100, 100, 1), nil)
	releases := make([]func(), 0, 5)
	for i := 0; i < 5; i++ {
		release, res := limiter.AcquireConcurrency(context.Background(), 1)
		require.True(t, res.Allowed)
		require.NotNil(t, release)
		releases = append(releases, release)
	}
	for _, release := range releases {
		release()
	}
}

// newTestContext 构造带已认证用户的 gin 上下文。
func newTestContext(userID int64) (*gin.Context, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, engine := gin.CreateTestContext(recorder)
	if userID > 0 {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: userID})
	}
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/iq-gallery/artifacts", nil)
	return c, engine
}

// TestUserIDFromContext 断言会话键复用 middleware 约定。
func TestUserIDFromContext(t *testing.T) {
	c, _ := newTestContext(77)
	require.EqualValues(t, 77, UserIDFromContext(c))

	anon, _ := newTestContext(0)
	require.Zero(t, UserIDFromContext(anon), "未认证返回 0")

	require.Zero(t, UserIDFromContext(nil), "nil 上下文安全返回 0")
}

// TestMiddlewareAbortsWithPanelCompatibleBody 断言 429 响应与 panel 逐字节一致。
//
// 前端与运维文档已按 panel 的 429 结构适配，两处响应体不一致会导致
// 客户端无法统一处理限流错误。
func TestMiddlewareAbortsWithPanelCompatibleBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redis := newFakeRedis()
	limiter := New(redis, fakeSettings(1, 100, 4), nil)

	engine := gin.New()
	reached := 0
	engine.GET("/x", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1})
	}, limiter.Middleware(), func(c *gin.Context) {
		reached++
		c.Status(http.StatusOK)
	})

	// 第一次放行。
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, reached)
	require.Equal(t, "0", rec.Header().Get("X-RateLimit-Remaining"))

	// 第二次被拦。
	req = httptest.NewRequest(http.MethodGet, "/x", nil)
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, 1, reached, "限流命中后不得进入 handler")
	require.Equal(t, "60", rec.Header().Get("Retry-After"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "RATE_LIMITED", body["code"])
	require.Equal(t,
		"Too many requests, please slow down and try again later",
		body["message"],
		"429 文案必须与 panel 限流完全一致")
}

// TestMiddlewareSkipsAnonymous 断言未登录请求不被限额拦截也不消耗额度。
func TestMiddlewareSkipsAnonymous(t *testing.T) {
	gin.SetMode(gin.TestMode)
	redis := newFakeRedis()
	limiter := New(redis, fakeSettings(1, 100, 4), nil)

	engine := gin.New()
	engine.GET("/y", limiter.Middleware(), func(c *gin.Context) { c.Status(http.StatusOK) })

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/y", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "未登录请求交给认证层处理")
	}
	require.Zero(t, redis.calls(), "未登录不得计数")
}

// TestAbortRateLimitedDefaultsRetryAfter 断言非法 retryAfter 退化为 60 秒。
func TestAbortRateLimitedDefaultsRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)

	abortRateLimited(c, 0)
	require.Equal(t, "60", rec.Header().Get("Retry-After"))
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
}

// TestLimitsFallBackToDefaults 断言配置为 0 时使用默认档位。
//
// 0 表示「未配置」，不能理解成「一律拒绝」，否则误配会把功能整体锁死。
func TestLimitsFallBackToDefaults(t *testing.T) {
	limiter := New(newFakeRedis(), fakeSettings(0, 0, 0), nil)
	readLimit, heavyLimit, maxConcurrent := limiter.limits(context.Background())

	require.Equal(t, DefaultReadLimitPerMinute, readLimit)
	require.Equal(t, DefaultHeavyLimitPerMinute, heavyLimit)
	require.Equal(t, DefaultMaxConcurrent, maxConcurrent)
}

// TestLimitsWithoutProviderUsesDefaults 断言没有配置源时也能工作。
func TestLimitsWithoutProviderUsesDefaults(t *testing.T) {
	limiter := New(nil, nil, nil)
	readLimit, heavyLimit, maxConcurrent := limiter.limits(context.Background())

	require.Equal(t, DefaultReadLimitPerMinute, readLimit)
	require.Equal(t, DefaultHeavyLimitPerMinute, heavyLimit)
	require.Equal(t, DefaultMaxConcurrent, maxConcurrent)
}
