package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type requestObservabilityTestRepo struct {
	panelRateLimitSettingRepo
	failSet   atomic.Bool
	blockRead atomic.Bool
	started   chan struct{}
	release   chan struct{}
}

func (r *requestObservabilityTestRepo) Set(ctx context.Context, key, value string) error {
	if r.failSet.Load() {
		return errors.New("storage unavailable")
	}
	return r.panelRateLimitSettingRepo.Set(ctx, key, value)
}

func (r *requestObservabilityTestRepo) GetValue(ctx context.Context, key string) (string, error) {
	value, err := r.panelRateLimitSettingRepo.GetValue(ctx, key)
	if r.blockRead.CompareAndSwap(true, false) {
		close(r.started)
		select {
		case <-r.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return value, err
}

func TestRequestObservabilitySettingPersistsAndAppliesImmediately(t *testing.T) {
	t.Setenv("SUB2API_REQUEST_OBSERVABILITY", "1") // old environment switch must have no effect
	repo := &requestObservabilityTestRepo{}
	svc := NewSettingService(repo, &config.Config{})
	svc.InitializeRequestObservability()
	require.False(t, svc.RequestObservabilityEnabled())
	for range 100 {
		svc.RequestObservabilityEnabled()
	}
	require.Equal(t, 1, repo.getValueCalls, "request path must use its memory snapshot")
	require.NoError(t, svc.SetRequestObservabilitySettings(t.Context(), RequestObservabilitySettings{Enabled: true}))
	require.True(t, svc.RequestObservabilityEnabled())
	restarted := NewSettingService(repo, &config.Config{})
	restarted.InitializeRequestObservability()
	require.True(t, restarted.RequestObservabilityEnabled())
	repo.failSet.Store(true)
	require.Error(t, svc.SetRequestObservabilitySettings(t.Context(), RequestObservabilitySettings{Enabled: false}))
	require.True(t, svc.RequestObservabilityEnabled(), "failed persistence must not change the live state")
	repo.failSet.Store(false)
	require.NoError(t, svc.SetRequestObservabilitySettings(t.Context(), RequestObservabilitySettings{}))
	require.False(t, svc.RequestObservabilityEnabled())
}

func TestRequestObservabilityRefreshCannotOverwriteNewSave(t *testing.T) {
	repo := &requestObservabilityTestRepo{started: make(chan struct{}), release: make(chan struct{})}
	svc := NewSettingService(repo, &config.Config{})
	svc.InitializeRequestObservability()
	svc.requestObservabilityRuntime.snapshot.Store(&requestObservabilitySnapshot{expiresAt: time.Now().Add(-time.Minute)})
	repo.blockRead.Store(true)
	require.False(t, svc.RequestObservabilityEnabled())
	select {
	case <-repo.started:
	case <-time.After(time.Second):
		t.Fatal("refresh did not start")
	}
	saved := make(chan error, 1)
	go func() {
		saved <- svc.SetRequestObservabilitySettings(t.Context(), RequestObservabilitySettings{Enabled: true})
	}()
	close(repo.release)
	select {
	case err := <-saved:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("save did not finish")
	}
	require.True(t, svc.RequestObservabilityEnabled())
}

func TestRequestObservabilityOtherNodeRefreshesWithoutBlockingRequests(t *testing.T) {
	repo := &requestObservabilityTestRepo{}
	one := NewSettingService(repo, &config.Config{})
	two := NewSettingService(repo, &config.Config{})
	one.InitializeRequestObservability()
	two.InitializeRequestObservability()
	require.NoError(t, one.SetRequestObservabilitySettings(t.Context(), RequestObservabilitySettings{Enabled: true}))
	require.False(t, two.RequestObservabilityEnabled())
	two.requestObservabilityRuntime.snapshot.Store(&requestObservabilitySnapshot{expiresAt: time.Now().Add(-time.Minute)})
	require.Eventually(t, two.RequestObservabilityEnabled, time.Second, time.Millisecond)
}

func TestRequestObservabilityReadFailureKeepsLastKnownState(t *testing.T) {
	repo := &requestObservabilityTestRepo{}
	svc := NewSettingService(repo, &config.Config{})
	require.NoError(t, svc.SetRequestObservabilitySettings(t.Context(), RequestObservabilitySettings{Enabled: true}))
	repo.getValueErr = errors.New("storage unavailable")
	svc.refreshRequestObservability()
	require.True(t, svc.RequestObservabilityEnabled())
	fresh := NewSettingService(repo, &config.Config{})
	fresh.InitializeRequestObservability()
	require.False(t, fresh.RequestObservabilityEnabled())
}
