package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const checkinCycleColumns = "id,state,start_date::text,end_date::text,rule_version,reward_amount::text,requests_threshold,settle_at"

func scanCheckinCycle(row checkinScanner) (*service.CheckinCycle, error) {
	v := &service.CheckinCycle{Days: []service.CheckinCycleDay{}}
	err := row.Scan(&v.ID, &v.State, &v.StartDate, &v.EndDate, &v.RuleVersion, &v.RewardAmount, &v.RequestsThreshold, &v.SettleAt)
	return v, err
}
func checkinCycleDays(ctx context.Context, q checkinQuery, user int64, today string, c *service.CheckinCycle) error {
	rows, err := q.QueryContext(ctx, `SELECT d::date::text,COALESCE(r.requests,0),EXISTS(SELECT 1 FROM checkin_records cr WHERE cr.user_id=$1 AND cr.business_date=d::date AND cr.reward_type='daily')
 FROM generate_series($2::date,$3::date,interval '1 day') d LEFT JOIN checkin_request_days r ON r.user_id=$1 AND r.business_date=d::date ORDER BY d`, user, c.StartDate, c.EndDate)
	if err != nil {
		return err
	}
	defer rows.Close()
	c.QualifiedDays = 0
	c.Days = []service.CheckinCycleDay{}
	for rows.Next() {
		var d service.CheckinCycleDay
		if err = rows.Scan(&d.Date, &d.Requests, &d.CheckedIn); err != nil {
			return err
		}
		d.Future = d.Date > today
		d.Met = d.CheckedIn && d.Requests >= c.RequestsThreshold
		d.Finalized = d.Date < today && (d.Met || !d.CheckedIn)
		if d.Met {
			c.QualifiedDays++
		}
		c.Days = append(c.Days, d)
	}
	return rows.Err()
}
func checkinLoadCycle(ctx context.Context, q checkinQuery, user int64, today string) (*service.CheckinCycle, error) {
	c, err := scanCheckinCycle(q.QueryRowContext(ctx, "SELECT "+checkinCycleColumns+" FROM checkin_cycles WHERE user_id=$1 ORDER BY CASE WHEN state IN ('active','pending_usage','blocked_user') THEN 0 ELSE 1 END,id DESC LIMIT 1", user))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = checkinCycleDays(ctx, q, user, today, c); err != nil {
		return nil, err
	}
	return c, nil
}
func checkinStartCycle(ctx context.Context, tx *sql.Tx, user int64, date string, now time.Time, c service.CheckinConfig) error {
	var exists bool
	// Only promised/open or completed windows overlap; broken/no-capacity may restart tomorrow.
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM checkin_cycles WHERE user_id=$1 AND (state IN ('active','pending_usage','blocked_user') OR start_date=$2::date OR (state='completed' AND end_date >= $2::date)))", user, date).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	amount, err := service.CheckinUSDUnits(c.CycleRewardUSD)
	if err != nil {
		return err
	}
	if amount <= 0 {
		return nil
	}
	var reserved bool
	err = tx.QueryRowContext(ctx, `UPDATE checkin_budgets SET reserved_usd=reserved_usd+$2::numeric WHERE business_date=$1::date AND limit_usd-spent_usd-reserved_usd >= $2::numeric RETURNING true`, date, c.CycleRewardUSD).Scan(&reserved)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	state := "active"
	if !reserved {
		state = "capacity_exhausted"
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return err
	}
	start, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return err
	}
	end := start.AddDate(0, 0, c.CycleDays-1)
	settle := end.AddDate(0, 0, 1).Add(time.Hour)
	_, err = tx.ExecContext(ctx, `INSERT INTO checkin_cycles(user_id,state,start_date,end_date,budget_date,rule_version,reward_amount,requests_threshold,settle_at,reserved,updated_at) VALUES($1,$2,$3::date,$4::date,$3::date,$5,$6::numeric,$7,$8,$9,$10)`, user, state, date, end.Format("2006-01-02"), c.Version, c.CycleRewardUSD, c.RequestsThreshold, settle, reserved, now)
	return err
}

type checkinCyclePending struct {
	cycle      *service.CheckinCycle
	user       int64
	budgetDate string
}

