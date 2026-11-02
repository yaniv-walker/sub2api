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

// IQBenchmarkArtifact 是单次执行的产出（一个模型对一道题的一次作品）。
//
// case_snapshot 冻结了当时的题目内容：题目被编辑或删除后，历史产出仍可按
// 当时题面回溯，这是「同题多模型横向对比」正确性的前提。
//
// 失败 / 超时 / 被内容审核拦截的产出同样入库（status 区分），作品展以占位卡片展示。
// unsafe_flags 记录入库前静态扫描命中的危险构造，公开侧永不下发该字段。
type IQBenchmarkArtifact struct {
	ent.Schema
}

func (IQBenchmarkArtifact) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "iq_benchmark_artifacts"},
	}
}

func (IQBenchmarkArtifact) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (IQBenchmarkArtifact) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("run_id"),
		field.Int64("task_id"),
		field.Int64("case_id"),
		// 题目快照（name/kind/prompt/system_prompt/params/expect_kind/scoring）。
		field.JSON("case_snapshot", map[string]any{}).
			Default(map[string]any{}),
		field.Int64("group_id"),
		field.String("model").
			MaxLen(200),
		field.JSON("request_params", map[string]any{}).
			Default(map[string]any{}),
		field.String("render_kind").
			MaxLen(16).
			Comment("image_url / svg / markdown / text"),
		field.String("image_url").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Optional().
			Nillable(),
		field.String("content").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Optional().
			Nillable(),
		field.Bool("content_truncated").
			Default(false),
		// 截断前原文的 SHA256（hex），用于校验与去重。
		field.String("content_sha256").
			MaxLen(64).
			Optional().
			Nillable(),
		// 命中的危险构造标记（script / event_handler / foreign_object / ...）。
		field.JSON("unsafe_flags", []string{}).
			Default([]string{}),
		field.String("raw_response_ref").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Optional().
			Nillable(),
		field.Int("duration_ms").
			Default(0).
			NonNegative(),
		field.Int("input_tokens").
			Default(0).
			NonNegative(),
		field.Int("output_tokens").
			Default(0).
			NonNegative(),
		field.Float("cost").
			SchemaType(map[string]string{dialect.Postgres: "numeric(20,10)"}).
			Default(0),
		field.String("status").
			MaxLen(16).
			Comment("success / failed / timeout / blocked"),
		// error_code 为内部码，公开侧只下发脱敏后的 error_summary。
		field.String("error_code").
			MaxLen(64).
			Optional().
			Nillable(),
		field.String("error_summary").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Optional().
			Nillable(),
		field.Float("score").
			SchemaType(map[string]string{dialect.Postgres: "numeric(8,2)"}).
			Optional().
			Nillable(),
		field.Int64("scored_by").
			Optional().
			Nillable(),
		field.Time("scored_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		// 对象存储键：清理时据此删除 blob。
		field.String("blob_key").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Optional().
			Nillable(),
		field.Time("deleted_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (IQBenchmarkArtifact) Indexes() []ent.Index {
	// 全部索引都显式命名：Ent 默认按字段拼接生成名字，多字段组合极易撞名
	// （例如 run_id 与 task_id 组合），显式命名后与 migration 249 完全一致，
	// 也让「Ent 建表」与「SQL 迁移」两条链路产出同一个 schema。
	return []ent.Index{
		index.Fields("run_id").
			StorageKey("idx_iq_artifact_run"),
		// 作品展主查询：未删除 + 时间倒序。
		index.Fields("created_at", "id").
			StorageKey("idx_iq_artifact_feed").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		// 筛选组合：分组 / 模型 / 题目 / 时间。
		index.Fields("group_id", "model", "case_id", "created_at").
			StorageKey("idx_iq_artifact_filters").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		index.Fields("status", "created_at").
			StorageKey("idx_iq_artifact_status").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
		// 容量守卫与清理扫描：按 task 保留最新 N 条。
		index.Fields("task_id", "created_at").
			StorageKey("idx_iq_artifact_task_live").
			Annotations(entsql.IndexWhere("deleted_at IS NULL")),
	}
}
