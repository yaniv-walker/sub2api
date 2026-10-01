package config

import "github.com/spf13/viper"

// UpstreamMonitorPluginConfig 上游监控插件配置
type UpstreamMonitorPluginConfig struct {
	Enabled     bool                            `mapstructure:"enabled"`
	AutoMigrate bool                            `mapstructure:"auto_migrate"`
	Features    UpstreamMonitorFeaturesConfig   `mapstructure:"features"`
	Cache       UpstreamMonitorCacheConfig      `mapstructure:"cache"`
	Alerts      UpstreamMonitorAlertsConfig     `mapstructure:"alerts"`
	Prediction  UpstreamMonitorPredictionConfig `mapstructure:"prediction"`
	Accounts    []UpstreamMonitorAccountConfig  `mapstructure:"accounts"`
}

// UpstreamMonitorAccountConfig 上游账号配置
type UpstreamMonitorAccountConfig struct {
	ID          int64  `mapstructure:"id"`
	Name        string `mapstructure:"name"`
	Type        string `mapstructure:"type"` // "sub2api" or "nexapi"
	ApiKey      string `mapstructure:"api_key"`
	Enabled     bool   `mapstructure:"enabled"`
	Description string `mapstructure:"description"`
}

type UpstreamMonitorFeaturesConfig struct {
	BalanceMonitoring     bool `mapstructure:"balance_monitoring"`
	ErrorAnalysis         bool `mapstructure:"error_analysis"`
	UsagePrediction       bool `mapstructure:"usage_prediction"`
	ConcurrencyMonitoring bool `mapstructure:"concurrency_monitoring"`
}

type UpstreamMonitorCacheConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	TTL            int    `mapstructure:"ttl"` // seconds
	RedisKeyPrefix string `mapstructure:"redis_key_prefix"`
}

type UpstreamMonitorAlertsConfig struct {
	LowBalanceThreshold    float64 `mapstructure:"low_balance_threshold"`
	HighErrorRateThreshold float64 `mapstructure:"high_error_rate_threshold"`
}

type UpstreamMonitorPredictionConfig struct {
	Algorithm     string `mapstructure:"algorithm"`
	WindowDays    int    `mapstructure:"window_days"`
	MinDataPoints int    `mapstructure:"min_data_points"`
}

func setUpstreamMonitorDefaults() {
	viper.SetDefault("plugins.upstream_monitor.enabled", false)
	viper.SetDefault("plugins.upstream_monitor.auto_migrate", true)
	viper.SetDefault("plugins.upstream_monitor.features.balance_monitoring", true)
	viper.SetDefault("plugins.upstream_monitor.features.error_analysis", true)
	viper.SetDefault("plugins.upstream_monitor.features.usage_prediction", true)
	viper.SetDefault("plugins.upstream_monitor.features.concurrency_monitoring", true)
	viper.SetDefault("plugins.upstream_monitor.cache.enabled", true)
	viper.SetDefault("plugins.upstream_monitor.cache.ttl", 300)
	viper.SetDefault("plugins.upstream_monitor.cache.redis_key_prefix", "upstream_monitor:")
	viper.SetDefault("plugins.upstream_monitor.alerts.low_balance_threshold", 10.0)
	viper.SetDefault("plugins.upstream_monitor.alerts.high_error_rate_threshold", 0.1)
	viper.SetDefault("plugins.upstream_monitor.prediction.algorithm", "moving_average")
	viper.SetDefault("plugins.upstream_monitor.prediction.window_days", 30)
	viper.SetDefault("plugins.upstream_monitor.prediction.min_data_points", 7)
}
