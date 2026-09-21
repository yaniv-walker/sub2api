package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UpstreamBalanceSnapshot 定义上游账号余额历史快照实体。
type UpstreamBalanceSnapshot struct {
	ent.Schema
}

// Annotations 指定插件表名，避免依赖 Ent 默认命名推断。
func (UpstreamBalanceSnapshot) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "upstream_balance_snapshots"}}
}

// Fields 定义上游类型、账号、余额及采样时间字段。
func (UpstreamBalanceSnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.String("upstream_type").MaxLen(50),
		field.Int64("account_id"),
		field.Float("balance").SchemaType(map[string]string{dialect.Postgres: "numeric(20,6)"}),
		field.String("currency").MaxLen(10).Default("CNY"),
		field.Time("snapshot_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).Default(time.Now),
	}
}

// Indexes 为账号时间范围查询和上游类型查询建立索引。
func (UpstreamBalanceSnapshot) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("upstream_type", "account_id", "snapshot_at"),
		index.Fields("upstream_type", "snapshot_at"),
	}
}
