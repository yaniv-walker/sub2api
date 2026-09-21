package plugin

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
)

// Plugin defines the interface that all plugins must implement.
type Plugin interface {
	// Name returns the unique name of the plugin.
	Name() string

	// Version returns the version of the plugin.
	Version() string

	// Description returns a brief description of the plugin.
	Description() string

	// Init initializes the plugin.
	// This is called once during application startup.
	Init(ctx context.Context) error

	// RegisterRoutes registers HTTP routes for the plugin.
	// The provided router group is specific to this plugin.
	RegisterRoutes(router *gin.RouterGroup)

	// Enabled returns whether the plugin is enabled.
	Enabled() bool

	// Cleanup performs cleanup when the plugin is being shut down.
	Cleanup() error
}

// Manager manages plugin lifecycle.
type Manager struct {
	plugins map[string]Plugin
	logger  *slog.Logger
}

// NewManager creates a new plugin manager.
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		plugins: make(map[string]Plugin),
		logger:  logger,
	}
}

// Register registers a plugin with the manager.
func (m *Manager) Register(plugin Plugin) error {
	if plugin == nil {
		return fmt.Errorf("plugin cannot be nil")
	}

	name := plugin.Name()
	if name == "" {
		return fmt.Errorf("plugin name cannot be empty")
	}

	if _, exists := m.plugins[name]; exists {
		return fmt.Errorf("plugin %s is already registered", name)
	}

	m.plugins[name] = plugin
	m.logger.Info("Plugin registered", "name", name, "version", plugin.Version())
	return nil
}

// InitAll initializes all registered and enabled plugins.
func (m *Manager) InitAll(ctx context.Context) error {
	for name, p := range m.plugins {
		if !p.Enabled() {
			m.logger.Info("Plugin disabled, skipping initialization", "name", name)
			continue
		}

		m.logger.Info("Initializing plugin", "name", name)
		if err := p.Init(ctx); err != nil {
			return fmt.Errorf("failed to initialize plugin %s: %w", name, err)
		}
		m.logger.Info("Plugin initialized successfully", "name", name)
	}
	return nil
}

// RegisterAllRoutes registers routes for all enabled plugins.
// RegisterAllRoutes registers enabled plugin routes behind the supplied admin middleware.
func (m *Manager) RegisterAllRoutes(router *gin.Engine, adminAuth gin.HandlerFunc) {
	for name, p := range m.plugins {
		if !p.Enabled() {
			continue
		}

		// Create a plugin-specific route group
		// Example: /api/v1/plugins/upstream-monitor
		pluginGroup := router.Group(fmt.Sprintf("/api/v1/plugins/%s", name))
		if adminAuth != nil {
			pluginGroup.Use(adminAuth)
		}
		p.RegisterRoutes(pluginGroup)
		m.logger.Info("Plugin routes registered", "name", name, "prefix", pluginGroup.BasePath())
	}
}

// CleanupAll performs cleanup for all plugins.
func (m *Manager) CleanupAll() {
	for name, p := range m.plugins {
		if !p.Enabled() {
			continue
		}

		m.logger.Info("Cleaning up plugin", "name", name)
		if err := p.Cleanup(); err != nil {
			m.logger.Error("Plugin cleanup failed", "name", name, "error", err)
		} else {
			m.logger.Info("Plugin cleaned up successfully", "name", name)
		}
	}
}

// Get returns a plugin by name.
func (m *Manager) Get(name string) (Plugin, bool) {
	p, exists := m.plugins[name]
	return p, exists
}

// List returns all registered plugins.
func (m *Manager) List() []Plugin {
	plugins := make([]Plugin, 0, len(m.plugins))
	for _, p := range m.plugins {
		plugins = append(plugins, p)
	}
	return plugins
}
