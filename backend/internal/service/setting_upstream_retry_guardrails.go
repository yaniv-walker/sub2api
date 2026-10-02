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

const SettingKeyUpstreamRetryGuardrails = "upstream_retry_guardrails"

const (
	upstreamRetryGuardrailsTTL       = 30 * time.Second
	upstreamRetryGuardrailsErrorTTL  = 5 * time.Second
	upstreamRetryGuardrailsDBTimeout = 2 * time.Second
)

type UpstreamRetryGuardrailsSettings struct {
	Enabled bool `json:"enabled"`
}

type upstreamRetryGuardrailsSnapshot struct {
	enabled   bool
	expiresAt time.Time
}

type upstreamRetryGuardrailsRuntime struct {
	mu         sync.Mutex
	snapshot   atomic.Pointer[upstreamRetryGuardrailsSnapshot]
	refreshing atomic.Bool
}

func (s *SettingService) readUpstreamRetryGuardrails(ctx context.Context) (UpstreamRetryGuardrailsSettings, error) {
	if s == nil || s.settingRepo == nil {
		return UpstreamRetryGuardrailsSettings{}, nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyUpstreamRetryGuardrails)
	if errors.Is(err, ErrSettingNotFound) || (err == nil && value == "") {
		return UpstreamRetryGuardrailsSettings{}, nil
	}
	if err != nil {
		return UpstreamRetryGuardrailsSettings{}, fmt.Errorf("get upstream retry guardrails: %w", err)
	}
	var settings UpstreamRetryGuardrailsSettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return settings, fmt.Errorf("invalid upstream retry guardrails settings")
	}
	return settings, nil
}

func (s *SettingService) GetUpstreamRetryGuardrailsSettings(ctx context.Context) (UpstreamRetryGuardrailsSettings, error) {
	if s == nil {
		return UpstreamRetryGuardrailsSettings{}, nil
	}
	runtime := &s.upstreamRetryGuardrailsRuntime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	settings, err := s.readUpstreamRetryGuardrails(ctx)
	if err == nil {
		runtime.snapshot.Store(&upstreamRetryGuardrailsSnapshot{enabled: settings.Enabled, expiresAt: time.Now().Add(upstreamRetryGuardrailsTTL)})
	}
	return settings, err
}

func (s *SettingService) SetUpstreamRetryGuardrailsSettings(ctx context.Context, settings UpstreamRetryGuardrailsSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("settings repository unavailable")
	}
	runtime := &s.upstreamRetryGuardrailsRuntime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if err := s.settingRepo.Set(ctx, SettingKeyUpstreamRetryGuardrails, string(data)); err != nil {
		return err
	}
	runtime.snapshot.Store(&upstreamRetryGuardrailsSnapshot{enabled: settings.Enabled, expiresAt: time.Now().Add(upstreamRetryGuardrailsTTL)})
	return nil
}

func (s *SettingService) InitializeUpstreamRetryGuardrails() {
	if s != nil {
		s.refreshUpstreamRetryGuardrails()
	}
}

func (s *SettingService) refreshUpstreamRetryGuardrails() {
	runtime := &s.upstreamRetryGuardrailsRuntime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), upstreamRetryGuardrailsDBTimeout)
	defer cancel()
	settings, err := s.readUpstreamRetryGuardrails(ctx)
	ttl := upstreamRetryGuardrailsTTL
	if err != nil {
		ttl = upstreamRetryGuardrailsErrorTTL
		if previous := runtime.snapshot.Load(); previous != nil {
			settings.Enabled = previous.enabled
		}
	}
	runtime.snapshot.Store(&upstreamRetryGuardrailsSnapshot{enabled: settings.Enabled, expiresAt: time.Now().Add(ttl)})
}

func (s *SettingService) UpstreamRetryGuardrailsEnabled() bool {
	if s == nil || s.settingRepo == nil {
		return false
	}
	runtime := &s.upstreamRetryGuardrailsRuntime
	snapshot := runtime.snapshot.Load()
	if (snapshot == nil || time.Now().After(snapshot.expiresAt)) && runtime.refreshing.CompareAndSwap(false, true) {
		go func() {
			defer runtime.refreshing.Store(false)
			s.refreshUpstreamRetryGuardrails()
		}()
	}
	return snapshot != nil && snapshot.enabled
}
