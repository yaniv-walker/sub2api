package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// HookType represents the type of hook event.
type HookType string

const (
	// HookAfterAccountCreated is triggered after an account is created.
	HookAfterAccountCreated HookType = "after_account_created"

	// HookAfterAccountUpdated is triggered after an account is updated.
	HookAfterAccountUpdated HookType = "after_account_updated"

	// HookAfterAccountDeleted is triggered after an account is deleted.
	HookAfterAccountDeleted HookType = "after_account_deleted"

	// HookAfterRequestCompleted is triggered after a request completes successfully.
	HookAfterRequestCompleted HookType = "after_request_completed"

	// HookAfterRequestFailed is triggered after a request fails.
	HookAfterRequestFailed HookType = "after_request_failed"
)

// HookHandler is a function that handles a hook event.
type HookHandler func(ctx context.Context, data interface{}) error

// HookManager manages event hooks for plugins.
type HookManager struct {
	mu      sync.RWMutex
	hooks   map[HookType][]HookHandler
	logger  *slog.Logger
}

// NewHookManager creates a new hook manager.
func NewHookManager(logger *slog.Logger) *HookManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &HookManager{
		hooks:  make(map[HookType][]HookHandler),
		logger: logger,
	}
}

// Register registers a hook handler for a specific event type.
func (m *HookManager) Register(hookType HookType, handler HookHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.hooks[hookType] = append(m.hooks[hookType], handler)
	m.logger.Debug("Hook handler registered", "type", hookType)
}

// Trigger triggers all registered handlers for a specific event type.
// Handlers are executed synchronously in the order they were registered.
func (m *HookManager) Trigger(ctx context.Context, hookType HookType, data interface{}) error {
	m.mu.RLock()
	handlers, exists := m.hooks[hookType]
	m.mu.RUnlock()

	if !exists || len(handlers) == 0 {
		return nil
	}

	m.logger.Debug("Triggering hook", "type", hookType, "handlers", len(handlers))

	for i, handler := range handlers {
		if err := handler(ctx, data); err != nil {
			m.logger.Error("Hook handler failed",
				"type", hookType,
				"handler_index", i,
				"error", err,
			)
			return fmt.Errorf("hook handler %d for %s failed: %w", i, hookType, err)
		}
	}

	return nil
}

// TriggerAsync triggers all registered handlers for a specific event type asynchronously.
// Errors are logged but do not block execution.
func (m *HookManager) TriggerAsync(ctx context.Context, hookType HookType, data interface{}) {
	m.mu.RLock()
	handlers, exists := m.hooks[hookType]
	m.mu.RUnlock()

	if !exists || len(handlers) == 0 {
		return
	}

	m.logger.Debug("Triggering hook async", "type", hookType, "handlers", len(handlers))

	for i, handler := range handlers {
		go func(idx int, h HookHandler) {
			if err := h(ctx, data); err != nil {
				m.logger.Error("Hook handler failed (async)",
					"type", hookType,
					"handler_index", idx,
					"error", err,
				)
			}
		}(i, handler)
	}
}

// Clear removes all handlers for a specific hook type.
func (m *HookManager) Clear(hookType HookType) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.hooks, hookType)
	m.logger.Debug("Hook handlers cleared", "type", hookType)
}

// ClearAll removes all registered hook handlers.
func (m *HookManager) ClearAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.hooks = make(map[HookType][]HookHandler)
	m.logger.Debug("All hook handlers cleared")
}

// Global hook manager instance (optional, can be injected via DI instead)
var (
	globalHookManager     *HookManager
	globalHookManagerOnce sync.Once
)

// GetGlobalHookManager returns the global hook manager instance.
// This is created lazily on first access.
func GetGlobalHookManager() *HookManager {
	globalHookManagerOnce.Do(func() {
		globalHookManager = NewHookManager(slog.Default())
	})
	return globalHookManager
}

// SetGlobalHookManager sets the global hook manager instance.
// This should be called during application initialization.
func SetGlobalHookManager(manager *HookManager) {
	globalHookManager = manager
}
