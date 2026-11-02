//go:build unit

package ratelimit

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------- AcquireUserTestSlot ----------
//
// 这一组覆盖「失败即拒绝」：自测的每次放行都对应真实费用，
// 所以 Redis 异常时必须 fail-closed，而不是像浏览限流那样放行。

// TestUserTestSlotFailsClosedOnRedisError 是此前已知缺口的直接覆盖：
// 之前只通过 service 层的假替身覆盖了「拿到/没拿到」两种结果，
// Redis 报错时的真实降级路径（返回 (nil, false)）从未被直接验证。
func TestUserTestSlotFailsClosedOnRedisError(t *testing.T) {
	redis := newFakeRedis()
	redis.err = errors.New("redis down")
	limiter := New(redis, nil, slog.New(slog.DiscardHandler))

	release, ok := limiter.AcquireUserTestSlot(context.Background(), 42, 2)
	require.False(t, ok, "a redis failure must reject, not allow")
	require.Nil(t, release, "no slot means no release function")
}

// TestUserTestSlotAllowsWithinLimit 覆盖正常配额。
func TestUserTestSlotAllowsWithinLimit(t *testing.T) {
	redis := newFakeRedis()
	limiter := New(redis, nil, slog.New(slog.DiscardHandler))

	release, ok := limiter.AcquireUserTestSlot(context.Background(), 42, 2)
	require.True(t, ok)
	require.NotNil(t, release)

	// 未释放前占一格；再取一格仍在限内。
	_, ok = limiter.AcquireUserTestSlot(context.Background(), 42, 2)
	require.True(t, ok, "second slot is still within maxConcurrent=2")
}

// TestUserTestSlotRejectsOverLimitAndRefunds 覆盖超限归还。
//
// 归还这一步很容易漏：不还的话，一次被拒的请求会永久占着一格，
// 用户的可用并发会被越挤越小，直到再也测不了。
func TestUserTestSlotRejectsOverLimitAndRefunds(t *testing.T) {
	redis := newFakeRedis()
	limiter := New(redis, nil, slog.New(slog.DiscardHandler))

	for i := 0; i < 2; i++ {
		_, ok := limiter.AcquireUserTestSlot(context.Background(), 42, 2)
		require.True(t, ok)
	}
	// 第三次超限。
	release, ok := limiter.AcquireUserTestSlot(context.Background(), 42, 2)
	require.False(t, ok)
	require.Nil(t, release)

	// 超限的那次必须把自增还回去：计数应仍为 2，而不是 3。
	redis.mu.Lock()
	count := redis.counts[userTestInflightPrefix+itoa(42)]
	redis.mu.Unlock()
	require.Equal(t, int64(2), count, "a rejected acquire must refund its increment")
}

// TestUserTestSlotReleaseIsIdempotent 覆盖释放函数的 sync.Once 保护。
//
// 调用方可能既在 defer 里释放、又在错误分支里释放；重复归还
// 会把计数越减越小，最终让并发限制形同失效。
func TestUserTestSlotReleaseIsIdempotent(t *testing.T) {
	redis := newFakeRedis()
	limiter := New(redis, nil, slog.New(slog.DiscardHandler))

	release, ok := limiter.AcquireUserTestSlot(context.Background(), 42, 1)
	require.True(t, ok)

	release()
	release()
	release()

	redis.mu.Lock()
	count := redis.counts[userTestInflightPrefix+itoa(42)]
	redis.mu.Unlock()
	require.Equal(t, int64(0), count, "release must only decrement once")
}

// TestUserTestSlotIsPerUser 保证不同用户互不影响。
func TestUserTestSlotIsPerUser(t *testing.T) {
	redis := newFakeRedis()
	limiter := New(redis, nil, slog.New(slog.DiscardHandler))

	_, ok := limiter.AcquireUserTestSlot(context.Background(), 1, 1)
	require.True(t, ok)
	_, ok = limiter.AcquireUserTestSlot(context.Background(), 2, 1)
	require.True(t, ok, "another user must have their own quota")
}

// TestUserTestSlotBypassedWhenUnlimitedOrAnonymous 覆盖两条短路。
//
// maxConcurrent<=0 是「不限制」，userID<=0 是「没有身份」，
// 两者都不该去碰 Redis（后者还会让所有匿名请求共用一个计数器）。
func TestUserTestSlotBypassedWhenUnlimitedOrAnonymous(t *testing.T) {
	redis := newFakeRedis()
	limiter := New(redis, nil, slog.New(slog.DiscardHandler))

	_, ok := limiter.AcquireUserTestSlot(context.Background(), 42, 0)
	require.True(t, ok, "maxConcurrent=0 means unlimited")
	_, ok = limiter.AcquireUserTestSlot(context.Background(), 0, 2)
	require.True(t, ok, "anonymous callers bypass the limiter")
	require.Equal(t, 0, redis.incrCalls, "neither case may touch redis")
}

// TestUserTestSlotAllowsWithoutRedis 覆盖未装配 Redis 的降级。
//
// 这里与上面的 fail-closed 不冲突：nil client 表示「本部署没启用
// Redis 限流」，是配置选择；err 表示「启用了但坏了」，是故障。
// 配置选择放行，故障拒绝。
func TestUserTestSlotAllowsWithoutRedis(t *testing.T) {
	limiter := New(nil, nil, slog.New(slog.DiscardHandler))

	_, ok := limiter.AcquireUserTestSlot(context.Background(), 42, 2)
	require.True(t, ok, "a nil redis client means the feature is not configured")
}
