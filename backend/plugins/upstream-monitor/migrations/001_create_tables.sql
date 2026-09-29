-- Upstream Monitor Plugin Database Migrations
-- Version: 001
-- Description: Create account error logs table

-- Table: account_error_logs
-- Purpose: Store error logs from upstream accounts for analysis
CREATE TABLE IF NOT EXISTS account_error_logs (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    account_id BIGINT NOT NULL COMMENT 'Account ID from accounts table',
    error_type VARCHAR(50) COMMENT 'Error type: rate_limit, server_error, timeout, etc.',
    error_code VARCHAR(20) COMMENT 'HTTP status code or error code: 429, 500, 503, etc.',
    error_message TEXT COMMENT 'Detailed error message',
    request_id VARCHAR(100) COMMENT 'Request ID for tracing',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    INDEX idx_account_id (account_id),
    INDEX idx_error_type (error_type),
    INDEX idx_created_at (created_at),
    INDEX idx_account_created (account_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Upstream account error logs (plugin: upstream-monitor)';

-- Table: account_daily_usage_summary
-- Purpose: Store daily aggregated usage statistics for accounts
CREATE TABLE IF NOT EXISTS account_daily_usage_summary (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    account_id BIGINT NOT NULL,
    usage_date DATE NOT NULL COMMENT 'Date of usage (YYYY-MM-DD)',
    request_count INT DEFAULT 0 COMMENT 'Total requests for this day',
    total_cost DECIMAL(10,2) DEFAULT 0.00 COMMENT 'Total cost in CNY',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_account_date (account_id, usage_date),
    INDEX idx_usage_date (usage_date),
    INDEX idx_account_id (account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Daily usage summary (plugin: upstream-monitor)';

-- Table: plugin_upstream_monitor_config
-- Purpose: Store plugin-specific configuration per account
CREATE TABLE IF NOT EXISTS plugin_upstream_monitor_config (
    account_id BIGINT PRIMARY KEY,
    upstream_type VARCHAR(50) COMMENT 'Upstream type: sub2api, nexapi, etc.',
    concurrency_limit INT DEFAULT 0 COMMENT 'Concurrency limit (0 = unlimited)',
    alert_enabled BOOLEAN DEFAULT TRUE COMMENT 'Enable low balance alerts',
    alert_threshold DECIMAL(10,2) DEFAULT 10.00 COMMENT 'Alert threshold in CNY',
    balance_cache JSON COMMENT 'Cached balance information',
    last_balance_check TIMESTAMP NULL COMMENT 'Last time balance was checked',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_upstream_type (upstream_type),
    INDEX idx_last_check (last_balance_check)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Plugin configuration per account (plugin: upstream-monitor)';
