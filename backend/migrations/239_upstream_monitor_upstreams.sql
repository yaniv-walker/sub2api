CREATE TABLE IF NOT EXISTS upstream_monitor_upstreams (
    id BIGSERIAL PRIMARY KEY,
    base_url VARCHAR(500) NOT NULL UNIQUE,
    name VARCHAR(200) NOT NULL DEFAULT '',
    upstream_type VARCHAR(20) NOT NULL CHECK (upstream_type IN ('sub2api', 'nexapi')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE upstream_monitor_upstreams IS 'Upstream monitor plugin-owned upstream configuration';
COMMENT ON COLUMN upstream_monitor_upstreams.base_url IS 'Normalized origin used to group host accounts';
