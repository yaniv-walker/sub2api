package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/model"
)

var ErrAlertBusy = errors.New("balance alert is being checked; please retry shortly")

// Only plugin-owned data is created. No global Ent schema migration is run.
const BalanceAlertSchema = `CREATE TABLE IF NOT EXISTS upstream_monitor_balance_alerts (
 upstream_id BIGINT PRIMARY KEY REFERENCES upstream_monitor_upstreams(id) ON DELETE CASCADE,
 enabled BOOLEAN NOT NULL DEFAULT FALSE,
 config JSONB NOT NULL,
 state JSONB NOT NULL DEFAULT '{}',
 next_check_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 lease_owner TEXT NOT NULL DEFAULT '',
 lease_until TIMESTAMPTZ NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

type BalanceAlertRepository struct{ client *ent.Client }

func NewBalanceAlertRepository(client *ent.Client) *BalanceAlertRepository {
	return &BalanceAlertRepository{client: client}
}

func (r *BalanceAlertRepository) Get(ctx context.Context, id int64) (*model.BalanceAlertRule, error) {
	rows, err := r.client.QueryContext(ctx, `SELECT config,state FROM upstream_monitor_balance_alerts WHERE upstream_id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rule := &model.BalanceAlertRule{UpstreamID: id, BalanceAlertConfig: model.DefaultBalanceAlertConfig()}
	if !rows.Next() {
		return rule, rows.Err()
	}
	var config, state []byte
	if err = rows.Scan(&config, &state); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(config, &rule.BalanceAlertConfig); err != nil {
		return nil, err
	}
	err = json.Unmarshal(state, &rule.State)
	return rule, err
}

func (r *BalanceAlertRepository) Save(ctx context.Context, id int64, c model.BalanceAlertConfig) error {
	if err := c.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(c)
	if err != nil {
		return err
	}
	result, err := r.client.ExecContext(ctx, `INSERT INTO upstream_monitor_balance_alerts(upstream_id,enabled,config)
 VALUES($1,$2,$3::jsonb) ON CONFLICT(upstream_id) DO UPDATE SET
 enabled=EXCLUDED.enabled, config=EXCLUDED.config,
 state=CASE WHEN upstream_monitor_balance_alerts.config->>'threshold' IS DISTINCT FROM EXCLUDED.config->>'threshold'
 OR upstream_monitor_balance_alerts.config->>'repeat_interval_seconds' IS DISTINCT FROM EXCLUDED.config->>'repeat_interval_seconds'
 OR upstream_monitor_balance_alerts.enabled IS DISTINCT FROM EXCLUDED.enabled THEN '{}'::jsonb ELSE upstream_monitor_balance_alerts.state END,
 next_check_at=NOW(),updated_at=NOW()
 WHERE upstream_monitor_balance_alerts.lease_until IS NULL OR upstream_monitor_balance_alerts.lease_until<NOW()`, id, c.Enabled, string(payload))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAlertBusy
	}
	return nil
}

func (r *BalanceAlertRepository) DueIDs(ctx context.Context) ([]int64, error) {
	rows, err := r.client.QueryContext(ctx, `SELECT a.upstream_id FROM upstream_monitor_balance_alerts a
 JOIN upstream_monitor_upstreams u ON u.id=a.upstream_id
 WHERE a.enabled AND u.enabled AND a.next_check_at<=NOW()
 AND (a.lease_until IS NULL OR a.lease_until<NOW()) ORDER BY a.next_check_at LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// The lease outlives the checker's 6 minute budget plus bounded SMTP IO.
func (r *BalanceAlertRepository) Claim(ctx context.Context, id int64, owner string) (*model.BalanceAlertRule, error) {
	rows, err := r.client.QueryContext(ctx, `UPDATE upstream_monitor_balance_alerts SET lease_owner=$2,lease_until=NOW()+INTERVAL '15 minutes'
 WHERE upstream_id=$1 AND enabled AND next_check_at<=NOW() AND (lease_until IS NULL OR lease_until<NOW())
 RETURNING config,state`, id, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	rule := &model.BalanceAlertRule{UpstreamID: id}
	var config, state []byte
	if err = rows.Scan(&config, &state); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(config, &rule.BalanceAlertConfig); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(state, &rule.State); err != nil {
		return nil, err
	}
	return rule, nil
}

func (r *BalanceAlertRepository) StoreState(ctx context.Context, id int64, owner string, state model.BalanceAlertState, release bool) error {
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	result, err := r.client.ExecContext(ctx, `UPDATE upstream_monitor_balance_alerts SET state=$3::jsonb,
 next_check_at=CASE WHEN $4 THEN NOW()+INTERVAL '5 minutes' ELSE next_check_at END,
 lease_until=CASE WHEN $4 THEN NULL ELSE lease_until END,
 lease_owner=CASE WHEN $4 THEN '' ELSE lease_owner END,updated_at=NOW()
 WHERE upstream_id=$1 AND lease_owner=$2 AND lease_until>NOW()
 AND ($4 OR EXISTS(SELECT 1 FROM upstream_monitor_upstreams WHERE id=$1 AND enabled))`, id, owner, string(payload), release)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("balance alert lease expired")
	}
	return nil
}
