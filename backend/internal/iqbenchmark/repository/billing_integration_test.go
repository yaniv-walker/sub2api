//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/service"
)

// dayStartUTC 返回给定时刻当天 00:00（保留原时区），与 service.dayStart 语义一致。
func dayStartUTC(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
}

// TestIQBenchmarkDailyCostScopesToInternalKeyOnly 覆盖任务 2.6 的核心隔离要求：
// 日成本预算只统计**内部专用 Key** 的用量，真实用户的消耗绝不计入。
//
// 这是整个计费方案的安全边界：一旦这里串了，内部检测就会消耗真实用户的
// 预算（或反之被真实用户的用量提前熔断）。
func TestIQBenchmarkDailyCostScopesToInternalKeyOnly(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()

	const internalKeyID int64 = 9001
	const realUserKeyID int64 = 9002
	const otherUserKeyID int64 = 9003

	_, err := db.ExecContext(ctx, `
		INSERT INTO usage_logs (user_id, api_key_id, actual_cost, created_at) VALUES
			(1, $1, 0.25, NOW()),
			(1, $1, 0.15, NOW()),
			(2, $2, 9.99, NOW()),
			(3, $3, 5.00, NOW())`, internalKeyID, realUserKeyID, otherUserKeyID)
	require.NoError(t, err)

	repo := iqTestRepo(db)
	spent, err := repo.DailyCost(ctx, internalKeyID, dayStartUTC(time.Now()))
	require.NoError(t, err)
	require.InDelta(t, 0.40, spent, 1e-9, "only the internal key's spend may be counted")

	// 真实用户各自的成本互不影响，也不被内部 Key 的统计污染。
	realSpent, err := repo.DailyCost(ctx, realUserKeyID, dayStartUTC(time.Now()))
	require.NoError(t, err)
	require.InDelta(t, 9.99, realSpent, 1e-9)

	// 没有任何用量的 Key 返回 0 而不是错误。
	noneSpent, err := repo.DailyCost(ctx, 123456, dayStartUTC(time.Now()))
	require.NoError(t, err)
	require.InDelta(t, 0, noneSpent, 1e-9)
}

// TestIQBenchmarkDailyCostResetsNextDay 覆盖「次日自动恢复」：
// 预算按自然日滚动，昨天的消耗不得计入今天。
func TestIQBenchmarkDailyCostResetsNextDay(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()

	const internalKeyID int64 = 9101
	_, err := db.ExecContext(ctx, `
		INSERT INTO usage_logs (user_id, api_key_id, actual_cost, created_at) VALUES
			(1, $1, 5.00, NOW() - INTERVAL '2 days'),
			(1, $1, 0.10, NOW())`, internalKeyID)
	require.NoError(t, err)

	repo := iqTestRepo(db)
	spent, err := repo.DailyCost(ctx, internalKeyID, dayStartUTC(time.Now()))
	require.NoError(t, err)
	require.InDelta(t, 0.10, spent, 1e-9, "yesterday's spend must not count against today's budget")
}

