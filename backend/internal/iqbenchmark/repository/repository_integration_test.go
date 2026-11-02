//go:build integration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	// ent/runtime 在 init 阶段注册各 schema 的运行期默认值（如 UpdatedAt）。
	// 缺了它，UpdateOneID(...).Save() 会在 defaults() 里解引用 nil 函数而 panic。
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/migrations"
)

// iqTestDB 建立一个一次性的、随机 schema 的 PostgreSQL 连接，并只跑作品展的迁移。
//
// 设计取舍：
//   - 只接受专用库（`iq_test`），绝不允许指向生产库；
//   - 每次运行拥有独立 schema，清理时只 DROP 自己那个 schema；
//   - 不跑全部 249 个迁移，只跑本功能依赖的最小前置表 + 249，
//     这样集成测试既快又不与上游迁移的演进耦合。
func iqTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("IQ_BENCHMARK_TEST_DSN")
	if dsn == "" {
		t.Skip("IQ_BENCHMARK_TEST_DSN not set (disposable PostgreSQL)")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "/iq_test", u.Path, "only a disposable iq_test database is allowed")

	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	require.NoError(t, admin.Ping(), "PostgreSQL must be reachable")

	schema := fmt.Sprintf("iq_it_%d", time.Now().UnixNano())
	_, err = admin.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)

	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	db.SetMaxOpenConns(16)

	t.Cleanup(func() {
		// Close 的 error 在清理阶段无补救手段，显式丢弃以通过 errcheck。
		_ = db.Close()
		if _, dropErr := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); dropErr != nil {
			t.Errorf("drop schema %s: %v", schema, dropErr)
		}
		_ = admin.Close()
	})

	// 249 只引用 groups（逻辑外键，刻意不建 FK）与自身四张表，
	// 因此这里只需要一个最小的 groups 表让 SQL 语义完整。
	_, err = db.Exec(`
		CREATE TABLE groups (id BIGSERIAL PRIMARY KEY, name TEXT NOT NULL, deleted_at TIMESTAMPTZ);
		CREATE TABLE users (id BIGSERIAL PRIMARY KEY, email TEXT, deleted_at TIMESTAMPTZ);
		CREATE TABLE usage_logs (id BIGSERIAL PRIMARY KEY, user_id BIGINT, api_key_id BIGINT,
			input_tokens BIGINT DEFAULT 0, output_tokens BIGINT DEFAULT 0,
			actual_cost NUMERIC(20,10) DEFAULT 0, created_at TIMESTAMPTZ DEFAULT NOW());
	`)
	require.NoError(t, err)

	migration, err := migrations.FS.ReadFile("249_iq_benchmark_gallery.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err, "migration 249 must apply to an empty database")
	// 幂等性：全部语句都是 IF NOT EXISTS，重复执行必须安全。
	_, err = db.Exec(string(migration))
	require.NoError(t, err, "migration 249 must be safely repeatable")
	return db
}

// iqTestRepo 构造一个同时具备 Ent 客户端与 *sql.DB 的仓储（与生产装配一致）。
//
// 只用一个 client 是必要的：Ent 的 Update 走 client，而日成本这类聚合查询
// 直接走 db；两者必须指向同一个 schema，否则测试会看不到自己写入的行。
func iqTestRepo(db *sql.DB) *Repository {
	return New(dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db))), db)
}

// TestIQBenchmarkMigrationAppliesCleanly 覆盖任务 1.8 的建表部分。
func TestIQBenchmarkMigrationAppliesCleanly(t *testing.T) {
	db := iqTestDB(t)

	for _, table := range []string{
		"iq_benchmark_cases",
		"iq_benchmark_tasks",
		"iq_benchmark_runs",
		"iq_benchmark_artifacts",
	} {
		var exists bool
		require.NoError(t, db.QueryRow(
			`SELECT EXISTS(SELECT 1 FROM information_schema.tables
			   WHERE table_schema = current_schema() AND table_name = $1)`, table,
		).Scan(&exists), table)
		require.True(t, exists, "table %s must exist", table)
	}
}