func (r *checkinRepository) ProcessCycles(ctx context.Context, now time.Time) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT "+checkinCycleColumns+",user_id,budget_date::text FROM checkin_cycles WHERE state IN ('active','pending_usage','blocked_user') AND start_date < ($1 AT TIME ZONE 'Asia/Shanghai')::date ORDER BY updated_at,id LIMIT 100 FOR UPDATE", now)
	if err != nil {
		return err
	}
	var cycles []checkinCyclePending
	for rows.Next() {
		c := &service.CheckinCycle{}
		var u int64
		var b string
		if err = rows.Scan(&c.ID, &c.State, &c.StartDate, &c.EndDate, &c.RuleVersion, &c.RewardAmount, &c.RequestsThreshold, &c.SettleAt, &u, &b); err != nil {
			rows.Close()
			return err
		}
		cycles = append(cycles, checkinCyclePending{c, u, b})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	today := now.In(loc).Format("2006-01-02")
	for _, p := range cycles {
		c := p.cycle
		// Rotate the bounded scan even when the user or usage needs more time.
		if _, err = tx.ExecContext(ctx, "UPDATE checkin_cycles SET updated_at=$2 WHERE id=$1", c.ID, now); err != nil {
			return err
		}
		if err = checkinCycleDays(ctx, tx, p.user, today, c); err != nil {
			return err
		}
		var balance string
		var active bool
		if err = tx.QueryRowContext(ctx, "SELECT balance::text,status='active' AND deleted_at IS NULL FROM users WHERE id=$1 FOR UPDATE", p.user).Scan(&balance, &active); err != nil {
			return err
		}
		if !active {
			if _, err = tx.ExecContext(ctx, "UPDATE checkin_cycles SET state='blocked_user' WHERE id=$1", c.ID); err != nil {
				return err
			}
			continue
		}
		if c.State == "blocked_user" {
			if _, err = tx.ExecContext(ctx, "UPDATE checkin_cycles SET state='active' WHERE id=$1", c.ID); err != nil {
				return err
			}
		}
		broken := false
		pending := false
		for _, d := range c.Days {
			if d.Date >= today {
				continue
			}
			if !d.CheckedIn {
				broken = true
			}
			if d.Requests < c.RequestsThreshold {
				pending = true
			}
		}
		if broken {
			if _, err = tx.ExecContext(ctx, "UPDATE checkin_budgets SET reserved_usd=reserved_usd-$2::numeric WHERE business_date=$1::date", p.budgetDate, c.RewardAmount); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE checkin_cycles SET state='broken',reserved=false WHERE id=$1", c.ID); err != nil {
				return err
			}
			continue
		}
		if now.Before(c.SettleAt) {
			continue
		}
		// No writer-drain watermark exists. Late usage may still qualify a day, so a
		// shortfall remains pending, with its original reservation, until it qualifies.
		if pending || c.QualifiedDays < len(c.Days) {
			if _, err = tx.ExecContext(ctx, "UPDATE checkin_cycles SET state='pending_usage' WHERE id=$1", c.ID); err != nil {
				return err
			}
			continue
		}
		record, e := checkinCredit(ctx, tx, p.user, c.EndDate, "cycle", c.RewardAmount, c.RuleVersion, now, service.CheckinDraw{Amount: c.RewardAmount, Weights: []int64{}})
		if e != nil {
			return e
		}
		if _, err = tx.ExecContext(ctx, "UPDATE checkin_budgets SET reserved_usd=reserved_usd-$2::numeric,spent_usd=spent_usd+$2::numeric WHERE business_date=$1::date", p.budgetDate, c.RewardAmount); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE checkin_cycles SET state='completed',reserved=false,settled_record_id=$2 WHERE id=$1", c.ID, record.ID); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return r.pruneCheckinRequestDedup(ctx, now)
}

