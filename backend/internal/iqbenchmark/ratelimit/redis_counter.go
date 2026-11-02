package ratelimit

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrLimiterUnavailable 表示限流后端不可用（此时调用方应按 fail-open 处理）。
var ErrLimiterUnavailable = errors.New("iqbenchmark: rate limiter backend unavailable")

// RedisCounter 用 go-redis 实现 RedisClient。
//
// 放在独立子包 ratelimit 内，实现「限流后端访问」这一件事：
// 既满足 depguard 对 internal/service 的禁用规则（本包不是 service），
// 也让限流器本体保持零 Redis 依赖，便于单测替换。
type RedisCounter struct {
	client *redis.Client
}

// NewRedisCounter 构造 Redis 计数器。
func NewRedisCounter(client *redis.Client) *RedisCounter {
	return &RedisCounter{client: client}
}

var _ RedisClient = (*RedisCounter)(nil)

// incrWithTTLScript 原子地自增并在首次写入时设置 TTL。
//
// 必须用 Lua：分两次调用（INCR 再 EXPIRE）在并发下会漏设过期时间，
// 导致键永不过期、额度永久被锁死。
var incrWithTTLScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
if current == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return current
`)

// IncrWithTTL 原子自增并在首次写入时设置过期时间。
func (r *RedisCounter) IncrWithTTL(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if r == nil || r.client == nil {
		return 0, ErrLimiterUnavailable
	}
	value, err := incrWithTTLScript.Run(ctx, r.client, []string{key}, ttl.Milliseconds()).Int64()
	if err != nil {
		return 0, err
	}
	return value, nil
}

// decrScript 把值减一，且永不小于 0（避免计数被减成负数后长期占位）。
var decrScript = redis.NewScript(`
local current = tonumber(redis.call('GET', KEYS[1]) or '0')
if current <= 0 then
  redis.call('DEL', KEYS[1])
  return 0
end
return redis.call('DECR', KEYS[1])
`)

// Decr 减一（不小于 0）。
func (r *RedisCounter) Decr(ctx context.Context, key string) (int64, error) {
	if r == nil || r.client == nil {
		return 0, ErrLimiterUnavailable
	}
	return decrScript.Run(ctx, r.client, []string{key}).Int64()
}

// SetNX 键不存在时写入。
func (r *RedisCounter) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	if r == nil || r.client == nil {
		return false, ErrLimiterUnavailable
	}
	return r.client.SetNX(ctx, key, value, ttl).Result()
}

// Delete 删除键。
func (r *RedisCounter) Delete(ctx context.Context, key string) error {
	if r == nil || r.client == nil {
		return ErrLimiterUnavailable
	}
	return r.client.Del(ctx, key).Err()
}
