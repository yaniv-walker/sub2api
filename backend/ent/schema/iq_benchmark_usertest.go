package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// IQBenchmarkUserTest 是「用户用自己本站 API Key 一键自测」的业务状态。
//
// 职责边界（重要）：
//   - 本表只存**请求的业务状态**：配额、并发、去重窗口、审核流转锚点；
//   - 作品内容本身仍写 iq_benchmark_artifacts（作品展的全部读路径零改动）；
//   - 两张表用 artifact_id 单向关联，artifacts **不**反向感知本表 ——
//     这样即使本表被整表清空，作品展也完全不受影响。
//
// 为什么不复用 iq_benchmark_runs：自测没有「任务的一次执行」这个语义，
// 伪造 run 会污染执行统计，也会被清理逻辑误删。
type IQBenchmarkUserTest struct {
	ent.Schema
}

func (IQBenchmarkUserTest) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "iq_benchmark_user_tests"},
	}
}

func (IQBenchmarkUserTest) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (IQBenchmarkUserTest) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id"),
		// 用的是用户自己的哪一个 Key —— 排障时要知道是哪把 Key 出的问题。
		field.Int64("api_key_id"),
		field.Int64("case_id"),
		field.Int64("group_id"),
		field.String("model").
			MaxLen(200),
		// 产出写入后回填；失败路径可能始终为 NULL。
		field.Int64("artifact_id").
			Optional().
			Nillable(),
		field.String("status").
			MaxLen(16).
			Default("pending").
			Comment("pending / running / success / failed / blocked / rejected"),
		field.String("error_code").
			MaxLen(64).
			Optional().
			Nillable(),
		field.String("error_summary").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Optional().
			Nillable(),
		// 发起前按题目规模预估，用于日成本预算预检。
		field.Float("estimated_cost").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,10)"}).
			Default(0),
		// 执行后从 usage 回采的真实成本。
		field.Float("actual_cost").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,10)"}).
			Default(0),
		// user + case + model + 分钟级时间片 的哈希，用于 10 分钟去重窗口。
		// 存哈希而不是明文拼接：避免把 user_id/case_id 以可读形式散落到索引里。
		field.String("dedupe_key").
			MaxLen(128),
	}
}

func (IQBenchmarkUserTest) Indexes() []ent.Index {
	// 显式命名，与 migration 251 保持一致，避免 Ent 与 SQL 两条建表链路分叉。
	return []ent.Index{
		// 用户维度的配额与历史。
		index.Fields("user_id", "created_at").
			StorageKey("idx_iq_usertest_user"),
		// 去重窗口查询。
		index.Fields("dedupe_key", "created_at").
			StorageKey("idx_iq_usertest_dedupe"),
		// 清理与并发统计只关心未完成的请求。
		index.Fields("status").
			StorageKey("idx_iq_usertest_pending").
			Annotations(entsql.IndexWhere("status IN ('pending', 'running')")),
		// 按产出反查自测记录（后台排障）。
		index.Fields("artifact_id").
			StorageKey("idx_iq_usertest_artifact").
			Unique().
			Annotations(entsql.IndexWhere("artifact_id IS NOT NULL")),
	}
}
