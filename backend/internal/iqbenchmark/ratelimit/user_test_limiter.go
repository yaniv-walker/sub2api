package ratelimit

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// 自测专用的键前缀。
//
// 与浏览用的 `iqgallery:` 刻意分开：自测的在途计数是「业务并发配额」，
// 浏览计数是「防刷保护」，混在一个命名空间里会让两类阈值互相干扰。
const (
	userTestInflightPrefix = "iqtest:user:inflight:"
	// userTestConcurrencyTTL 是并发槽位的兜底过期时间。
	//
	// 进程崩溃时释放函数不会执行，槽位会被永久占用。给一个远大于
	// 单次执行时长的 TTL，让崩溃后的用户最多等这么久就能恢复，
	// 而不是永久被锁在「并发已满」。
	userTestConcurrencyTTL = 15 * time.Minute
)

// AcquireUserTestSlot 占用一个在途自测槽位，返回释放函数。
//
// fail-closed：Redis 异常时不放行。理由与浏览限流相反 ——
// 浏览限流 fail-open 最坏是多几次请求，而自测的每一次放行都对应**真实费用**，
// 所以保护性限制在成本面前必须 fail-closed。
func (l *GalleryRateLimiter) AcquireUserTestSlot(ctx context.Context, userID int64, maxConcurrent int) (func(), bool) {
	if userID <= 0 || maxConcurrent <= 0 {
		return func() {}, true
	}
	if l.redis == nil {
		return func() {}, true
	}
	key := userTestInflightPrefix + itoa(userID)
	count, err := l.redis.IncrWithTTL(ctx, key, userTestConcurrencyTTL)
	if err != nil {
		l.logger.Warn("iqbenchmark: self-test slot check failed, rejecting (fail-closed)",
			"user_id", userID, "error", err)
		return nil, false
	}
	if count > int64(maxConcurrent) {
		// 归还刚占用的那一格，避免超限请求被永久计入。
		if _, decrErr := l.redis.Decr(ctx, key); decrErr != nil {
			l.logger.Warn("iqbenchmark: release self-test slot failed", "error", decrErr)
		}
		return nil, false
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			relCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if _, decrErr := l.redis.Decr(relCtx, key); decrErr != nil {
				l.logger.Warn("iqbenchmark: release self-test slot failed", "error", decrErr)
			}
		})
	}, true
}

// itoa 是 strconv.FormatInt 的短别名，让键的拼接读起来更清楚。
func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}
