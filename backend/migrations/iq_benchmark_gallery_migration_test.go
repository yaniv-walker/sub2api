//go:build unit

package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const iqBenchmarkMigration = "249_iq_benchmark_gallery.sql"

func readIQBenchmarkMigration(t *testing.T) string {
	t.Helper()
	content, err := FS.ReadFile(iqBenchmarkMigration)
	require.NoError(t, err)
	return string(content)
}

// stripSQLComments 去掉 `--` 行注释，避免注释里的说明文字（例如
// 「不扩展 users_signup_source_check」）被误判为实际执行的 DDL。
func stripSQLComments(sql string) string {
	lines := strings.Split(sql, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func TestMigration249CreatesOnlyNewTables(t *testing.T) {
	sql := readIQBenchmarkMigration(t)

	for _, table := range []string{
		"iq_benchmark_cases",
		"iq_benchmark_tasks",
		"iq_benchmark_runs",
		"iq_benchmark_artifacts",
	} {
		require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS "+table)
	}
}

// 硬约束：不给既有大表加列、不改既有 CHECK 约束、不新增 role 枚举。
func TestMigration249DoesNotAlterUpstreamTables(t *testing.T) {
	sql := stripSQLComments(readIQBenchmarkMigration(t))

	require.NotContains(t, sql, "ALTER TABLE users")
	require.NotContains(t, sql, "ALTER TABLE usage_logs")
	require.NotContains(t, sql, "ALTER TABLE accounts")
	require.NotContains(t, sql, "ALTER TABLE groups")
	require.NotContains(t, sql, "users_signup_source_check")
	require.NotContains(t, sql, "ADD COLUMN")
	require.NotContains(t, sql, "CREATE TRIGGER")
}

// 同「分组 × 模型」最多一个启用中任务：必须由数据库部分唯一索引强制，
// 不能只靠应用层检查（并发创建时应用层检查会漏）。
func TestMigration249EnforcesSingleEnabledTaskPerGroupModel(t *testing.T) {
	sql := readIQBenchmarkMigration(t)

	require.Contains(t, sql, "CREATE UNIQUE INDEX IF NOT EXISTS uq_iq_task_group_model_enabled")
	require.Contains(t, sql, "ON iq_benchmark_tasks (group_id, model)")
	require.Contains(t, sql, "WHERE enabled = TRUE")
}

// 间隔与抖动必须有数据库级 CHECK 兜底（service 层校验可能被绕过）。
func TestMigration249ConstrainsIntervalAndJitter(t *testing.T) {
	sql := readIQBenchmarkMigration(t)

	require.Contains(t, sql, "interval_minutes >= 15 AND interval_minutes <= 10080")
	require.Contains(t, sql, "jitter_seconds >= 0")
}

// 任务状态枚举必须覆盖三类熔断 + 人工暂停，否则熔断后无法落库。
func TestMigration249CoversAllTaskStates(t *testing.T) {
	sql := readIQBenchmarkMigration(t)

	for _, status := range []string{
		"idle", "running", "quota_exhausted",
		"budget_exhausted", "account_disabled", "paused_by_admin",
	} {
		require.Contains(t, sql, "'"+status+"'")
	}
}

// 产出必须能承载失败作品：状态含 failed / timeout / blocked。
func TestMigration249ArtifactStatusSupportsFailedWorks(t *testing.T) {
	sql := readIQBenchmarkMigration(t)

	require.Contains(t, sql, "'success', 'failed', 'timeout', 'blocked'")
}

// 题目快照与危险标记必须为 JSONB，否则无法按题面回溯与记录扫描结果。
func TestMigration249StoresSnapshotAndUnsafeFlags(t *testing.T) {
	sql := readIQBenchmarkMigration(t)

	require.Contains(t, sql, "case_snapshot JSONB NOT NULL")
	require.Contains(t, sql, "unsafe_flags JSONB NOT NULL DEFAULT '[]'::jsonb")
	require.Contains(t, sql, "jsonb_typeof(case_snapshot) = 'object'")
	require.Contains(t, sql, "jsonb_typeof(unsafe_flags) = 'array'")
}

// 作品展主查询与筛选必须有部分索引支撑（deleted_at IS NULL）。
func TestMigration249IndexesFeedAndFilters(t *testing.T) {
	sql := readIQBenchmarkMigration(t)

	require.Contains(t, sql, "idx_iq_artifact_feed")
	require.Contains(t, sql, "idx_iq_artifact_filters")
	require.Contains(t, sql, "idx_iq_artifact_status")
	require.Contains(t, sql, "WHERE deleted_at IS NULL")
}

// 任务表故意不加 group 外键：加了会让上游删除分组被本表阻断，
// 改变既有行为语义（这是硬约束的一部分）。
func TestMigration249DoesNotAddGroupForeignKey(t *testing.T) {
	sql := stripSQLComments(readIQBenchmarkMigration(t))

	require.NotContains(t, sql, "REFERENCES groups")
	require.Contains(t, readIQBenchmarkMigration(t), "故意不加 FK")
}

// 执行记录必须保存内部账号与专用 Key，供统计过滤与审计。
func TestMigration249RunsRecordInternalAccount(t *testing.T) {
	sql := readIQBenchmarkMigration(t)

	require.Contains(t, sql, "internal_user_id BIGINT NOT NULL")
	require.Contains(t, sql, "internal_api_key_id BIGINT NOT NULL")
}

// 成本回采标记必须在，否则 usage_logs 异步落库时无法补偿。
func TestMigration249RunsTrackCostCollection(t *testing.T) {
	sql := readIQBenchmarkMigration(t)

	require.Contains(t, sql, "cost_collected_at TIMESTAMPTZ")
	require.Contains(t, sql, "idx_iq_run_pending_cost")
}

// blob_key 必须持久化，否则清理时无法删除对象存储里的文件。
func TestMigration249ArtifactsTrackBlobKey(t *testing.T) {
	sql := readIQBenchmarkMigration(t)

	require.Contains(t, sql, "blob_key TEXT")
}

// 迁移文件必须是幂等的（重复执行不能报错）。
func TestMigration249IsIdempotent(t *testing.T) {
	sql := readIQBenchmarkMigration(t)

	require.NotContains(t, strings.ToUpper(sql), "DROP TABLE")
	require.NotContains(t, strings.ToUpper(sql), "DROP INDEX")
	require.NotContains(t, strings.ToUpper(sql), "ALTER TABLE")
}
