//go:build unit

package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

const (
	iqVerdictMigration      = "253_iq_benchmark_verdict.sql"
	iqVerdictIndexMigration = "254_iq_benchmark_verdict_index.sql"
)

func readIQVerdictMigration(t *testing.T) string {
	t.Helper()
	content, err := FS.ReadFile(iqVerdictMigration)
	require.NoError(t, err)
	return string(content)
}

func readIQVerdictIndexMigration(t *testing.T) string {
	t.Helper()
	content, err := FS.ReadFile(iqVerdictIndexMigration)
	require.NoError(t, err)
	return string(content)
}

// TestMigration253HasNoTransactionControl 断言迁移文件里没有 BEGIN/COMMIT。
//
// 迁移执行器（internal/repository/migrations_runner.go）已为每个文件
// 开启独立事务；文件内再写事务控制会提前提交外层事务，
// 破坏「一个迁移一个原子单元」的语义。这不是风格问题，是正确性问题。
func TestMigration253HasNoTransactionControl(t *testing.T) {
	for _, name := range []string{iqVerdictMigration, iqVerdictIndexMigration} {
		content, err := FS.ReadFile(name)
		require.NoError(t, err)
		code := stripSQLComments(string(content))
		upper := strings.ToUpper(code)
		require.NotContains(t, upper, "BEGIN;", "%s 不得包含 BEGIN;", name)
		require.NotContains(t, upper, "BEGIN TRANSACTION", "%s 不得包含 BEGIN TRANSACTION", name)
		require.NotContains(t, upper, "COMMIT;", "%s 不得包含 COMMIT;", name)
	}
}

// TestMigration253DoesNotAlterUpstreamTables 断言迁移只动本模块的四张表。
//
// 硬约束：不改上游核心文件/表。这里用「出现的 ALTER TABLE 目标必须在白名单内」
// 来做正向断言 —— 比逐条列出禁止的表更能拦住将来新加的表名。
func TestMigration253DoesNotAlterUpstreamTables(t *testing.T) {
	code := stripSQLComments(readIQVerdictMigration(t))

	allowed := map[string]struct{}{
		"iq_benchmark_cases":     {},
		"iq_benchmark_artifacts": {},
		"iq_benchmark_runs":      {},
	}

	lines := strings.Split(code, "\n")
	seenAlter := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)
		if strings.HasPrefix(upper, "ALTER TABLE ") {
			seenAlter = true
			rest := strings.TrimSpace(trimmed[len("ALTER TABLE "):])
			fields := strings.Fields(rest)
			require.NotEmpty(t, fields, "ALTER TABLE 后缺表名: %q", trimmed)
			table := strings.ToLower(fields[0])
			_, ok := allowed[table]
			require.True(t, ok, "迁移 253 不得 ALTER 白名单外的表 %q", table)
		}
	}
	require.True(t, seenAlter, "迁移 253 应当包含 ALTER TABLE 语句")
}

// TestMigration253MatchesGoVerdictConstants 断言 CHECK 枚举与 Go 常量逐字一致。
//
// 两份清单分居 SQL 与 Go，任何一侧改动都会让这条失败。
// 没有这条断言，改枚举时最容易只改一边，然后在运行期被库拒绝。
func TestMigration253MatchesGoVerdictConstants(t *testing.T) {
	code := stripSQLComments(readIQVerdictMigration(t))

	require.Contains(t, code, "'ok', 'degraded', 'generated', 'unknown'",
		"artifacts.verdict 的枚举必须与 Go 侧 Verdict* 常量一致")
	require.Contains(t, code, "'normal', 'degraded', 'unknown', 'generated'",
		"runs.verdict 的枚举必须与 Go 侧 RunVerdict* 常量一致")
}

// TestMigration253ErrorKindsMatchGoConstants 是 12 类归一化的双向一致性断言。
//
// 逐条检查 Go 侧每个 ErrorKind 都出现在 SQL 的 CHECK 里。
// 反向（SQL 多出一个）由 migration 的固定枚举本身保证 ——
// 这条测试的价值在于挡住「Go 加了第 13 类但忘了改迁移」。
func TestMigration253ErrorKindsMatchGoConstants(t *testing.T) {
	code := stripSQLComments(readIQVerdictMigration(t))

	for _, kind := range iqbenchmark.ErrorKinds {
		require.Contains(t, code, "'"+kind+"'",
			"error_kind 枚举 %q 必须在迁移 253 的 CHECK 里", kind)
	}
	require.Len(t, iqbenchmark.ErrorKinds, 12)
}

// TestMigration253AddsExpectedColumn 断言题目表新增参考答案列。
func TestMigration253AddsExpectedColumn(t *testing.T) {
	code := stripSQLComments(readIQVerdictMigration(t))

	require.Contains(t, code, "ADD COLUMN IF NOT EXISTS expected TEXT",
		"cases.expected 必须可空（NULL = 本题不可判分）")
	// 刻意不给默认值：空串与 NULL 在业务上含义不同，
	// 加 DEFAULT '' 会让两者合并，丢掉「不可判分」这个信息。
	require.NotContains(t, code, "expected TEXT DEFAULT",
		"expected 不得设默认值")
}