func (r *checkinRepository) pruneCheckinRequestDedup(ctx context.Context, now time.Time) error {
	// Separate short retention transaction; no global activity lock or unbounded deletes.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `WITH expired AS (
 SELECT d.user_id,d.business_date FROM checkin_request_days d
 WHERE d.finalized_at IS NULL AND d.business_date < ($1 AT TIME ZONE 'Asia/Shanghai')::date-35
 AND NOT EXISTS(SELECT 1 FROM checkin_cycles c WHERE c.user_id=d.user_id AND c.state IN ('active','pending_usage','blocked_user') AND d.business_date BETWEEN c.start_date AND c.end_date)
 ORDER BY d.business_date,d.user_id LIMIT 100 FOR UPDATE OF d SKIP LOCKED
 ) UPDATE checkin_request_days d SET finalized_at=$1 FROM expired e WHERE d.user_id=e.user_id AND d.business_date=e.business_date`, now)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `WITH expired AS (
 SELECT x.user_id,x.request_key FROM checkin_request_dedup x JOIN checkin_request_days d ON d.user_id=x.user_id AND d.business_date=x.business_date
 WHERE d.finalized_at IS NOT NULL ORDER BY x.business_date,x.user_id LIMIT 5000
 ) DELETE FROM checkin_request_dedup x USING expired e WHERE x.user_id=e.user_id AND x.request_key=e.request_key`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ResolveCycle allows a reasoned retry or cancellation of unresolved usage only.
// It cannot manufacture a qualifying day or force a balance credit.
func (r *checkinRepository) ResolveCycle(ctx context.Context, id, actor int64, action, reason string, now time.Time) error {
	if actor <= 0 || strings.TrimSpace(reason) == "" || len(reason) > 2000 || (action != "retry" && action != "cancel") {
		return fmt.Errorf("invalid cycle resolution")
	}
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	c := &service.CheckinCycle{}
	var user int64
	var budget string
	err = tx.QueryRowContext(ctx, "SELECT "+checkinCycleColumns+",user_id,budget_date::text FROM checkin_cycles WHERE id=$1 FOR UPDATE", id).Scan(&c.ID, &c.State, &c.StartDate, &c.EndDate, &c.RuleVersion, &c.RewardAmount, &c.RequestsThreshold, &c.SettleAt, &user, &budget)
	if err != nil {
		return err
	}
	before := c.State
	if action == "cancel" {
		if c.State != "pending_usage" || now.Before(c.SettleAt) {
			return infraerrors.Conflict("CHECKIN_CYCLE_RESOLVED", "only unresolved closed usage cycles can be cancelled")
		}
		// Materialize missing day rows, then lock all counters against concurrent
		// usage inserts before deciding that the closed cycle is still unqualified.
		if _, err = tx.ExecContext(ctx, `INSERT INTO checkin_request_days(user_id,business_date,requests)
 SELECT $1,d::date,0 FROM generate_series($2::date,$3::date,interval '1 day') d ON CONFLICT DO NOTHING`, user, c.StartDate, c.EndDate); err != nil {
			return err
		}
		locked, e := tx.QueryContext(ctx, "SELECT business_date FROM checkin_request_days WHERE user_id=$1 AND business_date BETWEEN $2::date AND $3::date ORDER BY business_date FOR UPDATE", user, c.StartDate, c.EndDate)
		if e != nil {
			return e
		}
		for locked.Next() {
		}
		e = locked.Err()
		locked.Close()
		if e != nil {
			return e
		}
		loc, _ := time.LoadLocation("Asia/Shanghai")
		if err = checkinCycleDays(ctx, tx, user, now.In(loc).Format("2006-01-02"), c); err != nil {
			return err
		}
		if c.QualifiedDays == len(c.Days) {
			return infraerrors.Conflict("CHECKIN_CYCLE_QUALIFIED", "qualified cycle cannot be cancelled")
		}
		if _, err = tx.ExecContext(ctx, "UPDATE checkin_budgets SET reserved_usd=reserved_usd-$2::numeric WHERE business_date=$1::date", budget, c.RewardAmount); err != nil {
			return err
		}
		c.State = "broken"
		if _, err = tx.ExecContext(ctx, "UPDATE checkin_cycles SET state='broken',reserved=false,updated_at=$2 WHERE id=$1", id, now); err != nil {
			return err
		}
	} else {
		if c.State != "pending_usage" && c.State != "blocked_user" && c.State != "active" {
			return infraerrors.Conflict("CHECKIN_CYCLE_RESOLVED", "cycle is already resolved")
		}
		if _, err = tx.ExecContext(ctx, "UPDATE checkin_cycles SET updated_at='1970-01-01' WHERE id=$1", id); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO checkin_cycle_audit(cycle_id,actor_id,action,reason,before_state,after_state,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)", id, actor, action, strings.TrimSpace(reason), before, c.State, now); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if action == "retry" {
		return r.ProcessCycles(ctx, now)
	}
	return nil
}

func (r *checkinRepository) ListCycles(ctx context.Context, user, cursor int64, limit int) (*service.CheckinCycleHistory, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx, "SELECT "+checkinCycleColumns+",user_id FROM checkin_cycles WHERE ($1::bigint=0 OR user_id=$1) AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT $3", user, cursor, limit+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	history := &service.CheckinCycleHistory{Items: []service.CheckinAdminCycle{}}
	for rows.Next() {
		var c service.CheckinAdminCycle
		if err = rows.Scan(&c.ID, &c.State, &c.StartDate, &c.EndDate, &c.RuleVersion, &c.RewardAmount, &c.RequestsThreshold, &c.SettleAt, &c.UserID); err != nil {
			return nil, err
		}
		history.Items = append(history.Items, c)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(history.Items) > limit {
		history.Items = history.Items[:limit]
		history.NextCursor = history.Items[limit-1].ID
	}
	loc, _ := time.LoadLocation("Asia/Shanghai")
	today := time.Now().In(loc).Format("2006-01-02")
	for i := range history.Items {
		c := &history.Items[i]
		if err = checkinCycleDays(ctx, r.db, c.UserID, today, &c.CheckinCycle); err != nil {
			return nil, err
		}
	}
	return history, nil
}
