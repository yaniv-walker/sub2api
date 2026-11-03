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

// IQBenchmarkRun 记录检测任务的一次执行。
//
// group_id / model 是快照值：任务后续被修改也不影响历史 run 的可回溯性。
// internal_user_id / internal_api_key_id 是执行所用的内部账号与专用 Key，
// 也是统计过滤的唯一依据（检测流量按这两个 ID 与该 user_agent 精确识别）。
type IQBenchmarkRun struct {
	ent.Schema
}

func (IQBenchmarkRun) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "iq_benchmark_runs"},
	}
}

func (IQBenchmarkRun) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (IQBenchmarkRun) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("task_id"),
		// 快照：任务后续被改也能回溯本次执行的组合与模型。
		field.Int64("group_id"),
		field.String("model").
			MaxLen(200),
		field.String("trigger").
			MaxLen(16).
			Comment("schedule / manual / retry"),
		field.Time("started_at").
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("finished_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Int("success_count").
			Default(0).
			NonNegative(),
		field.Int("failed_count").
			Default(0).
			NonNegative(),
		field.Int("timeout_count").
			Default(0).
			NonNegative(),
		field.Int("blocked_count").
			Default(0).
			NonNegative(),
		field.Int64("total_duration_ms").
			Default(0).
			NonNegative(),
		field.Float("total_cost").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,10)"}).
			Default(0),
		field.Int64("total_input_tokens").
			Default(0).
			NonNegative(),
		field.Int64("total_output_tokens").
			Default(0).
			NonNegative(),
		// 执行所用内部账号与专用 Key：审计与统计过滤的唯一依据。
		field.Int64("internal_user_id"),
		field.Int64("internal_api_key_id"),
		field.String("status").
			MaxLen(16).
			Default("running").
			Comment("running / success / partial / failed"),
		// verdict 是本次运行的汇总判定（判定口径），与 status（调用口径）并行。
		//
		// generated 且 logic_total = 0 表示「纯生成模式」：
		// 这一轮只出图，没有可判分的题目。
		field.String("verdict").
			MaxLen(16).
			Optional().
			Nillable().
			Comment("normal / degraded / unknown / generated"),
		// logic_total / logic_passed 是判定口径的分子分母。
		//
		// 与 success_count 等的区别：后者数调用成败，这里数判定结果。
		// 保留两套计数是刻意的 —— 通过率要能被追溯到"哪些题真的判了"，
		// 而调用成败仍要用于运维排障，二者都不可替代。
		field.Int("logic_total").
			Default(0).
			NonNegative(),
		field.Int("logic_passed").
			Default(0).
			NonNegative(),
		// 成本回采标记：usage_logs 异步落库时下一轮补偿回采。
		field.Time("cost_collected_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		// 清理软删。
		field.Time("deleted_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (IQBenchmarkRun) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("task_id", "created_at").
			StorageKey("idx_iq_run_task"),
		// 成本回填扫描：未回填且未删除的 run。
		index.Fields("created_at").
			StorageKey("idx_iq_run_pending_cost").
			Annotations(entsql.IndexWhere("cost_collected_at IS NULL AND deleted_at IS NULL")),
	}
}