// TestMigration253RequiresExpectedForCustomLogicCases 断言库层也拦自定义题缺答案。
func TestMigration253RequiresExpectedForCustomLogicCases(t *testing.T) {
	code := stripSQLComments(readIQVerdictMigration(t))

	require.Contains(t, code, "iq_benchmark_cases_custom_logic_expected_check")
	require.Contains(t, code, "builtin = TRUE", "内置题必须豁免")
	require.Contains(t, code, "kind = 'image'", "图片题必须豁免")
	// NOT VALID：对新增/更新生效，但不回头校验存量行，
	// 避免升级因预料不到的历史数据失败。
	require.Contains(t, code, "NOT VALID")
}

// TestMigration253ConstrainsLogicCounts 断言分子分母的合法性约束。
//
// logic_passed <= logic_total 这条能挡住「通过率超过 100%」这类脏数据。
func TestMigration253ConstrainsLogicCounts(t *testing.T) {
	code := stripSQLComments(readIQVerdictMigration(t))

	require.Contains(t, code, "logic_passed <= logic_total")
	require.Contains(t, code, "logic_total >= 0")
	require.Contains(t, code, "logic_passed >= 0")
	// 计数列必须 NOT NULL DEFAULT 0：既有 run 行没有这两列时
	// 不能变成 NULL，否则前端要处处判空。
	require.Contains(t, code, "ADD COLUMN IF NOT EXISTS logic_total INTEGER NOT NULL DEFAULT 0")
	require.Contains(t, code, "ADD COLUMN IF NOT EXISTS logic_passed INTEGER NOT NULL DEFAULT 0")
}

// TestMigration253IsIdempotent 断言所有 DDL 都可重复执行。
//
// 迁移可能在半途失败的库上重跑，因此每条 ADD COLUMN 都要 IF NOT EXISTS、
// 每条 ADD CONSTRAINT 前都要先 DROP CONSTRAINT IF EXISTS。
func TestMigration253IsIdempotent(t *testing.T) {
	code := stripSQLComments(readIQVerdictMigration(t))

	// 每个 ADD COLUMN 必须带 IF NOT EXISTS。
	for _, line := range strings.Split(code, "\n") {
		if !strings.Contains(strings.ToUpper(line), "ADD COLUMN") {
			continue
		}
		require.Contains(t, line, "IF NOT EXISTS", "ADD COLUMN 必须可重入: %q", strings.TrimSpace(line))
	}

	// 每个 ADD CONSTRAINT 之前必须有对应的 DROP CONSTRAINT IF EXISTS。
	added := constraintNames(code, "ADD CONSTRAINT")
	dropped := constraintNames(code, "DROP CONSTRAINT IF EXISTS")
	require.NotEmpty(t, added)
	for _, name := range added {
		require.Contains(t, dropped, name, "约束 %q 加之前必须先 DROP IF EXISTS", name)
	}
}

// TestMigration253DoesNotBackfill 断言迁移不写任何 UPDATE。
//
// 功能尚未上线，库里没有存量行；一旦出现 UPDATE 就说明有人在没数据的前提下
// 编造了回填逻辑，那是把「假判定」当判定卖。
func TestMigration253DoesNotBackfill(t *testing.T) {
	code := stripSQLComments(readIQVerdictMigration(t))
	upper := strings.ToUpper(code)

	require.NotContains(t, upper, "UPDATE IQ_BENCHMARK_",
		"迁移 253 不得包含回填 UPDATE（当前无历史数据）")
	require.NotContains(t, upper, "WITH RECURSIVE")
}

// TestMigration254CreatesIndexes 断言两张实测有效的索引。
func TestMigration254CreatesIndexes(t *testing.T) {
	code := stripSQLComments(readIQVerdictIndexMigration(t))

	// 通过率统计：verdict 前导 + 时间（实测 Index Only Scan）。
	require.Contains(t, code, "idx_iq_artifact_verdict_time")
	require.Contains(t, code, "(verdict, created_at DESC)")
	// run 级查询：部分索引，实测被选中。
	require.Contains(t, code, "idx_iq_run_degraded")
	require.Contains(t, code, "WHERE verdict = 'degraded' AND deleted_at IS NULL")
	// 都必须可重入，且只覆盖未删除行（与既有 feed 索引口径一致）。
	require.Contains(t, code, "WHERE deleted_at IS NULL")
	require.Contains(t, code, "CREATE INDEX IF NOT EXISTS")
}

// TestMigration254DoesNotCreateRedundantFeedIndex 断言没有重建被否决的覆盖索引。
//
// 曾在 PG 18 上用 20 万行实测：idx_iq_artifact_feed_verdict
// (created_at DESC, id DESC, verdict) 被规划器完全忽略 ——
// 矩阵要取十几列，两种情况都得回表，而它体积是原 feed 索引的 2.4 倍。
// 这条测试防止后来者"顺手再加一个覆盖索引"。
func TestMigration254DoesNotCreateRedundantFeedIndex(t *testing.T) {
	code := stripSQLComments(readIQVerdictIndexMigration(t))

	require.NotContains(t, code, "idx_iq_artifact_feed_verdict",
		"矩阵的时间线索引已由既有 idx_iq_artifact_feed 覆盖，不得重复建")
	require.NotContains(t, code, "(created_at DESC, id DESC, verdict)")
}

// constraintNames 从 SQL 里抽取指定前缀语句后的第一个标识符。
func constraintNames(code, prefix string) []string {
	names := make([]string, 0, 8)
	for _, line := range strings.Split(code, "\n") {
		idx := strings.Index(strings.ToUpper(line), strings.ToUpper(prefix))
		if idx < 0 {
			continue
		}
		rest := strings.TrimSpace(line[idx+len(prefix):])
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		names = append(names, strings.ToLower(strings.TrimRight(fields[0], ";")))
	}
	return names
}
