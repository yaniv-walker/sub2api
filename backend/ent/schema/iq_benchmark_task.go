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

// IQBenchmarkTask 是按「分组 × 模型」定期执行的检测任务。
//
// 同一 (group_id, model) 最多只能有一个 enabled=true 的任务，该约束由
// 数据库部分唯一索引 uq_iq_task_group_model_enabled 强制（见 migration 249），
// 不只依赖应用层检查。
//
// 多实例去重依赖 lease_owner / lease_expires_at：
// 调度器以 FOR UPDATE SKIP LOCKED 领取并写入租约，执行期间周期性续期；
// 续期影响行数为 0 即表示租约已被他人接管，执行方必须立即终止本轮。
type IQBenchmarkTask struct {
	ent.Schema
}

func (IQBenchmarkTask) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "iq_benchmark_tasks"},
	}
}

func (IQBenchmarkTask) Mixin() []ent.Mixin {
	return []ent.Mixin{
		mixins.TimeMixin{},
	}
}

func (IQBenchmarkTask) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			NotEmpty().
			MaxLen(100),
		// 故意不加 edge/FK：加了会让上游「删除分组」被本表阻断，改变既有行为语义。
		// 分组被删除后任务在下一轮执行时记失败并自动禁用。
		field.Int64("group_id"),
		field.String("model").
			NotEmpty().
			MaxLen(200),
		// 题目 ID 有序数组；运行时按顺序解析，已失效的 ID 被跳过。
		field.JSON("case_ids", []int64{}).
			Default([]int64{}),
		// 间隔 15 分钟 ~ 7 天；抖动由 service 层另校验 interval*60 - jitter >= 900。
		field.Int("interval_minutes").
			Range(15, 10080),
		field.Int("jitter_seconds").
			Default(0).
			Range(0, 604800).
			Comment("每次调度 ± [0, jitter] 秒偏移；service 层保证 interval*60 - jitter >= 900"),
		field.Bool("enabled").
			Default(false),
		field.String("status").
			MaxLen(24).
			Default("idle").
			Comment("idle / running / quota_exhausted / budget_exhausted / account_disabled / paused_by_admin"),
		field.String("status_reason").
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Optional().
			Nillable(),
		field.Time("next_run_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("last_run_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		// 多实例去重：持有租约的实例标识与租约到期时间。
		field.String("lease_owner").
			MaxLen(128).
			Optional().
			Nillable(),
		field.Time("lease_expires_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (IQBenchmarkTask) Indexes() []ent.Index {
	return []ent.Index{
		// 同组合仅一个启用中任务（部分唯一索引，需与 migration 249 保持一致）。
		index.Fields("group_id", "model").
			Unique().
			StorageKey("uq_iq_task_group_model_enabled").
			Annotations(entsql.IndexWhere("enabled = true")),
		index.Fields("next_run_at").
			StorageKey("idx_iq_task_due").
			Annotations(entsql.IndexWhere("enabled = true")),
		// 非唯一的 (group_id, model) 查找索引也必须显式命名：
		// Ent 默认按字段拼接生成名字，会与上面的部分唯一索引撞名（都叫
		// iqbenchmarktask_group_id_model），导致建表时报「index already exists」。
		index.Fields("group_id", "model").
			StorageKey("idx_iq_task_group_model"),
	}
}
