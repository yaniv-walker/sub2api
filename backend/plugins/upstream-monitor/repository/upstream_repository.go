package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/upstreammonitorupstream"
)

// UpstreamRepository stores plugin-owned configuration keyed by normalized
// upstream origin. It does not write to host account records.
type UpstreamRepository struct {
	client *ent.Client
}

func NewUpstreamRepository(client *ent.Client) *UpstreamRepository {
	return &UpstreamRepository{client: client}
}

func (r *UpstreamRepository) List(ctx context.Context) ([]*ent.UpstreamMonitorUpstream, error) {
	return r.client.UpstreamMonitorUpstream.Query().
		Order(ent.Asc(upstreammonitorupstream.FieldBaseURL)).
		All(ctx)
}

func (r *UpstreamRepository) GetByBaseURL(ctx context.Context, baseURL string) (*ent.UpstreamMonitorUpstream, error) {
	return r.client.UpstreamMonitorUpstream.Query().
		Where(upstreammonitorupstream.BaseURL(baseURL)).
		Only(ctx)
}

func (r *UpstreamRepository) Upsert(ctx context.Context, baseURL, name string, upstreamType upstreammonitorupstream.UpstreamType, enabled bool) (*ent.UpstreamMonitorUpstream, error) {
	id, err := r.client.UpstreamMonitorUpstream.Create().
		SetBaseURL(baseURL).
		SetName(name).
		SetUpstreamType(upstreamType).
		SetEnabled(enabled).
		OnConflictColumns(upstreammonitorupstream.FieldBaseURL).
		UpdateNewValues().
		ID(ctx)
	if err != nil {
		return nil, err
	}
	return r.client.UpstreamMonitorUpstream.Get(ctx, id)
}