// TestIQBenchmarkPartialUniqueEnabledTask 覆盖任务 1.8：
// 同一 (group_id, model) 只能有一个 enabled=true 的任务，但可以有任意多个停用的。
func TestIQBenchmarkPartialUniqueEnabledTask(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()

	insert := func(name string, groupID int64, model string, enabled bool) error {
		_, err := db.ExecContext(ctx, `
			INSERT INTO iq_benchmark_tasks (name, group_id, model, case_ids, interval_minutes, enabled)
			VALUES ($1, $2, $3, '[]'::jsonb, 60, $4)`, name, groupID, model, enabled)
		return err
	}

	require.NoError(t, insert("first-enabled", 7, "gpt-4o", true))

	// 同组同模型再建一个启用任务：必须被部分唯一索引拒绝。
	err := insert("duplicate-enabled", 7, "gpt-4o", true)
	require.Error(t, err, "a second enabled task for the same (group, model) must be rejected")

	// 停用任务不受约束限制。
	require.NoError(t, insert("disabled-a", 7, "gpt-4o", false))
	require.NoError(t, insert("disabled-b", 7, "gpt-4o", false))

	// 不同分组或不同模型各自独立。
	require.NoError(t, insert("other-group", 8, "gpt-4o", true))
	require.NoError(t, insert("other-model", 7, "gpt-4.1", true))

	var enabledCount int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM iq_benchmark_tasks WHERE group_id = 7 AND model = 'gpt-4o' AND enabled = TRUE`,
	).Scan(&enabledCount))
	require.Equal(t, 1, enabledCount, "exactly one enabled task per (group, model)")
}

// TestIQBenchmarkClaimSkipsLockedRows 覆盖任务 1.8 的 SKIP LOCKED 语义：
// 一个事务持锁时，另一个事务不得领到同一行。
func TestIQBenchmarkClaimSkipsLockedRows(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO iq_benchmark_tasks (name, group_id, model, case_ids, interval_minutes, enabled, next_run_at)
		VALUES ('due-1', 1, 'm', '[]'::jsonb, 60, TRUE, NOW() - INTERVAL '1 hour'),
		       ('due-2', 2, 'm', '[]'::jsonb, 60, TRUE, NOW() - INTERVAL '1 hour')`)
	require.NoError(t, err)

	// 事务 A 锁住第一行。
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()

	var lockedID int64
	require.NoError(t, tx.QueryRowContext(ctx, `
		SELECT id FROM iq_benchmark_tasks
		 WHERE enabled = TRUE AND next_run_at <= NOW()
		 ORDER BY next_run_at, id
		 LIMIT 1
		 FOR UPDATE SKIP LOCKED`).Scan(&lockedID))

	// 事务 B 用同样的语句必须跳过被锁的那一行，领到另一行。
	var otherID int64
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT id FROM iq_benchmark_tasks
		 WHERE enabled = TRUE AND next_run_at <= NOW()
		 ORDER BY next_run_at, id
		 LIMIT 1
		 FOR UPDATE SKIP LOCKED`).Scan(&otherID))

	require.NotEqual(t, lockedID, otherID, "SKIP LOCKED must not hand out an already-locked row")
}

// TestIQBenchmarkLeaseTakeover 覆盖任务 1.8 / 3.10 的租约接管：
// 过期租约的任务可以被另一个实例重新领取，未过期的不行。
func TestIQBenchmarkLeaseTakeover(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()

	id, err := iqInsertTask(t, db, "lease", 3, "m", 60)
	require.NoError(t, err)

	// 实例 A 领取并持有一个**已过期**的租约。
	_, err = db.ExecContext(ctx, `
		UPDATE iq_benchmark_tasks
		   SET lease_owner = 'inst-a', lease_expires_at = NOW() - INTERVAL '1 minute'
		 WHERE id = $1`, id)
	require.NoError(t, err)

	// 实例 B 可以接管（条件里带租约过期判断）。
	res, err := db.ExecContext(ctx, `
		UPDATE iq_benchmark_tasks
		   SET lease_owner = 'inst-b', lease_expires_at = NOW() + INTERVAL '90 seconds'
		 WHERE id = $1 AND (lease_expires_at IS NULL OR lease_expires_at < NOW())`, id)
	require.NoError(t, err)
	affected, err := res.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 1, affected, "an expired lease must be takeoverable")

	var owner string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT lease_owner FROM iq_benchmark_tasks WHERE id = $1`, id).Scan(&owner))
	require.Equal(t, "inst-b", owner)

	// 现在租约有效，实例 C 不得接管。
	res, err = db.ExecContext(ctx, `
		UPDATE iq_benchmark_tasks
		   SET lease_owner = 'inst-c', lease_expires_at = NOW() + INTERVAL '90 seconds'
		 WHERE id = $1 AND (lease_expires_at IS NULL OR lease_expires_at < NOW())`, id)
	require.NoError(t, err)
	affected, err = res.RowsAffected()
	require.NoError(t, err)
	require.EqualValues(t, 0, affected, "a live lease must not be takeoverable")
}

