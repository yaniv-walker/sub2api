package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UpstreamErrorRecord 定义上游账号请求错误记录实体。
type UpstreamErrorRecord struct {
	ent.Schema
}

// Annotations 指定插件错误记录表名。
func (UpstreamErrorRecord) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "upstream_error_records"}}
}

// Fields 定义错误来源、错误内容和发生时间字段。
func (UpstreamErrorRecord) Fields() []ent.Field {
	return []ent.Field{
		field.String("upstream_type").MaxLen(50),
		field.Int64("account_id"),
		field.String("error_type").MaxLen(100),
		field.String("error_message").Optional().Nillable().SchemaType(map[string]string{dialect.Postgres: "text"}),
		field.Int("http_status").Optional().Nillable(),
		field.Time("occurred_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

// Indexes 为账号时间范围、错误类型和上游类型查询建立索引。
func (UpstreamErrorRecord) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("upstream_type", "account_id", "occurred_at"),
		index.Fields("upstream_type", "error_type", "occurred_at"),
	}
}
