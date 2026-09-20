package upstreammonitor

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/plugin"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/handler"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Plugin implements the plugin.Plugin interface for upstream monitoring.
type Plugin struct {
	config      *config.UpstreamMonitorPluginConfig
	handler     *handler.MonitorHandler
	logger      *slog.Logger
	redisClient *redis.Client
	entClient   *ent.Client
	enabled     bool
}

// NewPluginWithHandler creates a new upstream monitor plugin instance with injected handler.
func NewPluginWithHandler(
	cfg *config.UpstreamMonitorPluginConfig,
	redisClient *redis.Client,
	entClient *ent.Client,
	logger *slog.Logger,
	handler *handler.MonitorHandler,
) *Plugin {
	if logger == nil {
		logger = slog.Default()
	}

	return &Plugin{
		config:      cfg,
		redisClient: redisClient,
		entClient:   entClient,
		logger:      logger.With("plugin", "upstream-monitor"),
		handler:     handler,
		enabled:     cfg.Enabled,
	}
}

// Name returns the plugin name.
func (p *Plugin) Name() string {
	return "upstream-monitor"
}

// Version returns the plugin version.
func (p *Plugin) Version() string {
	return "1.0.0"
}

// Description returns the plugin description.
func (p *Plugin) Description() string {
	return "上游账号统一监控与管理插件"
}

// Enabled returns whether the plugin is enabled.
func (p *Plugin) Enabled() bool {
	return p.enabled
}

// Init initializes the plugin.
func (p *Plugin) Init(ctx context.Context) error {
	p.logger.Info("Initializing upstream monitor plugin",
		"auto_migrate", p.config.AutoMigrate,
		"features", fmt.Sprintf("%+v", p.config.Features),
	)

	// Run database migrations if enabled
	if p.config.AutoMigrate {
		if err := p.runMigrations(ctx); err != nil {
			return fmt.Errorf("failed to run migrations: %w", err)
		}
		p.logger.Info("Database migrations completed successfully")
	}

	// Register event hooks
	p.registerHooks()

	p.logger.Info("Upstream monitor plugin initialized successfully")
	return nil
}

// RegisterRoutes registers HTTP routes for the plugin.
func (p *Plugin) RegisterRoutes(router *gin.RouterGroup) {
	p.logger.Info("Registering routes", "base_path", router.BasePath())
	p.handler.RegisterRoutes(router)
}

// Cleanup performs cleanup when the plugin is shut down.
func (p *Plugin) Cleanup() error {
	p.logger.Info("Cleaning up upstream monitor plugin")
	// TODO: Implement cleanup logic if needed
	return nil
}

// registerHooks registers event hooks for the plugin.
func (p *Plugin) registerHooks() {
	hookManager := plugin.GetGlobalHookManager()

	// Listen for request failed events to record errors
	hookManager.Register(plugin.HookAfterRequestFailed, func(ctx context.Context, data interface{}) error {
		// TODO: Extract event data and record error
		p.logger.Debug("Received request failed event", "data", data)
		return nil
	})

	// Listen for account deleted events to clean up plugin data
	hookManager.Register(plugin.HookAfterAccountDeleted, func(ctx context.Context, data interface{}) error {
		// TODO: Clean up plugin data for the deleted account
		p.logger.Debug("Received account deleted event", "data", data)
		return nil
	})

	p.logger.Info("Event hooks registered")
}

// runMigrations runs database migrations for the plugin.
func (p *Plugin) runMigrations(ctx context.Context) error {
	p.logger.Info("Running database migrations for upstream monitor plugin")

	if p.entClient == nil {
		return fmt.Errorf("ent client is nil, cannot run migrations")
	}

	// Run auto-migration to create tables and indexes
	if err := p.entClient.Schema.Create(ctx); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}
