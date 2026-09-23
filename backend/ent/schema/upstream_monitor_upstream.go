package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// UpstreamMonitorUpstream stores plugin-owned settings for one upstream
// origin. Host accounts remain untouched and are associated by normalized URL.
type UpstreamMonitorUpstream struct {
	ent.Schema
}

func (UpstreamMonitorUpstream) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "upstream_monitor_upstreams"}}
}

func (UpstreamMonitorUpstream) Fields() []ent.Field {
	return []ent.Field{
		field.String("base_url").MaxLen(500).NotEmpty().Unique(),
		field.String("name").MaxLen(200).Default(""),
		field.Enum("upstream_type").Values("sub2api", "nexapi"),
		field.String("access_token").Optional().Nillable().Sensitive(),
		field.String("personal_access_token").Optional().Nillable().Sensitive(),
		field.String("passkey").Optional().Nillable().Sensitive(),
		field.Float("quota_divider").Default(431778),
		field.Bool("enabled").Default(true),
		field.Time("created_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).Default(time.Now).Immutable(),
		field.Time("updated_at").SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).Default(time.Now).UpdateDefault(time.Now),
	}
}
