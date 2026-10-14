-- Eligibility excludes online-payment codes from offline recharge history.
CREATE INDEX IF NOT EXISTS idx_payment_orders_recharge_code ON payment_orders(recharge_code);
