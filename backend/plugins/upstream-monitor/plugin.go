package upstreammonitor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/plugin"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/handler"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/repository"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Plugin implements the plugin.Plugin interface for upstream monitoring.
type Plugin struct {
	config           *config.UpstreamMonitorPluginConfig
	handler          *handler.MonitorHandler
	logger           *slog.Logger
	redisClient      *redis.Client
	entClient        *ent.Client
	errorRecords     *repository.ErrorRecordRepository
	balanceSnapshots *repository.BalanceSnapshotRepository
	enabled          bool
}

// NewPluginWithHandler creates a new upstream monitor plugin instance with injected handler.
func NewPluginWithHandler(
	cfg *config.UpstreamMonitorPluginConfig,
	redisClient *redis.Client,
	entClient *ent.Client,
	logger *slog.Logger,
	handler *handler.MonitorHandler,
	errorRecords *repository.ErrorRecordRepository,
	balanceSnapshots *repository.BalanceSnapshotRepository,
) *Plugin {
	if logger == nil {
		logger = slog.Default()
	}

	return &Plugin{
		config:           cfg,
		redisClient:      redisClient,
		entClient:        entClient,
		logger:           logger.With("plugin", "upstream-monitor"),
		handler:          handler,
		errorRecords:     errorRecords,
		balanceSnapshots: balanceSnapshots,
		enabled:          cfg.Enabled,
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

	// The plugin-owned configuration table is required by the routes, so it
	// must always be ensured when the enabled plugin starts. AutoMigrate is
	// retained for compatibility with older config files but cannot disable
	// this minimal, isolated bootstrap.
	if err := p.runMigrations(ctx); err != nil {
		return fmt.Errorf("failed to initialize plugin tables: %w", err)
	}
	p.logger.Info("Plugin-owned database tables ready")

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
	hookManager := plugin.GetGlobalHookManager()
	hookManager.Clear(plugin.HookAfterRequestCompleted)
	hookManager.Clear(plugin.HookAfterRequestFailed)
	hookManager.Clear(plugin.HookAfterAccountDeleted)
	return nil
}

// registerHooks registers event hooks for the plugin.
func (p *Plugin) registerHooks() {
	hookManager := plugin.GetGlobalHookManager()

	hookManager.Register(plugin.HookAfterRequestCompleted, func(ctx context.Context, data interface{}) error {
		event, ok := data.(plugin.RequestEvent)
		if !ok {
			return fmt.Errorf("unexpected request completed event type %T", data)
		}
		p.logger.Info("Upstream request completed", "account_id", event.AccountID, "upstream_type", event.UpstreamType, "model", event.Model, "request_id", event.RequestID)
		return nil
	})

	hookManager.Register(plugin.HookAfterRequestFailed, func(ctx context.Context, data interface{}) error {
		event, ok := data.(plugin.RequestEvent)
		if !ok {
			return fmt.Errorf("unexpected request failed event type %T", data)
		}
		if p.errorRecords == nil {
			return fmt.Errorf("error record repository is nil")
		}
		occurredAt := event.OccurredAt
		if occurredAt.IsZero() {
			occurredAt = time.Now()
		}
		errorType := event.ErrorType
		if errorType == "" {
			errorType = "upstream_error"
		}
		_, err := p.errorRecords.Create(ctx, event.UpstreamType, event.AccountID, errorType, event.ErrorMessage, event.HTTPStatus, occurredAt)
		return err
	})

	// Listen for account deleted events to clean up plugin data
	hookManager.Register(plugin.HookAfterAccountDeleted, func(ctx context.Context, data interface{}) error {
		event, ok := data.(plugin.AccountDeletedEvent)
		if !ok {
			return fmt.Errorf("unexpected account deleted event type %T", data)
		}
		if p.errorRecords != nil {
			if _, err := p.errorRecords.DeleteByAccount(ctx, event.UpstreamType, event.AccountID); err != nil {
				return err
			}
		}
		if p.balanceSnapshots != nil {
			if _, err := p.balanceSnapshots.DeleteByAccount(ctx, event.UpstreamType, event.AccountID); err != nil {
				return err
			}
		}
		return nil
	})

	p.logger.Info("Event hooks registered")
}

// runMigrations runs database migrations for the plugin.
func (p *Plugin) runMigrations(ctx context.Context) error {
	if p.entClient == nil {
		return fmt.Errorf("ent client is nil, cannot run migrations")
	}
	// Create only the plugin-owned configuration table. Calling the global Ent
	// schema migration here would also mutate unrelated host tables.
	_, err := p.entClient.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS upstream_monitor_upstreams (
    id BIGSERIAL PRIMARY KEY,
    base_url VARCHAR(500) NOT NULL UNIQUE,
    name VARCHAR(200) NOT NULL DEFAULT '',
    upstream_type VARCHAR(20) NOT NULL CHECK (upstream_type IN ('sub2api', 'nexapi')),
    access_token TEXT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`)
	if err != nil {
		return fmt.Errorf("create upstream monitor configuration table: %w", err)
	}
	if _, err := p.entClient.ExecContext(ctx, `ALTER TABLE upstream_monitor_upstreams ADD COLUMN IF NOT EXISTS access_token TEXT NULL`); err != nil {
		return fmt.Errorf("add upstream monitor access token column: %w", err)
	}
	return nil
}
