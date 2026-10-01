package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const SettingKeyUpstreamMonitorEnabled = "upstream_monitor_enabled"

const (
	upstreamMonitorSettingTTL       = 30 * time.Second
	upstreamMonitorSettingErrorTTL  = 5 * time.Second
	upstreamMonitorSettingDBTimeout = 2 * time.Second
)

type UpstreamMonitorSettings struct {
	Enabled bool `json:"enabled"`
}

type upstreamMonitorSettingSnapshot struct {
	enabled   bool
	expiresAt time.Time
}

type upstreamMonitorSettingRuntime struct {
	mu         sync.Mutex
	snapshot   atomic.Pointer[upstreamMonitorSettingSnapshot]
	refreshing atomic.Bool
}

func (s *SettingService) upstreamMonitorDefaultEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.Plugins.UpstreamMonitor.Enabled
}

func (s *SettingService) readUpstreamMonitorSettings(ctx context.Context) (UpstreamMonitorSettings, error) {
	settings := UpstreamMonitorSettings{Enabled: s.upstreamMonitorDefaultEnabled()}
	if s == nil || s.settingRepo == nil {
		return settings, nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyUpstreamMonitorEnabled)
	if errors.Is(err, ErrSettingNotFound) || (err == nil && value == "") {
		return settings, nil
	}
	if err != nil {
		return settings, fmt.Errorf("get upstream monitor settings: %w", err)
	}
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return settings, fmt.Errorf("invalid upstream monitor settings")
	}
	return settings, nil
}

func (s *SettingService) GetUpstreamMonitorSettings(ctx context.Context) (UpstreamMonitorSettings, error) {
	if s == nil {
		return UpstreamMonitorSettings{}, nil
	}
	runtime := &s.upstreamMonitorSettingRuntime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	settings, err := s.readUpstreamMonitorSettings(ctx)
	if err == nil {
		runtime.snapshot.Store(&upstreamMonitorSettingSnapshot{enabled: settings.Enabled, expiresAt: time.Now().Add(upstreamMonitorSettingTTL)})
	}
	return settings, err
}

func (s *SettingService) SetUpstreamMonitorSettings(ctx context.Context, settings UpstreamMonitorSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("settings repository unavailable")
	}
	runtime := &s.upstreamMonitorSettingRuntime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if err := s.settingRepo.Set(ctx, SettingKeyUpstreamMonitorEnabled, string(data)); err != nil {
		return err
	}
	runtime.snapshot.Store(&upstreamMonitorSettingSnapshot{enabled: settings.Enabled, expiresAt: time.Now().Add(upstreamMonitorSettingTTL)})
	return nil
}

func (s *SettingService) InitializeUpstreamMonitorSettings() {
	if s != nil {
		s.refreshUpstreamMonitorSettings()
	}
}

func (s *SettingService) refreshUpstreamMonitorSettings() {
	runtime := &s.upstreamMonitorSettingRuntime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), upstreamMonitorSettingDBTimeout)
	defer cancel()
	settings, err := s.readUpstreamMonitorSettings(ctx)
	ttl := upstreamMonitorSettingTTL
	if err != nil {
		ttl = upstreamMonitorSettingErrorTTL
		if previous := runtime.snapshot.Load(); previous != nil {
			settings.Enabled = previous.enabled
		}
	}
	runtime.snapshot.Store(&upstreamMonitorSettingSnapshot{enabled: settings.Enabled, expiresAt: time.Now().Add(ttl)})
}

func (s *SettingService) UpstreamMonitorEnabled() bool {
	if s == nil || s.settingRepo == nil {
		return s.upstreamMonitorDefaultEnabled()
	}
	runtime := &s.upstreamMonitorSettingRuntime
	snapshot := runtime.snapshot.Load()
	if (snapshot == nil || time.Now().After(snapshot.expiresAt)) && runtime.refreshing.CompareAndSwap(false, true) {
		go func() {
			defer runtime.refreshing.Store(false)
			s.refreshUpstreamMonitorSettings()
		}()
	}
	return snapshot != nil && snapshot.enabled
}
