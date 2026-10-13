package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type checkinRepository struct{ db *sql.DB }

var _ service.CheckinRepository = (*checkinRepository)(nil)

func NewCheckinRepository(db *sql.DB) service.CheckinRepository { return &checkinRepository{db: db} }

type checkinQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// Serializes feature mutations, including rule replacement and reservation settlement.
// User row locks still serialize balance writes with the existing billing system.
func (r *checkinRepository) begin(ctx context.Context) (*sql.Tx, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(24320261013)"); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}
func checkinDate(now time.Time, c service.CheckinConfig) string {
	loc, _ := time.LoadLocation(c.Timezone)
	if loc == nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return now.In(loc).Format("2006-01-02")
}
func checkinRule(ctx context.Context, q checkinQuery, now time.Time, latest bool) (*service.CheckinConfig, error) {
	query := "SELECT version,config,effective_at,enabled FROM checkin_rules"
	args := []any{}
	if !latest {
		query += " WHERE effective_at <= $1"
		args = append(args, now)
	}
	query += " ORDER BY version DESC LIMIT 1"
	var c service.CheckinConfig
	var raw []byte
	var version int64
	var at time.Time
	var enabled bool
	err := q.QueryRowContext(ctx, query, args...).Scan(&version, &raw, &at, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		c = service.DefaultCheckinConfig()
		return &c, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &c); err != nil {
		return nil, err
	}
	c.Version = version
	c.EffectiveAt = at
	c.Enabled = enabled
	// A disable command takes effect immediately even if a scheduled revision is pending.
	if !latest {
		var latestEnabled bool
		if err = q.QueryRowContext(ctx, "SELECT enabled FROM checkin_rules ORDER BY version DESC LIMIT 1").Scan(&latestEnabled); err != nil {
			return nil, err
		}
		if !latestEnabled {
			c.Enabled = false
		}
	}
	return &c, nil
}
func (r *checkinRepository) GetConfig(ctx context.Context, now time.Time, latest bool) (*service.CheckinConfig, error) {
	return checkinRule(ctx, r.db, now, latest)
}
func (r *checkinRepository) SaveConfig(ctx context.Context, c service.CheckinConfig, expected, actor int64, now time.Time) (*service.CheckinConfig, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	current, err := checkinRule(ctx, tx, now, true)
	if err != nil {
		return nil, err
	}
	if current.Version != expected {
		return nil, service.ErrCheckinVersion
	}
	if !c.Enabled {
		c.EffectiveAt = now
	} else if current.Version > 0 {
		loc, _ := time.LoadLocation(c.Timezone)
		if loc == nil {
			return nil, fmt.Errorf("invalid timezone")
		}
		t := now.In(loc)
		next := time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
		if c.EffectiveAt.Before(next) {
			c.EffectiveAt = next
		}
	} else if c.EffectiveAt.Before(now) {
		c.EffectiveAt = now
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	err = tx.QueryRowContext(ctx, "INSERT INTO checkin_rules(config,enabled,effective_at,actor_id,created_at) VALUES($1,$2,$3,$4,$5) RETURNING version", raw, c.Enabled, c.EffectiveAt, actor, now).Scan(&c.Version)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &c, nil
}

const checkinRecordColumns = "id,user_id,business_date::text,reward_type,reward_amount::text,balance_after::text,rule_version,claimed_at,bonus"

type checkinScanner interface{ Scan(...any) error }

func scanCheckinRecord(row checkinScanner) (*service.CheckinRecord, error) {
	var v service.CheckinRecord
	var raw []byte
	err := row.Scan(&v.ID, &v.UserID, &v.BusinessDate, &v.RewardType, &v.RewardAmount, &v.BalanceAfter, &v.RuleVersion, &v.ClaimedAt, &raw)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, &v.Bonus)
	return &v, err
}
func checkinDaily(ctx context.Context, q checkinQuery, user int64, date string) (*service.CheckinRecord, error) {
	v, err := scanCheckinRecord(q.QueryRowContext(ctx, "SELECT "+checkinRecordColumns+" FROM checkin_records WHERE user_id=$1 AND business_date=$2::date AND reward_type='daily'", user, date))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// completed_at proves actual fulfillment. Pending refunds conservatively suspend
// the entire contribution; settled partial refunds revoke only their paid fraction.
const checkinPaidSQL = `SELECT COALESCE(SUM(amount),0)::text FROM checkin_paid_contributions WHERE user_id=$1`

func checkinState(ctx context.Context, q checkinQuery, user int64, now time.Time, c service.CheckinConfig, lock bool) (*service.CheckinInputs, error) {
	v := &service.CheckinInputs{UserID: user}
	query := "SELECT balance::text,status='active' FROM users WHERE id=$1 AND deleted_at IS NULL"
	if lock {
		query += " FOR UPDATE"
	}
	if err := q.QueryRowContext(ctx, query, user).Scan(&v.Balance, &v.Active); err != nil {
		return nil, err
	}
	if err := q.QueryRowContext(ctx, checkinPaidSQL, user).Scan(&v.PaidCreditUSD); err != nil {
		return nil, err
	}
	date := checkinDate(now, c)
	if err := q.QueryRowContext(ctx, "SELECT COALESCE((SELECT requests FROM checkin_request_days WHERE user_id=$1 AND business_date=$2::date),0)", user, date).Scan(&v.TodayRequests); err != nil {
		return nil, err
	}
	var err error
	v.Record, err = checkinDaily(ctx, q, user, date)
	if err != nil {
		return nil, err
	}
	v.BudgetRemaining = c.DailyBudgetUSD
	err = q.QueryRowContext(ctx, "SELECT (limit_usd-spent_usd-reserved_usd)::text FROM checkin_budgets WHERE business_date=$1::date", date).Scan(&v.BudgetRemaining)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	v.Cycle, err = checkinLoadCycle(ctx, q, user, date)
	return v, err
}
func (r *checkinRepository) GetUserState(ctx context.Context, user int64, now time.Time, c service.CheckinConfig) (*service.CheckinInputs, error) {
	return checkinState(ctx, r.db, user, now, c, false)
}
func checkinCredit(ctx context.Context, tx *sql.Tx, user int64, date, kind, amount string, version int64, now time.Time, draw service.CheckinDraw) (*service.CheckinRecord, error) {
	var balance string
	if err := tx.QueryRowContext(ctx, "UPDATE users SET balance=balance+$2::numeric,updated_at=$3 WHERE id=$1 RETURNING balance::text", user, amount, now).Scan(&balance); err != nil {
		return nil, err
	}
	bonus, err := json.Marshal(draw.Bonus)
	if err != nil {
		return nil, err
	}
	weights, err := json.Marshal(draw.Weights)
	if err != nil {
		return nil, err
	}
	v, err := scanCheckinRecord(tx.QueryRowContext(ctx, "INSERT INTO checkin_records(user_id,business_date,reward_type,reward_amount,balance_after,rule_version,claimed_at,bonus,weights) VALUES($1,$2::date,$3,$4::numeric,$5::numeric,$6,$7,$8,$9) RETURNING "+checkinRecordColumns, user, date, kind, amount, balance, version, now, bonus, weights))
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO checkin_reward_ledger(record_id,user_id,amount,created_at) VALUES($1,$2,$3::numeric,$4)", v.ID, user, amount, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO checkin_invalidation_outbox(user_id,created_at) VALUES($1,$2)", user, now); err != nil {
		return nil, err
	}
	// Reuse the native durable worker: synchronous auth invalidation deliberately
	// swallows downstream errors, so it cannot acknowledge this delivery promise.
	if _, err = tx.ExecContext(ctx, `INSERT INTO auth_cache_invalidation_outbox(cache_key)
 SELECT encode(sha256(convert_to(k.key,'UTF8')),'hex') FROM api_keys k WHERE user_id=$1 AND deleted_at IS NULL AND key<>''`, user); err != nil {
		return nil, err
	}
	return v, nil
}
func (r *checkinRepository) Claim(ctx context.Context, user int64, now time.Time, draw service.CheckinDrawFunc) (*service.CheckinClaimResult, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	c, err := checkinRule(ctx, tx, now, false)
	if err != nil {
		return nil, err
	}
	date := checkinDate(now, *c)
	// Retrying a completed claim never draws again, including after disabling.
	existing, err := checkinDaily(ctx, tx, user, date)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return &service.CheckinClaimResult{Record: existing}, nil
	}
	if !c.Enabled || c.Version == 0 {
		return nil, service.ErrCheckinDisabled
	}
	input, err := checkinState(ctx, tx, user, now, *c, true)
	if err != nil {
		return nil, err
	}
	paid, err := service.CheckinUSDUnits(input.PaidCreditUSD)
	if err != nil {
		return nil, err
	}
	minimum, err := service.CheckinUSDUnits(c.MinimumPaidCreditUSD)
	if err != nil {
		return nil, err
	}
	if !input.Active || paid < minimum {
		return nil, service.ErrCheckinIneligible
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO checkin_budgets(business_date,limit_usd) VALUES($1::date,$2::numeric) ON CONFLICT DO NOTHING", date, c.DailyBudgetUSD); err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT (limit_usd-spent_usd-reserved_usd)::text FROM checkin_budgets WHERE business_date=$1::date FOR UPDATE", date).Scan(&input.BudgetRemaining); err != nil {
		return nil, err
	}
	remaining, err := service.CheckinUSDUnits(input.BudgetRemaining)
	if err != nil {
		return nil, err
	}
	// Check worst-case payout before invoking randomness, preventing budget rerolls.
	max := int64(0)
	for _, option := range c.Rewards {
		n, e := service.CheckinUSDUnits(option.Amount)
		if e != nil {
			return nil, e
		}
		if n > max {
			max = n
		}
	}
	if remaining < max {
		return nil, service.ErrCheckinBudget
	}
	result, err := draw(*c, *input)
	if err != nil {
		return nil, err
	}
	amount, err := service.CheckinUSDUnits(result.Amount)
	if err != nil || amount <= 0 || amount > max {
		return nil, fmt.Errorf("invalid checkin draw")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE checkin_budgets SET spent_usd=spent_usd+$2::numeric WHERE business_date=$1::date", date, result.Amount); err != nil {
		return nil, err
	}
	record, err := checkinCredit(ctx, tx, user, date, "daily", result.Amount, c.Version, now, result)
	if err != nil {
		return nil, err
	}
	if c.CycleEnabled {
		if err = checkinStartCycle(ctx, tx, user, date, now, *c); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &service.CheckinClaimResult{Created: true, Record: record}, nil
}
func (r *checkinRepository) History(ctx context.Context, user, cursor int64, limit int) (*service.CheckinHistory, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, "SELECT "+checkinRecordColumns+" FROM checkin_records WHERE ($1::bigint=0 OR user_id=$1) AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT $3", user, cursor, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	h := &service.CheckinHistory{Items: []service.CheckinRecord{}}
	for rows.Next() {
		v, e := scanCheckinRecord(rows)
		if e != nil {
			return nil, e
		}
		h.Items = append(h.Items, *v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(h.Items) > limit {
		h.Items = h.Items[:limit]
		h.NextCursor = h.Items[limit-1].ID
	}
	return h, nil
}
func (r *checkinRepository) PendingInvalidations(ctx context.Context, limit int) ([]service.CheckinInvalidation, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, "SELECT id,user_id FROM checkin_invalidation_outbox ORDER BY id LIMIT $1", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	v := []service.CheckinInvalidation{}
	for rows.Next() {
		var i service.CheckinInvalidation
		if err = rows.Scan(&i.ID, &i.UserID); err != nil {
			return nil, err
		}
		v = append(v, i)
	}
	return v, rows.Err()
}
func (r *checkinRepository) AckInvalidation(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, "DELETE FROM checkin_invalidation_outbox WHERE id=$1", id)
	return err
}
