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

const SettingKeyRequestObservability = "request_observability"

const (
	requestObservabilityTTL       = 30 * time.Second
	requestObservabilityErrorTTL  = 5 * time.Second
	requestObservabilityDBTimeout = 2 * time.Second
)

type RequestObservabilitySettings struct {
	Enabled bool `json:"enabled"`
}

type requestObservabilitySnapshot struct {
	enabled   bool
	expiresAt time.Time
}

// Serialize DB reads/writes and publication so a refresh that started before
// an admin save cannot overwrite the saved local state afterward.
type requestObservabilityRuntime struct {
	mu         sync.Mutex
	snapshot   atomic.Pointer[requestObservabilitySnapshot]
	refreshing atomic.Bool
}

func (s *SettingService) readRequestObservabilitySettings(ctx context.Context) (RequestObservabilitySettings, error) {
	if s == nil || s.settingRepo == nil {
		return RequestObservabilitySettings{}, nil
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyRequestObservability)
	if errors.Is(err, ErrSettingNotFound) || (err == nil && value == "") {
		return RequestObservabilitySettings{}, nil
	}
	if err != nil {
		return RequestObservabilitySettings{}, fmt.Errorf("get request observability settings: %w", err)
	}
	var settings RequestObservabilitySettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return settings, fmt.Errorf("invalid request observability settings")
	}
	return settings, nil
}

func (s *SettingService) GetRequestObservabilitySettings(ctx context.Context) (RequestObservabilitySettings, error) {
	if s == nil {
		return RequestObservabilitySettings{}, nil
	}
	runtime := &s.requestObservabilityRuntime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	settings, err := s.readRequestObservabilitySettings(ctx)
	if err == nil {
		runtime.snapshot.Store(&requestObservabilitySnapshot{enabled: settings.Enabled, expiresAt: time.Now().Add(requestObservabilityTTL)})
	}
	return settings, err
}

func (s *SettingService) SetRequestObservabilitySettings(ctx context.Context, settings RequestObservabilitySettings) error {
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
	if err := s.settingRepo.Set(ctx, SettingKeyRequestObservability, string(data)); err != nil {
		return err
	}
	runtime.snapshot.Store(&requestObservabilitySnapshot{enabled: settings.Enabled, expiresAt: time.Now().Add(requestObservabilityTTL)})
	return nil
}

// InitializeRequestObservability loads the persisted state before serving.
// A storage outage leaves the feature disabled; later requests retry in the
// background without delaying gateway traffic.
func (s *SettingService) InitializeRequestObservability() {
	if s != nil {
		s.refreshRequestObservability()
	}
}

func (s *SettingService) refreshRequestObservability() {
	runtime := &s.requestObservabilityRuntime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), requestObservabilityDBTimeout)
	defer cancel()
	settings, err := s.readRequestObservabilitySettings(ctx)
	ttl := requestObservabilityTTL
	if err != nil {
		ttl = requestObservabilityErrorTTL
		if previous := runtime.snapshot.Load(); previous != nil {
			settings.Enabled = previous.enabled
		}
	}
	runtime.snapshot.Store(&requestObservabilitySnapshot{enabled: settings.Enabled, expiresAt: time.Now().Add(ttl)})
}

// RequestObservabilityEnabled only reads memory on the request path. At most
// one background refresh is active per SettingService instance.
func (s *SettingService) RequestObservabilityEnabled() bool {
	if s == nil || s.settingRepo == nil {
		return false
	}
	runtime := &s.requestObservabilityRuntime
	snapshot := runtime.snapshot.Load()
	if (snapshot == nil || time.Now().After(snapshot.expiresAt)) && runtime.refreshing.CompareAndSwap(false, true) {
		go func() {
			defer runtime.refreshing.Store(false)
			s.refreshRequestObservability()
		}()
	}
	return snapshot != nil && snapshot.enabled
}
