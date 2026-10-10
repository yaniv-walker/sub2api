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

// SettingKeyGatewayRemediation is the single runtime gate for additive
// performance/remediation behavior. It is opt-in and defaults off.
const SettingKeyGatewayRemediation = "gateway_remediation"

const (
	gatewayRemediationTTL       = 30 * time.Second
	gatewayRemediationErrorTTL  = 5 * time.Second
	gatewayRemediationDBTimeout = 2 * time.Second
)

type GatewayRemediationSettings struct {
	Enabled bool `json:"enabled"`
}

type requestObservabilitySnapshot struct {
	enabled   bool
	expiresAt time.Time
}

type requestObservabilityRuntime struct {
	mu         sync.Mutex
	snapshot   atomic.Pointer[requestObservabilitySnapshot]
	refreshing atomic.Bool
}

func (s *SettingService) readGatewayRemediationSettings(ctx context.Context) (GatewayRemediationSettings, error) {
	if s == nil || s.settingRepo == nil {
		return GatewayRemediationSettings{}, nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyGatewayRemediation)
	if errors.Is(err, ErrSettingNotFound) || (err == nil && value == "") {
		return GatewayRemediationSettings{}, nil
	}
	if err != nil {
		return GatewayRemediationSettings{}, fmt.Errorf("get gateway remediation settings: %w", err)
	}
	var settings GatewayRemediationSettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return settings, fmt.Errorf("invalid gateway remediation settings")
	}
	return settings, nil
}

func (s *SettingService) GetGatewayRemediationSettings(ctx context.Context) (GatewayRemediationSettings, error) {
	if s == nil {
		return GatewayRemediationSettings{}, nil
	}
	runtime := &s.requestObservabilityRuntime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	settings, err := s.readGatewayRemediationSettings(ctx)
	if err == nil {
		runtime.snapshot.Store(&requestObservabilitySnapshot{enabled: settings.Enabled, expiresAt: time.Now().Add(gatewayRemediationTTL)})
	}
	return settings, err
}

func (s *SettingService) SetGatewayRemediationSettings(ctx context.Context, settings GatewayRemediationSettings) error {
	if s == nil || s.settingRepo == nil {
		return fmt.Errorf("settings repository unavailable")
	}
	runtime := &s.requestObservabilityRuntime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if err := s.settingRepo.Set(ctx, SettingKeyGatewayRemediation, string(data)); err != nil {
		return err
	}
	runtime.snapshot.Store(&requestObservabilitySnapshot{enabled: settings.Enabled, expiresAt: time.Now().Add(gatewayRemediationTTL)})
	return nil
}

func (s *SettingService) InitializeGatewayRemediation() {
	if s != nil {
		s.refreshGatewayRemediation()
	}
}

func (s *SettingService) refreshGatewayRemediation() {
	runtime := &s.requestObservabilityRuntime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), gatewayRemediationDBTimeout)
	defer cancel()
	settings, err := s.readGatewayRemediationSettings(ctx)
	ttl := gatewayRemediationTTL
	if err != nil {
		ttl = gatewayRemediationErrorTTL
		if previous := runtime.snapshot.Load(); previous != nil {
			settings.Enabled = previous.enabled
		}
	}
	runtime.snapshot.Store(&requestObservabilitySnapshot{enabled: settings.Enabled, expiresAt: time.Now().Add(ttl)})
}

// GatewayRemediationEnabled reads only the atomic snapshot on the request path.
// Expired snapshots refresh asynchronously and never block a request.
func (s *SettingService) GatewayRemediationEnabled() bool {
	if s == nil || s.settingRepo == nil {
		return false
	}
	runtime := &s.requestObservabilityRuntime
	snapshot := runtime.snapshot.Load()
	if (snapshot == nil || time.Now().After(snapshot.expiresAt)) && runtime.refreshing.CompareAndSwap(false, true) {
		go func() {
			defer runtime.refreshing.Store(false)
			s.refreshGatewayRemediation()
		}()
	}
	return snapshot != nil && snapshot.enabled
}
