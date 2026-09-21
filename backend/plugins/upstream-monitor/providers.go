package upstreammonitor

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	hostservice "github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/handler"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/repository"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/service"
	"github.com/google/wire"
)

// ProviderSet is the Wire provider set for the upstream monitor plugin.
var ProviderSet = wire.NewSet(
	// Repository layer
	repository.NewBalanceSnapshotRepository,
	repository.NewErrorRecordRepository,
	repository.NewUpstreamRepository,
	repository.NewRepositories,

	// Service layer
	service.NewUpstreamInfoFetcher,
	service.NewBalanceAggregator,
	service.NewErrorAnalyzer,
	ProvideUsagePredictor,

	// Handler layer
	ProvideAccountProvider,
	handler.NewMonitorHandler,

	// Plugin
	ProvidePluginConfig,
	NewPluginWithHandler,
)

// ProvideAccountProvider keeps the plugin dependent on the narrow handler
// contract while sourcing accounts from the host database repository.
func ProvideAccountProvider(repo hostservice.AccountRepository) handler.AccountProvider {
	return repo
}

// ProvidePluginConfig extracts the plugin config from the main config.
func ProvidePluginConfig(cfg *config.Config) *config.UpstreamMonitorPluginConfig {
	return &cfg.Plugins.UpstreamMonitor
}

// ProvideUsagePredictor creates a usage predictor with configuration.
func ProvideUsagePredictor(cfg *config.UpstreamMonitorPluginConfig, snapshotRepo *repository.BalanceSnapshotRepository) *service.UsagePredictor {
	predictorConfig := &service.PredictionConfig{
		Algorithm:     cfg.Prediction.Algorithm,
		WindowDays:    cfg.Prediction.WindowDays,
		MinDataPoints: cfg.Prediction.MinDataPoints,
	}
	if predictorConfig.Algorithm == "" {
		predictorConfig.Algorithm = "moving_average"
	}
	if predictorConfig.WindowDays == 0 {
		predictorConfig.WindowDays = 30
	}
	if predictorConfig.MinDataPoints == 0 {
		predictorConfig.MinDataPoints = 7
	}
	return service.NewUsagePredictor(predictorConfig, snapshotRepo)
}
