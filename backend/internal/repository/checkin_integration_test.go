package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Opt-in database must be a disposable checkin_test database, never production.
// Every run owns a random schema and drops only that schema on cleanup.
func checkinTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("CHECKIN_TEST_DSN")
	if dsn == "" {
		t.Skip("CHECKIN_TEST_DSN not set (disposable PostgreSQL)")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "/checkin_test", u.Path, "only disposable checkin_test database allowed")
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	require.NoError(t, admin.Ping())
	schema := fmt.Sprintf("checkin_it_%d", time.Now().UnixNano())
	_, err = admin.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	db.SetMaxOpenConns(24)
	t.Cleanup(func() {
		db.Close()
		_, dropErr := admin.Exec("DROP SCHEMA " + schema + " CASCADE")
		if dropErr != nil {
			t.Error(dropErr)
		}
		admin.Close()
	})
	_, err = db.Exec(`CREATE TABLE users(id BIGINT PRIMARY KEY,balance NUMERIC(20,8) NOT NULL DEFAULT 0,total_recharged NUMERIC(20,8) NOT NULL DEFAULT 0,status TEXT DEFAULT 'active',deleted_at TIMESTAMPTZ,updated_at TIMESTAMPTZ);
 CREATE TABLE payment_orders(id BIGINT PRIMARY KEY,user_id BIGINT,amount NUMERIC(20,2),bonus_amount NUMERIC(20,2),pay_amount NUMERIC(20,2),refund_amount NUMERIC(20,2),order_type TEXT,status TEXT,paid_at TIMESTAMPTZ,completed_at TIMESTAMPTZ,recharge_code TEXT NOT NULL DEFAULT '');
 CREATE TABLE redeem_codes(id BIGSERIAL PRIMARY KEY,code TEXT UNIQUE,type TEXT,value NUMERIC(20,8),status TEXT,used_by BIGINT,used_at TIMESTAMPTZ);
 CREATE TABLE usage_logs(id BIGSERIAL PRIMARY KEY,user_id BIGINT,request_id TEXT,input_tokens INT DEFAULT 0,output_tokens INT DEFAULT 0,cache_creation_tokens INT DEFAULT 0,cache_read_tokens INT DEFAULT 0,image_input_tokens INT DEFAULT 0,image_output_tokens INT DEFAULT 0,image_count INT DEFAULT 0,actual_cost NUMERIC(20,10) DEFAULT 0,created_at TIMESTAMPTZ);
 CREATE TABLE api_keys(user_id BIGINT,key TEXT,deleted_at TIMESTAMPTZ);
 CREATE TABLE auth_cache_invalidation_outbox(id BIGSERIAL PRIMARY KEY,cache_key VARCHAR(64) NOT NULL,created_at TIMESTAMPTZ DEFAULT NOW());`)
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("243_checkin_rewards.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err, "new migration must be safely repeatable")
	indexMigration, err := migrations.FS.ReadFile("244_checkin_recharge_code_lookup.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(indexMigration))
	require.NoError(t, err)
	_, err = db.Exec(string(indexMigration))
	require.NoError(t, err)
	return db
}
func seedCheckinUser(t *testing.T, db *sql.DB, id int64) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO users(id,balance,total_recharged) VALUES($1,-2,10);
 `, id)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO payment_orders(id,user_id,amount,bonus_amount,pay_amount,refund_amount,order_type,status,paid_at,completed_at) VALUES($1,$1,10,2,72,0,'balance','COMPLETED',NOW(),NOW())`, id)
	require.NoError(t, err)
}
func TestCheckinPostgresConcurrencyBudgetAndLateUsage(t *testing.T) {
	db := checkinTestDB(t)
	ctx := context.Background()
	r := NewCheckinRepository(db)
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, loc)
	c := service.DefaultCheckinConfig()
	c.Enabled = true
	c.CycleEnabled = true
	c.RequestsThreshold = 3
	c.DailyBudgetUSD = "2.00"
	c.Rewards = []service.CheckinRewardOption{{Amount: "0.25", Weight: 9999}, {Amount: "1.00", Weight: 1}}
	_, err := r.SaveConfig(ctx, c, 0, 1, now)
	require.NoError(t, err)
	for user := int64(1); user <= 3; user++ {
		seedCheckinUser(t, db, user)
	}
	_, err = db.Exec("INSERT INTO api_keys(user_id,key) VALUES(1,'isolated-test-key')")
	require.NoError(t, err)
	var calls atomic.Int64
	draw := func(service.CheckinConfig, service.CheckinInputs) (service.CheckinDraw, error) {
		calls.Add(1)
		return service.CheckinDraw{Amount: "0.25", Weights: []int64{9999, 1}}, nil
	}
	var wg sync.WaitGroup
	results := make(chan *service.CheckinClaimResult, 20)
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); v, e := r.Claim(ctx, 1, now, draw); results <- v; errs <- e }()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	created := 0
	for v := range results {
		if v.Created {
			created++
		}
	}
	require.Equal(t, 1, created)
	require.Equal(t, int64(1), calls.Load())
	state, err := r.GetUserState(ctx, 1, now, c)
	require.NoError(t, err)
	require.Equal(t, "-1.75000000", state.Balance)
	require.Equal(t, "8.00000000", state.PaidCreditUSD)
	require.Equal(t, "active", state.Cycle.State)
	_, err = r.Claim(ctx, 2, now, draw)
	require.NoError(t, err)
	_, err = r.Claim(ctx, 3, now, draw)
	require.ErrorIs(t, err, service.ErrCheckinBudget)
	require.Equal(t, int64(2), calls.Load(), "exhausted budget cannot reroll")
	var recharge string
	require.NoError(t, db.QueryRow("SELECT total_recharged::text FROM users WHERE id=1").Scan(&recharge))
	require.Equal(t, "10.00000000", recharge)
	for day := 0; day < 7; day++ {
		at := now.AddDate(0, 0, day)
		if day > 0 {
			_, err = r.Claim(ctx, 1, at, draw)
			require.NoError(t, err)
		}
		n := 3
		if day == 6 {
			n = 1
		}
		for req := 0; req < n; req++ {
			key := fmt.Sprintf("d%d-r%d", day, req)
			_, err = db.Exec("INSERT INTO usage_logs(user_id,request_id,input_tokens,created_at) VALUES(1,$1,1,$2),(1,$1,1,$2)", key, at)
			require.NoError(t, err)
		}
	}
	_, err = db.Exec("DELETE FROM usage_logs")
	require.NoError(t, err)
	settle := time.Date(2026, 10, 20, 1, 5, 0, 0, loc)
	require.NoError(t, r.ProcessCycles(ctx, settle))
	state, err = r.GetUserState(ctx, 1, settle, c)
	require.NoError(t, err)
	require.Equal(t, "pending_usage", state.Cycle.State)
	require.Equal(t, 6, state.Cycle.QualifiedDays)
	state2, err := r.GetUserState(ctx, 2, settle, c)
	require.NoError(t, err)
	require.Equal(t, "broken", state2.Cycle.State)
	for req := 1; req < 3; req++ {
		_, err = db.Exec("INSERT INTO usage_logs(user_id,request_id,input_tokens,created_at) VALUES(1,$1,1,$2)", fmt.Sprintf("d6-r%d", req), now.AddDate(0, 0, 6))
		require.NoError(t, err)
	}
	settleErrors := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); settleErrors <- r.ProcessCycles(ctx, settle) }()
	}
	wg.Wait()
	close(settleErrors)
	for e := range settleErrors {
		require.NoError(t, e)
	}
	require.NoError(t, r.ProcessCycles(ctx, settle))
	state, err = r.GetUserState(ctx, 1, settle, c)
	require.NoError(t, err)
	require.Equal(t, "completed", state.Cycle.State)
	require.Equal(t, "0.25000000", state.Balance)
	var ledger, outbox int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM checkin_reward_ledger WHERE user_id=1").Scan(&ledger))
	require.Equal(t, 8, ledger)
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM checkin_invalidation_outbox WHERE user_id=1").Scan(&outbox))
	require.Equal(t, 8, outbox)
	var authEvents int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM auth_cache_invalidation_outbox WHERE cache_key=encode(sha256(convert_to('isolated-test-key','UTF8')),'hex')").Scan(&authEvents))
	require.Equal(t, 8, authEvents)
	history, err := r.History(ctx, 1, 0, 3)
	require.NoError(t, err)
	require.Len(t, history.Items, 3)
	require.NotZero(t, history.NextCursor)
	next, err := r.History(ctx, 1, history.NextCursor, 100)
	require.NoError(t, err)
	require.Len(t, next.Items, 5)
	// Disabling still permits retries and never changes the original payout.
	latest, err := r.GetConfig(ctx, settle, true)
	require.NoError(t, err)
	latest.Enabled = false
	_, err = r.SaveConfig(ctx, *latest, latest.Version, 1, settle)
	require.NoError(t, err)
	retry, err := r.Claim(ctx, 1, now.AddDate(0, 0, 6), draw)
	require.NoError(t, err)
	require.False(t, retry.Created)
}
func TestCheckinPostgresManualAndRedeemCredits(t *testing.T) {
	db := checkinTestDB(t)
	ctx := context.Background()
	r := NewCheckinRepository(db)
	now := time.Now()
	c := service.DefaultCheckinConfig()
	c.Enabled = true
	c.CycleEnabled = false
	_, err := r.SaveConfig(ctx, c, 0, 1, now)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users(id,balance) VALUES(1,30),(2,10);
 INSERT INTO redeem_codes(code,type,value,status,used_by,used_at) VALUES
 ('manual','admin_balance',6,'used',1,NOW()),
 ('offline','balance',4,'used',1,NOW()),
 ('unused','balance',100,'unused',1,NULL),
 ('negative','balance',-10,'used',1,NOW()),
 ('subscription','subscription',100,'used',1,NOW()),
 ('other-user','balance',10,'used',2,NOW());`)
	require.NoError(t, err)
	state, err := r.GetUserState(ctx, 1, now, c)
	require.NoError(t, err)
	require.Equal(t, "10.00000000", state.PaidCreditUSD, "historical manual top-ups and used balance codes count")
	// Online payment already has a redeem record, including its bonus. Count
	// its paid contribution once and retain refund handling through the order.
	_, err = db.Exec(`INSERT INTO payment_orders(id,user_id,amount,bonus_amount,pay_amount,refund_amount,order_type,status,paid_at,completed_at,recharge_code)
 VALUES(1,1,12,2,72,0,'balance','COMPLETED',NOW(),NOW(),'online');
 INSERT INTO redeem_codes(code,type,value,status,used_by,used_at) VALUES('online','balance',12,'used',1,NOW());`)
	require.NoError(t, err)
	state, err = r.GetUserState(ctx, 1, now, c)
	require.NoError(t, err)
	require.Equal(t, "20.00000000", state.PaidCreditUSD, "online code must not duplicate payment or count its bonus")
	_, err = db.Exec(`UPDATE payment_orders SET status='REFUNDED',refund_amount=12 WHERE id=1;
 INSERT INTO redeem_codes(code,type,value,status,used_by,used_at) VALUES('new-offline','balance',3,'used',1,NOW());`)
	require.NoError(t, err)
	state, err = r.GetUserState(ctx, 1, now, c)
	require.NoError(t, err)
	require.Equal(t, "13.00000000", state.PaidCreditUSD, "new credits are visible immediately, refunded orders stay excluded")
	result, err := r.Claim(ctx, 1, now, func(service.CheckinConfig, service.CheckinInputs) (service.CheckinDraw, error) {
		return service.CheckinDraw{Amount: "0.01", Weights: []int64{8500, 700, 400, 200, 150, 50}}, nil
	})
	require.NoError(t, err, "manual and code credits must also satisfy claim eligibility")
	require.True(t, result.Created)
	state, err = r.GetUserState(ctx, 1, now, c)
	require.NoError(t, err)
	require.Equal(t, "13.00000000", state.PaidCreditUSD, "checkin rewards never increase eligible credit")
}

func TestCheckinPostgresPaidCreditRefundCurrencyAndEligibility(t *testing.T) {
	db := checkinTestDB(t)
	seedCheckinUser(t, db, 1)
	r := NewCheckinRepository(db)
	ctx := context.Background()
	c := service.DefaultCheckinConfig()
	now := time.Now()
	tests := []struct {
		status   string
		refund   string
		expected string
	}{{"COMPLETED", "0", "8.00000000"}, {"PARTIALLY_REFUNDED", "2.50", "6.00000000"}, {"REFUNDED", "10", "0.00000000"}, {"REFUNDING", "2.50", "0.00000000"}, {"REFUND_REQUESTED", "10", "0.00000000"}, {"REFUND_PENDING", "2.50", "0.00000000"}, {"REFUND_FAILED", "2.50", "0.00000000"}}
	for _, test := range tests {
		t.Run(test.status, func(t *testing.T) {
			_, err := db.Exec("UPDATE payment_orders SET status=$1,refund_amount=$2::numeric WHERE id=1", test.status, test.refund)
			require.NoError(t, err)
			v, err := r.GetUserState(ctx, 1, now, c)
			require.NoError(t, err)
			require.Equal(t, test.expected, v.PaidCreditUSD)
		})
	}
	_, err := db.Exec("UPDATE payment_orders SET status='COMPLETED',refund_amount=0,order_type='subscription' WHERE id=1")
	require.NoError(t, err)
	v, err := r.GetUserState(ctx, 1, now, c)
	require.NoError(t, err)
	require.Equal(t, "0.00000000", v.PaidCreditUSD)
	_, err = db.Exec("UPDATE payment_orders SET order_type='balance',completed_at=NULL WHERE id=1")
	require.NoError(t, err)
	v, err = r.GetUserState(ctx, 1, now, c)
	require.NoError(t, err)
	require.Equal(t, "0.00000000", v.PaidCreditUSD)
}

func TestCheckinPostgresUsefulRequestsIncludeFreeAndMediaAndIgnoreFailures(t *testing.T) {
	db := checkinTestDB(t)
	now := time.Now()
	c := service.DefaultCheckinConfig()
	c.Enabled = true
	_, err := NewCheckinRepository(db).SaveConfig(context.Background(), c, 0, 1, now.Add(-time.Hour))
	require.NoError(t, err)
	rows := []struct {
		key                         string
		tokens, images, imageTokens int
		cost                        string
	}{
		{"failed", 0, 0, 0, "0"}, {"free", 1, 0, 0, "0"}, {"image", 0, 1, 0, "0"},
		{"image_tokens", 0, 0, 10, "0"}, {"per_call", 0, 0, 0, "0.1"},
	}
	for _, row := range rows {
		_, err := db.Exec("INSERT INTO usage_logs(user_id,request_id,input_tokens,image_count,image_output_tokens,actual_cost,created_at) VALUES(7,$1,$2,$3,$4,$5::numeric,$6)", row.key, row.tokens, row.images, row.imageTokens, row.cost, now)
		require.NoError(t, err)
	}
	var count int64
	date := checkinDate(now, c)
	require.NoError(t, db.QueryRow("SELECT requests FROM checkin_request_days WHERE user_id=7 AND business_date=$1::date", date).Scan(&count))
	require.Equal(t, int64(4), count)
	// Retry after usage cleanup must still deduplicate the original request.
	_, err = db.Exec("DELETE FROM usage_logs")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO usage_logs(user_id,request_id,input_tokens,created_at) VALUES(7,'free',1,$1)", now)
	require.NoError(t, err)
	require.NoError(t, db.QueryRow("SELECT requests FROM checkin_request_days WHERE user_id=7 AND business_date=$1::date", date).Scan(&count))
	require.Equal(t, int64(4), count)
}

func TestCheckinPostgresCompleteMigrationChain(t *testing.T) {
	dsn := os.Getenv("CHECKIN_TEST_DSN")
	if dsn == "" {
		t.Skip("CHECKIN_TEST_DSN not set (disposable PostgreSQL)")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "/checkin_test", u.Path)
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer admin.Close()
	database := fmt.Sprintf("checkin_chain_%d", time.Now().UnixNano())
	_, err = admin.Exec("CREATE DATABASE " + database)
	require.NoError(t, err)
	defer admin.Exec("DROP DATABASE " + database)
	u.Path = "/" + database
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	require.NoError(t, ApplyMigrations(ctx, db), "fresh entire schema, including new checkin migration")
	require.NoError(t, ApplyMigrations(ctx, db), "runner checksum and repeat startup")
	var exists bool
	require.NoError(t, db.QueryRow("SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE filename='243_checkin_rewards.sql')").Scan(&exists))
	require.True(t, exists)
	var user int64
	require.NoError(t, db.QueryRow("INSERT INTO users(email,password_hash) VALUES('checkin-isolated@example.invalid','non-login-test-value') RETURNING id").Scan(&user))
	_, err = db.Exec("INSERT INTO api_keys(user_id,key,name) VALUES($1,'sk-isolated-checkin-verification','isolated')", user)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO payment_orders(user_id,amount,bonus_amount,pay_amount,order_type,status,paid_at,completed_at,expires_at) VALUES($1,10,2,72,'balance','COMPLETED',NOW(),NOW(),NOW()+interval '1 hour')", user)
	require.NoError(t, err)
	r := NewCheckinRepository(db)
	c := service.DefaultCheckinConfig()
	c.Enabled = true
	now := time.Now()
	_, err = r.SaveConfig(ctx, c, 0, user, now)
	require.NoError(t, err)
	_, err = r.Claim(ctx, user, now, func(service.CheckinConfig, service.CheckinInputs) (service.CheckinDraw, error) {
		return service.CheckinDraw{Amount: "0.15", Weights: []int64{8500, 700, 400, 200, 150, 50}}, nil
	})
	require.NoError(t, err)
	var authEvents int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM auth_cache_invalidation_outbox WHERE cache_key=encode(sha256(convert_to('sk-isolated-checkin-verification','UTF8')),'hex')").Scan(&authEvents))
	require.Equal(t, 1, authEvents)
}

func TestCheckinPostgresScheduledRulesAndImmediateDisable(t *testing.T) {
	db := checkinTestDB(t)
	r := NewCheckinRepository(db)
	ctx := context.Background()
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Date(2026, 10, 13, 23, 59, 0, 0, loc)
	c := service.DefaultCheckinConfig()
	c.Enabled = true
	first, err := r.SaveConfig(ctx, c, 0, 1, now)
	require.NoError(t, err)
	c.MinimumPaidCreditUSD = "10.00"
	future, err := r.SaveConfig(ctx, c, first.Version, 1, now)
	require.NoError(t, err)
	require.Equal(t, "2026-10-14T00:00:00+08:00", future.EffectiveAt.Format(time.RFC3339))
	active, err := r.GetConfig(ctx, now, false)
	require.NoError(t, err)
	require.Equal(t, first.Version, active.Version)
	active, err = r.GetConfig(ctx, now.Add(time.Minute), false)
	require.NoError(t, err)
	require.Equal(t, future.Version, active.Version)
	c.Enabled = false
	disabled, err := r.SaveConfig(ctx, c, future.Version, 1, now)
	require.NoError(t, err)
	c.Enabled = true
	enabled, err := r.SaveConfig(ctx, c, disabled.Version, 1, now)
	require.NoError(t, err)
	active, err = r.GetConfig(ctx, now, false)
	require.NoError(t, err)
	require.False(t, active.Enabled, "scheduled enable cannot undo immediate disable before midnight")
	active, err = r.GetConfig(ctx, now.Add(time.Minute), false)
	require.NoError(t, err)
	require.True(t, active.Enabled)
	require.Equal(t, enabled.Version, active.Version)
	_, err = r.SaveConfig(ctx, c, first.Version, 1, now)
	require.ErrorIs(t, err, service.ErrCheckinVersion)
}

func TestCheckinPostgresCapacityExhaustedStillPaysDaily(t *testing.T) {
	db := checkinTestDB(t)
	seedCheckinUser(t, db, 1)
	r := NewCheckinRepository(db)
	ctx := context.Background()
	now := time.Now()
	c := service.DefaultCheckinConfig()
	c.Enabled = true
	c.CycleEnabled = true
	c.DailyBudgetUSD = "2.00"
	c.CycleRewardUSD = "2.00"
	_, err := r.SaveConfig(ctx, c, 0, 1, now)
	require.NoError(t, err)
	claimed, err := r.Claim(ctx, 1, now, func(service.CheckinConfig, service.CheckinInputs) (service.CheckinDraw, error) {
		return service.CheckinDraw{Amount: "0.15", Weights: []int64{8500, 700, 400, 200, 150, 50}}, nil
	})
	require.NoError(t, err)
	require.True(t, claimed.Created)
	state, err := r.GetUserState(ctx, 1, now, c)
	require.NoError(t, err)
	require.Equal(t, "capacity_exhausted", state.Cycle.State)
	require.Equal(t, "-1.85000000", state.Balance)
	var reserved string
	require.NoError(t, db.QueryRow("SELECT reserved_usd::text FROM checkin_budgets").Scan(&reserved))
	require.Equal(t, "0.00000000", reserved)
}

func TestCheckinPostgresCycleResolutionBlockedUserAndReentry(t *testing.T) {
	db := checkinTestDB(t)
	ctx := context.Background()
	r := NewCheckinRepository(db)
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(loc).AddDate(0, 0, -8)
	c := service.DefaultCheckinConfig()
	c.Enabled = true
	c.CycleEnabled = true
	c.RequestsThreshold = 1
	_, err := r.SaveConfig(ctx, c, 0, 1, now)
	require.NoError(t, err)
	seedCheckinUser(t, db, 1)
	draw := func(service.CheckinConfig, service.CheckinInputs) (service.CheckinDraw, error) {
		return service.CheckinDraw{Amount: "0.15", Weights: []int64{8500, 700, 400, 200, 150, 50}}, nil
	}
	for day := 0; day < 7; day++ {
		at := now.AddDate(0, 0, day)
		_, err = r.Claim(ctx, 1, at, draw)
		require.NoError(t, err)
		if day < 6 {
			_, err = db.Exec("INSERT INTO usage_logs(user_id,request_id,input_tokens,created_at) VALUES(1,$1,1,$2)", fmt.Sprintf("r%d", day), at)
			require.NoError(t, err)
		}
	}
	settle := now.AddDate(0, 0, 8)
	require.NoError(t, r.ProcessCycles(ctx, settle))
	state, err := r.GetUserState(ctx, 1, settle, c)
	require.NoError(t, err)
	require.Equal(t, "pending_usage", state.Cycle.State)
	require.NoError(t, r.ResolveCycle(ctx, state.Cycle.ID, 99, "retry", "用量队列再次核对", settle))
	require.Error(t, r.ResolveCycle(ctx, state.Cycle.ID, 99, "cancel", "", settle))
	require.NoError(t, r.ResolveCycle(ctx, state.Cycle.ID, 99, "cancel", "已核对最后自然日确实不足，管理员终止未确认资格", settle))
	var reserved string
	require.NoError(t, db.QueryRow("SELECT reserved_usd::text FROM checkin_budgets WHERE business_date=$1::date", checkinDate(now, c)).Scan(&reserved))
	require.Equal(t, "0.00000000", reserved)
	require.Error(t, r.ResolveCycle(ctx, state.Cycle.ID, 99, "cancel", "重复取消", settle))
	_, err = r.Claim(ctx, 1, settle, draw)
	require.NoError(t, err)
	state, err = r.GetUserState(ctx, 1, settle, c)
	require.NoError(t, err)
	require.Equal(t, "active", state.Cycle.State)
	newID := state.Cycle.ID
	for day := 0; day < 7; day++ {
		at := settle.AddDate(0, 0, day)
		if day > 0 {
			_, err = r.Claim(ctx, 1, at, draw)
			require.NoError(t, err)
		}
		_, err = db.Exec("INSERT INTO usage_logs(user_id,request_id,input_tokens,created_at) VALUES(1,$1,1,$2)", fmt.Sprintf("new%d", day), at)
		require.NoError(t, err)
	}
	_, err = db.Exec("UPDATE users SET status='disabled' WHERE id=1")
	require.NoError(t, err)
	nextSettle := settle.AddDate(0, 0, 8)
	require.NoError(t, r.ProcessCycles(ctx, nextSettle))
	state, err = r.GetUserState(ctx, 1, nextSettle, c)
	require.NoError(t, err)
	require.Equal(t, "blocked_user", state.Cycle.State)
	require.Error(t, r.ResolveCycle(ctx, newID, 99, "cancel", "不能取消已经达标奖", nextSettle))
	_, err = db.Exec("UPDATE users SET status='active',deleted_at=NOW() WHERE id=1")
	require.NoError(t, err)
	require.NoError(t, r.ProcessCycles(ctx, nextSettle))
	var cycleState string
	require.NoError(t, db.QueryRow("SELECT state FROM checkin_cycles WHERE id=$1", newID).Scan(&cycleState))
	require.Equal(t, "blocked_user", cycleState)
	_, err = db.Exec("UPDATE users SET deleted_at=NULL WHERE id=1")
	require.NoError(t, err)
	require.NoError(t, r.ResolveCycle(ctx, newID, 99, "retry", "账户已恢复，再核对结算", nextSettle))
	state, err = r.GetUserState(ctx, 1, nextSettle, c)
	require.NoError(t, err)
	require.Equal(t, "completed", state.Cycle.State)
	h, err := r.ListCycles(ctx, 0, 0, 1)
	require.NoError(t, err)
	require.Len(t, h.Items, 1)
	require.Equal(t, int64(1), h.Items[0].UserID)
	require.NotZero(t, h.NextCursor)
	var audits int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM checkin_cycle_audit").Scan(&audits))
	require.Equal(t, 3, audits)
}

func TestCheckinPostgresDisabledTriggerAndDedupRetention(t *testing.T) {
	db := checkinTestDB(t)
	r := NewCheckinRepository(db)
	ctx := context.Background()
	now := time.Now()
	c := service.DefaultCheckinConfig()
	_, err := db.Exec("INSERT INTO usage_logs(user_id,request_id,input_tokens,created_at) VALUES(5,'disabled',1,$1)", now)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM checkin_request_dedup").Scan(&count))
	require.Zero(t, count)
	c.Enabled = true
	_, err = r.SaveConfig(ctx, c, 0, 1, now.Add(-time.Hour))
	require.NoError(t, err)
	old := now.AddDate(0, 0, -40)
	date := checkinDate(old, c)
	_, err = db.Exec("INSERT INTO checkin_request_days(user_id,business_date,requests) VALUES(5,$1::date,12)", date)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO checkin_request_dedup(user_id,request_key,business_date) VALUES(5,'req:old',$1::date)", date)
	require.NoError(t, err)
	require.NoError(t, r.ProcessCycles(ctx, now))
	var finalized bool
	require.NoError(t, db.QueryRow("SELECT finalized_at IS NOT NULL FROM checkin_request_days WHERE user_id=5").Scan(&finalized))
	require.True(t, finalized)
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM checkin_request_dedup").Scan(&count))
	require.Zero(t, count)
	_, err = db.Exec("INSERT INTO usage_logs(user_id,request_id,input_tokens,created_at) VALUES(5,'old',1,$1)", old)
	require.NoError(t, err)
	require.NoError(t, db.QueryRow("SELECT requests FROM checkin_request_days WHERE user_id=5").Scan(&count))
	require.Equal(t, 12, count)
}

func TestCheckinPostgresBatchRotationAvoidsStarvation(t *testing.T) {
	db := checkinTestDB(t)
	r := NewCheckinRepository(db)
	ctx := context.Background()
	now := time.Now()
	c := service.DefaultCheckinConfig()
	c.Enabled = true
	c.CycleEnabled = true
	saved, err := r.SaveConfig(ctx, c, 0, 1, now.AddDate(0, 0, -2))
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO users(id,balance) SELECT n,0 FROM generate_series(1,205) n")
	require.NoError(t, err)
	date := checkinDate(now.AddDate(0, 0, -1), c)
	_, err = db.Exec("INSERT INTO checkin_budgets(business_date,limit_usd,reserved_usd) VALUES($1::date,1000,102.5)", date)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO checkin_cycles(user_id,state,start_date,end_date,budget_date,rule_version,reward_amount,requests_threshold,settle_at,reserved,updated_at)
 SELECT n,'active',$1::date,$1::date+6,$1::date,$2,0.5,300,$3,true,$4 FROM generate_series(1,205) n`, date, saved.Version, now.AddDate(0, 0, 6), now.AddDate(0, 0, -1))
	require.NoError(t, err)
	for _, expected := range []int{100, 200, 205} {
		require.NoError(t, r.ProcessCycles(ctx, now))
		var broken int
		require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM checkin_cycles WHERE state='broken'").Scan(&broken))
		require.Equal(t, expected, broken)
	}
	var reserved string
	require.NoError(t, db.QueryRow("SELECT reserved_usd::text FROM checkin_budgets").Scan(&reserved))
	require.Equal(t, "0.00000000", reserved)
	// Broken yesterday can open a new seven-day commitment before old window end.
	_, err = db.Exec("INSERT INTO payment_orders(id,user_id,amount,bonus_amount,pay_amount,refund_amount,order_type,status,paid_at,completed_at) VALUES(1,1,10,0,72,0,'balance','COMPLETED',NOW(),NOW())")
	require.NoError(t, err)
	_, err = r.Claim(ctx, 1, now, func(service.CheckinConfig, service.CheckinInputs) (service.CheckinDraw, error) {
		return service.CheckinDraw{Amount: "0.15", Weights: []int64{8500, 700, 400, 200, 150, 50}}, nil
	})
	require.NoError(t, err)
	state, err := r.GetUserState(ctx, 1, now, c)
	require.NoError(t, err)
	require.Equal(t, "active", state.Cycle.State)
	require.Equal(t, checkinDate(now, c), state.Cycle.StartDate)
}