// iqInsertTask 插入一个到期任务并返回 id。
func iqInsertTask(t *testing.T, db *sql.DB, name string, groupID int64, model string, intervalMinutes int) (int64, error) {
	t.Helper()
	var id int64
	err := db.QueryRow(`
		INSERT INTO iq_benchmark_tasks (name, group_id, model, case_ids, interval_minutes, enabled, next_run_at)
		VALUES ($1, $2, $3, '[]'::jsonb, $4, TRUE, NOW() - INTERVAL '1 minute')
		RETURNING id`, name, groupID, model, intervalMinutes).Scan(&id)
	return id, err
}

// TestIQBenchmarkEntSchemaMatchesMigration 保证 Ent schema 与迁移的索引名一致
// （历史上重复索引名曾导致 `go generate ./ent` 生成的 DDL 与迁移冲突）。
func TestIQBenchmarkEntSchemaMatchesMigration(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()

	expected := map[string][]string{
		"iq_benchmark_cases":     {"idx_iq_case_listing"},
		"iq_benchmark_tasks":     {"uq_iq_task_group_model_enabled", "idx_iq_task_due", "idx_iq_task_group_model"},
		"iq_benchmark_runs":      {"idx_iq_run_task", "idx_iq_run_pending_cost"},
		"iq_benchmark_artifacts": {"idx_iq_artifact_run", "idx_iq_artifact_feed", "idx_iq_artifact_filters", "idx_iq_artifact_status", "idx_iq_artifact_task_live"},
	}
	for table, indexes := range expected {
		for _, index := range indexes {
			var exists bool
			require.NoError(t, db.QueryRowContext(ctx,
				`SELECT EXISTS(SELECT 1 FROM pg_indexes
				   WHERE schemaname = current_schema() AND tablename = $1 AND indexname = $2)`,
				table, index).Scan(&exists))
			require.True(t, exists, "index %s on %s must exist", index, table)
		}
	}
}

// TestIQBenchmarkTaskStatusCheck 保证 status 的 CHECK 约束确实生效
// （枚举值变更必须同时改迁移与服务层常量）。
func TestIQBenchmarkTaskStatusCheck(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()

	id, err := iqInsertTask(t, db, "status-check", 4, "m", 60)
	require.NoError(t, err)

	for _, status := range []string{
		"idle", "running", "quota_exhausted", "budget_exhausted", "account_disabled", "paused_by_admin",
	} {
		_, err := db.ExecContext(ctx, `UPDATE iq_benchmark_tasks SET status = $1 WHERE id = $2`, status, id)
		require.NoError(t, err, "status %s must be accepted", status)
	}

	_, err = db.ExecContext(ctx, `UPDATE iq_benchmark_tasks SET status = 'not_a_status' WHERE id = $1`, id)
	require.Error(t, err, "an unknown status must be rejected by the CHECK constraint")
}

// TestIQBenchmarkIntervalCheck 保证区间约束（15 分钟 ~ 7 天）在库层面生效。
func TestIQBenchmarkIntervalCheck(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()

	for _, minutes := range []int{14, 10081} {
		_, err := db.ExecContext(ctx, `
			INSERT INTO iq_benchmark_tasks (name, group_id, model, case_ids, interval_minutes)
			VALUES ('bad-interval', 5, 'm', '[]'::jsonb, $1)`, minutes)
		require.Error(t, err, "interval_minutes=%d must be rejected", minutes)
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO iq_benchmark_tasks (name, group_id, model, case_ids, interval_minutes)
		VALUES ('ok-min', 6, 'm', '[]'::jsonb, 15),
		       ('ok-max', 6, 'm2', '[]'::jsonb, 10080)`)
	require.NoError(t, err, "the boundary values 15 and 10080 must be accepted")
}

// TestIQBenchmarkServiceStatusValuesMatchMigration 断言服务层常量与迁移 CHECK 列表一致。
//
// 这两处是最容易各自漂移的地方：改了迁移忘了改常量，或反之。
func TestIQBenchmarkServiceStatusValuesMatchMigration(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()

	id, err := iqInsertTask(t, db, "status-consts", 9, "m", 60)
	require.NoError(t, err)

	// 服务层声明的每个状态都必须能被库接受。
	for _, status := range []string{
		iqbenchmark.TaskStatusIdle,
		iqbenchmark.TaskStatusRunning,
		iqbenchmark.TaskStatusQuotaExhausted,
		iqbenchmark.TaskStatusBudgetExhausted,
		iqbenchmark.TaskStatusAccountDisabled,
		iqbenchmark.TaskStatusPausedByAdmin,
	} {
		require.NotEmpty(t, status)
		_, err := db.ExecContext(ctx, `UPDATE iq_benchmark_tasks SET status = $1 WHERE id = $2`, status, id)
		require.NoError(t, err, "service constant %q must be accepted by the CHECK constraint", status)
	}
}
