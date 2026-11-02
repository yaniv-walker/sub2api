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

	// 250 依赖 249 建出的 iq_benchmark_tasks，必须按序应用。
	// 它只重写 status 的 CHECK 约束（加 invalid_target），不改数据行。
	migration250, err := migrations.FS.ReadFile("250_iq_benchmark_invalid_target.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(migration250))
	require.NoError(t, err, "migration 250 must apply on top of 249")
	// 幂等性：DROP ... IF EXISTS + ADD，重复执行必须安全。
	_, err = db.Exec(string(migration250))
	require.NoError(t, err, "migration 250 must be safely repeatable")

	// 251 依赖 249 建出的 artifacts/cases，必须按序应用。
	// 它把 run_id/task_id 放宽为可空（用户自测无 run/task），并新增
	// source / triggered_by_user_id / moderation_status 与 user_tests 表。
	migration251, err := migrations.FS.ReadFile("251_iq_benchmark_user_test.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(migration251))
	require.NoError(t, err, "migration 251 must apply on top of 250")
	// 幂等性：ADD COLUMN IF NOT EXISTS + DROP CONSTRAINT IF EXISTS + CREATE TABLE/INDEX
	// IF NOT EXISTS，重复执行必须安全（这也是迁移器「每个文件一个事务」的前提）。
	_, err = db.Exec(string(migration251))
	require.NoError(t, err, "migration 251 must be safely repeatable")

	// 252 只给 artifacts 加 reviewed_by / reviewed_at 两列（审核留痕），
	// 不依赖 251 之外的结构，但仍须按序应用。
	migration252, err := migrations.FS.ReadFile("252_iq_benchmark_artifact_review.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(migration252))
	require.NoError(t, err, "migration 252 must apply on top of 251")
	// 幂等性：ADD COLUMN IF NOT EXISTS，重复执行必须安全。
	_, err = db.Exec(string(migration252))
	require.NoError(t, err, "migration 252 must be safely repeatable")
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
		"iq_benchmark_cases":      {"idx_iq_case_listing"},
		"iq_benchmark_tasks":      {"uq_iq_task_group_model_enabled", "idx_iq_task_due", "idx_iq_task_group_model"},
		"iq_benchmark_runs":       {"idx_iq_run_task", "idx_iq_run_pending_cost"},
		"iq_benchmark_artifacts":  {"idx_iq_artifact_run", "idx_iq_artifact_feed", "idx_iq_artifact_filters", "idx_iq_artifact_status", "idx_iq_artifact_task_live", "idx_iq_artifact_source_feed"},
		"iq_benchmark_user_tests": {"idx_iq_usertest_user", "idx_iq_usertest_dedupe", "idx_iq_usertest_pending", "idx_iq_usertest_artifact"},
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

// TestIQBenchmarkUserTestArtifactKeepsNullRunAndTask 覆盖迁移 251 的核心语义：
// 用户自测的产出**没有** run/task，两列必须真的是 NULL 而不是被伪造成 0。
//
// 这是「领域层用 0 表示不存在、数据库用 NULL 表示不存在」这一映射的
// 端到端证明：写入时传 0，读回时仍为 0，而库里实际存的是 NULL。
func TestIQBenchmarkUserTestArtifactKeepsNullRunAndTask(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()
	repo := iqTestRepo(db)

	created, err := repo.CreateArtifact(ctx, iqbenchmark.ArtifactCreateInput{
		// RunID / TaskID 刻意为 0：仓库层必须把它们写成 NULL。
		RunID:             0,
		TaskID:            0,
		CaseID:            1,
		CaseSnapshot:      map[string]any{"name": "selftest"},
		GroupID:           7,
		Model:             "gpt-4o",
		RenderKind:        iqbenchmark.RenderKindMarkdown,
		Status:            iqbenchmark.ArtifactStatusSuccess,
		Source:            iqbenchmark.SourceUserTest,
		TriggeredByUserID: 42,
	})
	require.NoError(t, err)

	// 数据库里必须是 NULL，而不是 0。
	var runID, taskID sql.NullInt64
	var triggeredBy sql.NullInt64
	var source, moderation string
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT run_id, task_id, triggered_by_user_id, source, moderation_status
		   FROM iq_benchmark_artifacts WHERE id = $1`, created.ID).
		Scan(&runID, &taskID, &triggeredBy, &source, &moderation))
	require.False(t, runID.Valid, "run_id must be SQL NULL for a user self-test artifact")
	require.False(t, taskID.Valid, "task_id must be SQL NULL for a user self-test artifact")
	require.True(t, triggeredBy.Valid)
	require.Equal(t, int64(42), triggeredBy.Int64)
	require.Equal(t, iqbenchmark.SourceUserTest, source)
	require.Equal(t, iqbenchmark.ModerationApproved, moderation,
		"moderation_status must default to approved when not specified")

	// 读回领域对象时折叠为 0（而不是报错或返回假 ID）。
	got, err := repo.GetArtifact(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, int64(0), got.RunID)
	require.Equal(t, int64(0), got.TaskID)
	require.Equal(t, int64(42), got.TriggeredByUserID)
	require.Equal(t, iqbenchmark.SourceUserTest, got.Source)
}

// TestIQBenchmarkModerateArtifactPersistsReview 覆盖审核动作的落库与留痕。
//
// 这是「待审产出能被放行」的端到端证明：写入 pending → 审核为 approved →
// 公开侧的过滤条件（moderation_status='approved'）能查到它。
// 没有这条链路，require_review 打开后用户测出的作品永远进不了公开页。
func TestIQBenchmarkModerateArtifactPersistsReview(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()
	repo := iqTestRepo(db)

	created, err := repo.CreateArtifact(ctx, iqbenchmark.ArtifactCreateInput{
		CaseID:            1,
		CaseSnapshot:      map[string]any{"name": "pending-one"},
		GroupID:           7,
		Model:             "gpt-4o",
		RenderKind:        iqbenchmark.RenderKindMarkdown,
		Status:            iqbenchmark.ArtifactStatusSuccess,
		Source:            iqbenchmark.SourceUserTest,
		TriggeredByUserID: 42,
		ModerationStatus:  iqbenchmark.ModerationPending,
	})
	require.NoError(t, err)

	// 审核前：公开侧口径（approved）查不到它。
	before, err := repo.ListArtifacts(ctx, iqbenchmark.ArtifactListFilter{
		Source:           iqbenchmark.SourceUserTest,
		ModerationStatus: iqbenchmark.ModerationApproved,
		Page:             1,
		PageSize:         10,
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), before.Total, "pending artifacts must not be publicly visible")

	// 管理员放行。
	require.NoError(t, repo.ModerateArtifact(ctx, created.ID, iqbenchmark.ModerationApproved, 7, time.Now()))

	// 审核后：能查到，且留痕可读回。
	after, err := repo.ListArtifacts(ctx, iqbenchmark.ArtifactListFilter{
		Source:           iqbenchmark.SourceUserTest,
		ModerationStatus: iqbenchmark.ModerationApproved,
		Page:             1,
		PageSize:         10,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), after.Total, "approved artifact must become publicly visible")

	got, err := repo.GetArtifact(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, iqbenchmark.ModerationApproved, got.ModerationStatus)
	require.Equal(t, int64(7), got.ReviewedBy, "the reviewer must be recorded")
	require.NotNil(t, got.ReviewedAt, "the review time must be recorded")

	// 不存在的产出必须报错，不能静默「更新 0 行」。
	require.ErrorIs(t,
		repo.ModerateArtifact(ctx, 999999, iqbenchmark.ModerationApproved, 7, time.Now()),
		iqbenchmark.ErrArtifactNotFound)
}

// TestIQBenchmarkArtifactSourceConsistencyCheck 保证 source 与 triggered_by_user_id
// 的自洽约束在库层面生效 —— 这条不变量是「按来源统计」可信的前提。
func TestIQBenchmarkArtifactSourceConsistencyCheck(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()

	// scheduled 却带了触发用户：必须被拒。
	_, err := db.ExecContext(ctx, `
		INSERT INTO iq_benchmark_artifacts
			(case_id, case_snapshot, group_id, model, render_kind, status, source, triggered_by_user_id)
		VALUES (1, '{}'::jsonb, 1, 'm', 'markdown', 'success', 'scheduled', 42)`)
	require.Error(t, err, "scheduled + triggering user must violate iq_artifact_source_consistent")

	// user_test 却没有触发用户：必须被拒。
	_, err = db.ExecContext(ctx, `
		INSERT INTO iq_benchmark_artifacts
			(case_id, case_snapshot, group_id, model, render_kind, status, source, triggered_by_user_id)
		VALUES (1, '{}'::jsonb, 1, 'm', 'markdown', 'success', 'user_test', NULL)`)
	require.Error(t, err, "user_test without a triggering user must violate iq_artifact_source_consistent")

	// 非法来源值：必须被拒。
	_, err = db.ExecContext(ctx, `
		INSERT INTO iq_benchmark_artifacts
			(case_id, case_snapshot, group_id, model, render_kind, status, source)
		VALUES (1, '{}'::jsonb, 1, 'm', 'markdown', 'success', 'all')`)
	require.Error(t, err, "'all' is a query filter, never a storable source")

	// 合法的定时产出：NULL 触发用户 + scheduled 必须被接受。
	_, err = db.ExecContext(ctx, `
		INSERT INTO iq_benchmark_artifacts
			(case_id, case_snapshot, group_id, model, render_kind, status, source, triggered_by_user_id)
		VALUES (1, '{}'::jsonb, 1, 'm', 'markdown', 'success', 'scheduled', NULL)`)
	require.NoError(t, err, "scheduled without a triggering user must be accepted")
}

// TestIQBenchmarkListArtifactsFiltersBySource 保证来源过滤真的下推到 SQL：
// 默认口径（scheduled）绝不能把用户自测混进「模型排行」。
func TestIQBenchmarkListArtifactsFiltersBySource(t *testing.T) {
	db := iqTestDB(t)
	ctx := context.Background()
	repo := iqTestRepo(db)

	mk := func(source string, userID int64) {
		_, err := repo.CreateArtifact(ctx, iqbenchmark.ArtifactCreateInput{
			RunID:             0,
			TaskID:            0,
			CaseID:            1,
			CaseSnapshot:      map[string]any{"name": "s"},
			GroupID:           3,
			Model:             "gpt-4o",
			RenderKind:        iqbenchmark.RenderKindMarkdown,
			Status:            iqbenchmark.ArtifactStatusSuccess,
			Source:            source,
			TriggeredByUserID: userID,
		})
		require.NoError(t, err)
	}
	mk(iqbenchmark.SourceScheduled, 0)
	mk(iqbenchmark.SourceUserTest, 42)
	mk(iqbenchmark.SourceUserTest, 43)

	scheduled, err := repo.ListArtifacts(ctx, iqbenchmark.ArtifactListFilter{
		Source: iqbenchmark.SourceScheduled, Page: 1, PageSize: 10,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), scheduled.Total, "scheduled scope must exclude user self-tests")

	userTests, err := repo.ListArtifacts(ctx, iqbenchmark.ArtifactListFilter{
		Source: iqbenchmark.SourceUserTest, Page: 1, PageSize: 10,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), userTests.Total)

	all, err := repo.ListArtifacts(ctx, iqbenchmark.ArtifactListFilter{
		Source: "", Page: 1, PageSize: 10,
	})
	require.NoError(t, err)
	require.Equal(t, int64(3), all.Total, "an empty source filter must return the full set")
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
		iqbenchmark.TaskStatusInvalidTarget,
	} {
		require.NotEmpty(t, status)
		_, err := db.ExecContext(ctx, `UPDATE iq_benchmark_tasks SET status = $1 WHERE id = $2`, status, id)
		require.NoError(t, err, "service constant %q must be accepted by the CHECK constraint", status)
	}
}
