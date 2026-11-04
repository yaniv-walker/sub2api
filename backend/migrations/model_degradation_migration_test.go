//go:build unit

package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 本文件盯住 255/256 这两个迁移的三类「改了就出事」的形态：
//
//  1. 写入 api_key —— 用户自带上游凭据是这套功能的前提，而凭据一旦落库，
//     泄漏面就从「一次请求的内存」变成「整张表 + 备份 + 从库」。
//  2. 漏掉状态/判定枚举的 CHECK —— 应用层写错值时，没有约束就会静默存进去，
//     之后统计口径全错且无法回溯修正。
//  3. 种子题与题库约束不自洽 —— render 题若带了 expected，或 logic 题没有，
//     迁移会直接被 CHECK 拒绝，上线即失败。

func TestMigration255NeverPersistsCredentials(t *testing.T) {
	content, err := FS.ReadFile("255_model_degradation.sql")
	require.NoError(t, err)

	// 只看语句，不看注释 —— 注释里提到 api_key 是在解释「为什么不存它」，
	// 直接对全文断言 NotContains 会把这句说明本身判成违规。
	sql := stripSQLComments(string(content))

	// 表里根本不应该存在能存 Key 的列。
	require.NotContains(t, sql, "api_key")
	require.NotContains(t, sql, "APIKey")
	require.NotContains(t, sql, "apiKey")
	// base_url 只留脱敏展示列，不留原始值列。
	require.Contains(t, sql, "base_url_display")
	require.NotContains(t, sql, "base_url VARCHAR")
	require.NotContains(t, sql, "raw_base_url")
}

func TestMigration255DefinesAllFourTables(t *testing.T) {
	content, err := FS.ReadFile("255_model_degradation.sql")
	require.NoError(t, err)

	sql := string(content)
	for _, table := range []string{
		"model_degradation_tasks",
		"model_degradation_runs",
		"model_degradation_items",
		"model_degradation_visibility",
	} {
		require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS "+table)
	}
}

func TestMigration255RestrictsRunStatusAndVerdictEnums(t *testing.T) {
	content, err := FS.ReadFile("255_model_degradation.sql")
	require.NoError(t, err)

	sql := string(content)

	// 运行状态三态：running 之外的都已结束。
	require.Contains(t, sql, "CHECK (status IN ('running', 'completed', 'cancelled'))")
	// 判定四态。generated 是纯生成模式，必须能存。
	require.Contains(t, sql, "CHECK (verdict IN ('normal', 'degraded', 'unknown', 'generated'))")
	// 生成题判定独立，且允许「未跑」的空串。
	require.Contains(t, sql, "CHECK (render_verdict IN ('', 'ok', 'degraded', 'unknown', 'generated'))")
	// 协议只有三种。
	require.Contains(t, sql, "CHECK (protocol IN ('chat_completions', 'responses', 'messages'))")
}

func TestMigration255EnforcesConsistentTerminalState(t *testing.T) {
	content, err := FS.ReadFile("255_model_degradation.sql")
	require.NoError(t, err)

	sql := string(content)

	// 结束态必须有完成时间，运行中必须没有 —— 否则「耗时」统计会把
	// 未完成的运行算进去。
	require.Contains(t, sql, "status = 'running' AND completed_at IS NULL")
	require.Contains(t, sql, "status <> 'running' AND completed_at IS NOT NULL")
	// 通过数不可能超过总数。
	require.Contains(t, sql, "CHECK (logic_passed <= logic_total)")
}

func TestMigration255KeepsHistoryReadableAfterOwnerDeletion(t *testing.T) {
	content, err := FS.ReadFile("255_model_degradation.sql")
	require.NoError(t, err)

	sql := string(content)

	// 归属用快照列，不回查 users，也不加外键（加外键会让删用户被历史阻断）。
	require.Contains(t, sql, "owner_email_snapshot")
	require.NotContains(t, sql, "REFERENCES users")
	require.NotContains(t, sql, "REFERENCES groups")
}

func TestMigration255DropsItemsWithRun(t *testing.T) {
	content, err := FS.ReadFile("255_model_degradation.sql")
	require.NoError(t, err)

	sql := string(content)
	// 删一次运行必须连带删掉它的题目结果，不能留孤儿行。
	require.Contains(t, sql, "REFERENCES model_degradation_runs(id) ON DELETE CASCADE")
}

func TestMigration256SeedSatisfiesTaskTypeExpectedConstraint(t *testing.T) {
	content, err := FS.ReadFile("256_model_degradation_seed.sql")
	require.NoError(t, err)

	sql := string(content)

	// 与 255 的 CHECK 同一条规则：logic 必须有 expected，render 必须为空。
	// 这里用「逐行断言」而不是解析 SQL：种子只有两行，写死更不可能漂移。
	require.Contains(t, sql, "'logic'")
	require.Contains(t, sql, "'candy-shape-flavor'")
	require.Contains(t, sql, "'21'")

	require.Contains(t, sql, "'render'")
	require.Contains(t, sql, "'pelican-bicycle'")

	// render 题的两个占位符必须都在，否则随机场景/校验码机制失效，
	// 用户就能拿旧图充数。
	require.Contains(t, sql, "{scene}")
	require.Contains(t, sql, "{code}")
}

func TestMigration256SeedIsIdempotent(t *testing.T) {
	content, err := FS.ReadFile("256_model_degradation_seed.sql")
	require.NoError(t, err)

	sql := string(content)
	// 重放安全：冲突时更新内容而不是报错。
	require.Contains(t, sql, "ON CONFLICT (task_type, key) DO UPDATE")
	// 不允许出现无冲突处理的裸 INSERT ... VALUES。
	require.NotContains(t, sql, "INSERT INTO model_degradation_tasks (task_type, key, title, prompt_template, expected, sort_order, enabled)\nVALUES\n    (\n        'logic',\n        'candy-shape-flavor',\n        '糖果摸取：保证不同形状的苹果味和桃子味',\n        '不使用任何外部工具回答以下问题：在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）\n\n| 形状 | 苹果味 | 桃子味 | 西瓜味 |\n| 圆形 | 7 | 9 | 8 |\n| 五角星形 | 7 | 6 | 4 |',\n        '21',\n        10,\n        TRUE\n    );")
}

func TestMigration255And256ContainNoTransactionStatements(t *testing.T) {
	// 迁移运行器把每个文件包在自己的事务里，文件内出现 BEGIN/COMMIT 会导致
	// 嵌套事务报错。这是本仓库最容易踩的迁移陷阱，固化成测试。
	for _, name := range []string{
		"255_model_degradation.sql",
		"256_model_degradation_seed.sql",
	} {
		content, err := FS.ReadFile(name)
		require.NoError(t, err)

		sql := string(content)
		for _, stmt := range []string{"BEGIN;", "COMMIT;", "ROLLBACK;"} {
			require.False(
				t,
				containsBareStatement(sql, stmt),
				"%s must not contain %s (runner wraps each file in its own transaction)",
				name, stmt,
			)
		}
	}
}

// containsBareStatement 判断语句是否作为独立语句出现（行首、忽略前导空白），
// 避免把注释里的 "BEGIN;" 误判成真语句。
func containsBareStatement(sql, stmt string) bool {
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		if strings.HasPrefix(strings.ToUpper(trimmed), stmt) {
			return true
		}
	}
	return false
}
