-- Independent rewards never enter recharge/referral accounting.
CREATE TABLE IF NOT EXISTS checkin_rules (
 version BIGSERIAL PRIMARY KEY, config JSONB NOT NULL,
 enabled BOOLEAN NOT NULL, effective_at TIMESTAMPTZ NOT NULL,
 actor_id BIGINT NOT NULL, created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS checkin_rules_effective ON checkin_rules(effective_at, version);
CREATE TABLE IF NOT EXISTS checkin_budgets (
 business_date DATE PRIMARY KEY, limit_usd NUMERIC(20,8) NOT NULL CHECK(limit_usd >= 0),
 spent_usd NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(spent_usd >= 0),
 reserved_usd NUMERIC(20,8) NOT NULL DEFAULT 0 CHECK(reserved_usd >= 0),
 CHECK(spent_usd + reserved_usd <= limit_usd)
);
CREATE TABLE IF NOT EXISTS checkin_records (
 id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL REFERENCES users(id),
 business_date DATE NOT NULL, reward_type TEXT NOT NULL CHECK(reward_type IN ('daily','cycle')),
 reward_amount NUMERIC(20,8) NOT NULL CHECK(reward_amount > 0), balance_after NUMERIC(20,8) NOT NULL,
 rule_version BIGINT NOT NULL REFERENCES checkin_rules(version), claimed_at TIMESTAMPTZ NOT NULL,
 bonus JSONB NOT NULL, weights JSONB NOT NULL,
 UNIQUE(user_id, business_date, reward_type)
);
CREATE INDEX IF NOT EXISTS checkin_records_user_history ON checkin_records(user_id,id DESC);
CREATE TABLE IF NOT EXISTS checkin_reward_ledger (
 id BIGSERIAL PRIMARY KEY, record_id BIGINT NOT NULL UNIQUE REFERENCES checkin_records(id),
 user_id BIGINT NOT NULL REFERENCES users(id), amount NUMERIC(20,8) NOT NULL CHECK(amount > 0),
 created_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS checkin_invalidation_outbox (
 id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL, created_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS checkin_paid_contributions (
 order_id BIGINT PRIMARY KEY REFERENCES payment_orders(id), user_id BIGINT NOT NULL,
 amount NUMERIC(20,8) NOT NULL CHECK(amount >= 0), fulfilled_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS checkin_paid_contributions_user ON checkin_paid_contributions(user_id);
-- refund_amount is credited USD, NOT gateway payment currency (pay_amount).
CREATE OR REPLACE FUNCTION checkin_sync_paid_contribution() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.order_type='balance' AND NEW.completed_at IS NOT NULL AND NEW.paid_at IS NOT NULL AND NEW.pay_amount>0 THEN
 INSERT INTO checkin_paid_contributions(order_id,user_id,amount,fulfilled_at,updated_at)
 VALUES(NEW.id,NEW.user_id,CASE WHEN NEW.status IN ('COMPLETED','PARTIALLY_REFUNDED') THEN ROUND(GREATEST(NEW.amount-NEW.bonus_amount,0)*GREATEST(1-LEAST(GREATEST(NEW.refund_amount,0)/NULLIF(NEW.amount,0),1),0),8) ELSE 0 END,NEW.completed_at,NOW())
 ON CONFLICT(order_id) DO UPDATE SET user_id=EXCLUDED.user_id,amount=EXCLUDED.amount,fulfilled_at=EXCLUDED.fulfilled_at,updated_at=EXCLUDED.updated_at;
 ELSE
 UPDATE checkin_paid_contributions SET amount=0,updated_at=NOW() WHERE order_id=NEW.id;
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS checkin_payment_contribution ON payment_orders;
CREATE TRIGGER checkin_payment_contribution AFTER INSERT OR UPDATE ON payment_orders FOR EACH ROW EXECUTE FUNCTION checkin_sync_paid_contribution();
INSERT INTO checkin_paid_contributions(order_id,user_id,amount,fulfilled_at,updated_at)
 SELECT id,user_id,CASE WHEN status IN ('COMPLETED','PARTIALLY_REFUNDED') THEN ROUND(GREATEST(amount-bonus_amount,0)*GREATEST(1-LEAST(GREATEST(refund_amount,0)/NULLIF(amount,0),1),0),8) ELSE 0 END,completed_at,NOW()
 FROM payment_orders WHERE order_type='balance' AND completed_at IS NOT NULL AND paid_at IS NOT NULL AND pay_amount>0
 ON CONFLICT(order_id) DO UPDATE SET amount=EXCLUDED.amount,updated_at=EXCLUDED.updated_at;
CREATE TABLE IF NOT EXISTS checkin_request_dedup (
 user_id BIGINT NOT NULL, request_key TEXT NOT NULL, business_date DATE NOT NULL,
 PRIMARY KEY(user_id,request_key)
);
CREATE TABLE IF NOT EXISTS checkin_request_days (
 user_id BIGINT NOT NULL, business_date DATE NOT NULL, requests BIGINT NOT NULL DEFAULT 0,
 last_inserted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), finalized_at TIMESTAMPTZ, PRIMARY KEY(user_id,business_date)
);
CREATE INDEX IF NOT EXISTS checkin_request_dedup_expiry ON checkin_request_dedup(business_date,user_id);
-- Durable counters survive usage retention; retries of the same request count once.
-- No historical backfill: activity prior to feature activation cannot start a cycle.
CREATE OR REPLACE FUNCTION checkin_count_usage() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE inserted_count INTEGER; activity_enabled BOOLEAN; cycle_open BOOLEAN;
 usage_date DATE; day_finalized TIMESTAMPTZ;
BEGIN
 -- Failed-request placeholders have neither billed cost nor useful usage.
 -- Include free token requests and image/video billing with zero text tokens.
 IF COALESCE(NEW.actual_cost,0) <= 0
 AND COALESCE(NEW.input_tokens,0)::bigint+COALESCE(NEW.output_tokens,0)::bigint+COALESCE(NEW.cache_creation_tokens,0)::bigint+COALESCE(NEW.cache_read_tokens,0)::bigint+COALESCE(NEW.image_input_tokens,0)::bigint+COALESCE(NEW.image_output_tokens,0)::bigint <= 0
 AND COALESCE(NEW.image_count,0) <= 0 THEN RETURN NEW; END IF;
 usage_date := (NEW.created_at AT TIME ZONE 'Asia/Shanghai')::date;
 SELECT COALESCE((SELECT enabled FROM checkin_rules WHERE effective_at<=NOW() ORDER BY version DESC LIMIT 1),false)
 AND COALESCE((SELECT enabled FROM checkin_rules ORDER BY version DESC LIMIT 1),false) INTO activity_enabled;
 SELECT EXISTS(SELECT 1 FROM checkin_cycles WHERE user_id=NEW.user_id AND state IN ('active','pending_usage','blocked_user') AND usage_date BETWEEN start_date AND end_date) INTO cycle_open;
 IF NOT activity_enabled AND NOT cycle_open THEN RETURN NEW; END IF;
 IF usage_date < (NOW() AT TIME ZONE 'Asia/Shanghai')::date-35 AND NOT cycle_open THEN RETURN NEW; END IF;
 -- A finalized snapshot is immutable, including after dedup retention expires.
 SELECT finalized_at INTO day_finalized FROM checkin_request_days WHERE user_id=NEW.user_id AND business_date=usage_date FOR UPDATE;
 IF day_finalized IS NOT NULL THEN RETURN NEW; END IF;
 INSERT INTO checkin_request_dedup(user_id,request_key,business_date)
 VALUES(NEW.user_id,CASE WHEN COALESCE(NEW.request_id,'') <> '' THEN 'req:'||NEW.request_id ELSE 'row:'||NEW.id::text END,usage_date)
 ON CONFLICT DO NOTHING;
 GET DIAGNOSTICS inserted_count = ROW_COUNT;
 IF inserted_count > 0 THEN
 INSERT INTO checkin_request_days(user_id,business_date,requests) VALUES(NEW.user_id,usage_date,1)
 ON CONFLICT(user_id,business_date) DO UPDATE SET requests=checkin_request_days.requests+1,last_inserted_at=NOW();
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS checkin_usage_insert ON usage_logs;
CREATE TRIGGER checkin_usage_insert AFTER INSERT ON usage_logs FOR EACH ROW EXECUTE FUNCTION checkin_count_usage();
CREATE TABLE IF NOT EXISTS checkin_cycles (
 id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL REFERENCES users(id),
 state TEXT NOT NULL CHECK(state IN ('active','pending_usage','blocked_user','completed','broken','capacity_exhausted')),
 start_date DATE NOT NULL, end_date DATE NOT NULL, budget_date DATE NOT NULL REFERENCES checkin_budgets(business_date),
 rule_version BIGINT NOT NULL REFERENCES checkin_rules(version), reward_amount NUMERIC(20,8) NOT NULL,
 requests_threshold BIGINT NOT NULL, settle_at TIMESTAMPTZ NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 reserved BOOLEAN NOT NULL, settled_record_id BIGINT REFERENCES checkin_records(id),
 UNIQUE(user_id,start_date), CHECK(end_date >= start_date)
);
CREATE UNIQUE INDEX IF NOT EXISTS checkin_cycles_open_user ON checkin_cycles(user_id) WHERE state IN ('active','pending_usage','blocked_user');
CREATE INDEX IF NOT EXISTS checkin_cycles_processing ON checkin_cycles(updated_at,id) WHERE state IN ('active','pending_usage','blocked_user');
CREATE TABLE IF NOT EXISTS checkin_cycle_audit (
 id BIGSERIAL PRIMARY KEY, cycle_id BIGINT NOT NULL REFERENCES checkin_cycles(id),
 actor_id BIGINT NOT NULL, action TEXT NOT NULL CHECK(action IN ('retry','cancel')),
 reason TEXT NOT NULL CHECK(length(trim(reason)) > 0),
 before_state TEXT NOT NULL, after_state TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL
);
