package schema

import (
	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// IQBenchmarkCase 是「模型智商检测」的题目。
//
// 题目一旦被某次执行使用，其内容会被冻结进 iq_benchmark_artifacts.case_snapshot，
// 因此题目本身可自由编辑而不影响历史产出的可回溯性。
//
// builtin=true 的内置题（鹈鹕骑自行车）由 service 层保护：不可删除，可禁用可编辑。
type IQBenchmarkCase struct {
	ent.Schema
}

func (IQBenchmarkCase) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "iq_benchmark_cases"},
	}
}

func (IQBenchmarkCase) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (IQBenchmarkCase) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			NotEmpty().
			MaxLen(100),
		// kind 决定调用哪个网关端点：
		//   image          -> /v1/images/generations
		//   text / code    -> /v1/chat/completions
		field.String("kind").
			MaxLen(16).
			Comment("image -> images/generations; text/code -> chat/completions"),
		field.String("prompt").
			SchemaType(map[string]string{"postgres": "text"}).
			NotEmpty(),
		field.String("system_prompt").
			SchemaType(map[string]string{"postgres": "text"}).
			Optional().
			Nillable(),
		// params 为下发请求参数的原始 JSON（temperature / max_tokens / size / n 等）。
		field.JSON("params", map[string]any{}).
			Default(map[string]any{}),
		field.String("expect_kind").
			MaxLen(16).
			Comment("image_url / svg / markdown / text"),
		field.String("scoring").
			MaxLen(16).
			Default("manual").
			Comment("manual / rule / manual_rule / llm_judge"),
		// 首版不实现 LLM 裁判：字段保留但执行侧恒不启用。
		field.Bool("llm_judge_enabled").
			Default(false),
		// builtin=true 的内置题不可删除（service 层守卫）。
		field.Bool("builtin").
			Default(false),
		field.Bool("enabled").
			Default(true),
		field.Int("sort_order").
			Default(0),
		// 是否允许用户自测：默认 false，需管理员显式放开。
		field.Bool("allow_user_test").
			Default(false),
	}
}

func (IQBenchmarkCase) Indexes() []ent.Index {
	return []ent.Index{
		// 显式命名（与 migration 249 保持一致），避免 Ent 生成的默认索引名
		// 与 SQL 迁移里的名字不一致，导致两条建表链路产出不同 schema。
		index.Fields("enabled", "sort_order", "id").
			StorageKey("idx_iq_case_listing"),
	}
}
