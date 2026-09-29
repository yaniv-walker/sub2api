package upstreammonitor

// Config holds the configuration for the upstream monitor plugin.
type Config struct {
	Enabled     bool            `mapstructure:"enabled" yaml:"enabled"`
	AutoMigrate bool            `mapstructure:"auto_migrate" yaml:"auto_migrate"`
	Features    FeaturesConfig  `mapstructure:"features" yaml:"features"`
	Cache       CacheConfig     `mapstructure:"cache" yaml:"cache"`
	Alerts      AlertsConfig    `mapstructure:"alerts" yaml:"alerts"`
	Prediction  PredictionConfig `mapstructure:"prediction" yaml:"prediction"`
}

// FeaturesConfig controls which features are enabled.
type FeaturesConfig struct {
	BalanceMonitoring     bool `mapstructure:"balance_monitoring" yaml:"balance_monitoring"`
	ErrorAnalysis         bool `mapstructure:"error_analysis" yaml:"error_analysis"`
	UsagePrediction       bool `mapstructure:"usage_prediction" yaml:"usage_prediction"`
	ConcurrencyMonitoring bool `mapstructure:"concurrency_monitoring" yaml:"concurrency_monitoring"`
}

// CacheConfig controls caching behavior.
type CacheConfig struct {
	Enabled        bool   `mapstructure:"enabled" yaml:"enabled"`
	TTL            int    `mapstructure:"ttl" yaml:"ttl"` // seconds
	RedisKeyPrefix string `mapstructure:"redis_key_prefix" yaml:"redis_key_prefix"`
}

// AlertsConfig controls alert thresholds.
type AlertsConfig struct {
	LowBalanceThreshold     float64 `mapstructure:"low_balance_threshold" yaml:"low_balance_threshold"`         // CNY
	HighErrorRateThreshold  float64 `mapstructure:"high_error_rate_threshold" yaml:"high_error_rate_threshold"` // 0.1 = 10%
}

// PredictionConfig controls prediction algorithm settings.
type PredictionConfig struct {
	Algorithm      string `mapstructure:"algorithm" yaml:"algorithm"`           // "moving_average" or "linear_regression"
	WindowDays     int    `mapstructure:"window_days" yaml:"window_days"`       // Number of days to look back
	MinDataPoints  int    `mapstructure:"min_data_points" yaml:"min_data_points"` // Minimum data points required
}

// DefaultConfig returns the default plugin configuration.
func DefaultConfig() Config {
	return Config{
		Enabled:     false, // Disabled by default
		AutoMigrate: true,
		Features: FeaturesConfig{
			BalanceMonitoring:     true,
			ErrorAnalysis:         true,
			UsagePrediction:       true,
			ConcurrencyMonitoring: true,
		},
		Cache: CacheConfig{
			Enabled:        true,
			TTL:            300, // 5 minutes
			RedisKeyPrefix: "upstream_monitor:",
		},
		Alerts: AlertsConfig{
			LowBalanceThreshold:    10.0,  // ¥10
			HighErrorRateThreshold: 0.1,   // 10%
		},
		Prediction: PredictionConfig{
			Algorithm:     "moving_average",
			WindowDays:    30,
			MinDataPoints: 7,
		},
	}
}