// TestIQBenchmarkGuardBudgetBreaker 覆盖任务 2.6 的「超预算熔断」：
// 预算用尽时必须返回 exhausted=true，且把任务状态写成 budget_exhausted 并清空 next_run_at。
func TestIQBenchmarkGuardBudgetBreaker(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()

	const internalKeyID int64 = 9201
	taskID, err := iqInsertTask(t, db, "budget-breaker", 21, "m", 60)
	require.NoError(t, err)

	repo := iqTestRepo(db)
	guard := service.NewGuard(repo, nil)

	// 预算 1.0，已花 0.5：不应熔断。
	_, err = db.ExecContext(ctx, `INSERT INTO usage_logs (user_id, api_key_id, actual_cost, created_at) VALUES (1, $1, 0.5, NOW())`, internalKeyID)
	require.NoError(t, err)

	exhausted, spent, err := guard.CheckBudget(ctx, internalKeyID, 1.0, time.Now())
	require.NoError(t, err)
	require.False(t, exhausted)
	require.InDelta(t, 0.5, spent, 1e-9)

	// 再花 0.6 → 累计 1.1 超过预算：必须熔断。
	_, err = db.ExecContext(ctx, `INSERT INTO usage_logs (user_id, api_key_id, actual_cost, created_at) VALUES (1, $1, 0.6, NOW())`, internalKeyID)
	require.NoError(t, err)

	exhausted, spent, err = guard.CheckBudget(ctx, internalKeyID, 1.0, time.Now())
	require.NoError(t, err)
	require.True(t, exhausted, "spend above the budget must trip the breaker")
	require.InDelta(t, 1.1, spent, 1e-9)

	// 熔断写入任务状态：budget_exhausted，并清空 next_run_at（次日由调度器恢复）。
	require.NoError(t, repo.SetTaskStatus(ctx, taskID, iqbenchmark.TaskStatusBudgetExhausted, "daily budget exhausted", nil))

	var status string
	var nextRunAt *time.Time
	var reason *string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT status, next_run_at, status_reason FROM iq_benchmark_tasks WHERE id = $1`, taskID,
	).Scan(&status, &nextRunAt, &reason))
	require.Equal(t, iqbenchmark.TaskStatusBudgetExhausted, status)
	require.Nil(t, nextRunAt, "a budget-exhausted task must stop being scheduled")
	require.NotNil(t, reason)
}

// TestIQBenchmarkGuardBudgetDisabledWhenZero 覆盖「预算为 0 表示不限额」。
func TestIQBenchmarkGuardBudgetDisabledWhenZero(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()

	repo := iqTestRepo(db)
	guard := service.NewGuard(repo, nil)

	_, err := db.ExecContext(ctx, `INSERT INTO usage_logs (user_id, api_key_id, actual_cost, created_at) VALUES (1, 9301, 100.0, NOW())`)
	require.NoError(t, err)

	exhausted, _, err := guard.CheckBudget(ctx, 9301, 0, time.Now())
	require.NoError(t, err)
	require.False(t, exhausted, "budget<=0 disables the check, it must never trip")
}

// TestIQBenchmarkUsageSnapshotByRequestIDs 覆盖 run 结束后按 request_id 回采成本：
// 只统计本次 run 的 request_id，未命中的数量要如实报告（不猜、不补 0）。
func TestIQBenchmarkUsageSnapshotByRequestIDs(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()

	// iqTestDB 的精简 usage_logs 没有 request_id 列（那是上游迁移加的），
	// 这里按需补上，让回采 SQL 完整可跑。
	_, err := db.ExecContext(ctx, `ALTER TABLE usage_logs ADD COLUMN request_id TEXT`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		INSERT INTO usage_logs (user_id, api_key_id, request_id, actual_cost, input_tokens, output_tokens, created_at) VALUES
			(1, 9401, 'req-a', 0.10, 100, 200, NOW()),
			(1, 9401, 'req-b', 0.20, 300, 400, NOW()),
			(1, 9401, 'req-unrelated', 9.99, 9999, 9999, NOW())`)
	require.NoError(t, err)

	repo := iqTestRepo(db)
	snapshot, err := repo.SumUsageByRequestIDs(ctx, 9401, []string{"req-a", "req-b", "req-missing"})
	require.NoError(t, err)

	require.InDelta(t, 0.30, snapshot.Cost, 1e-9, "only the run's own request_ids may be summed")
	require.EqualValues(t, 400, snapshot.InputTokens)
	require.EqualValues(t, 600, snapshot.OutputTokens)
	require.EqualValues(t, 2, snapshot.Matched, "req-missing must not be counted as matched")

	// 空列表必须安全返回 0，不构造非法 SQL。
	empty, err := repo.SumUsageByRequestIDs(ctx, 9401, nil)
	require.NoError(t, err)
	require.InDelta(t, 0, empty.Cost, 1e-9)
	require.EqualValues(t, 0, empty.Matched)
}
