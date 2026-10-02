package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestUpstreamRetryGuardrailsDefaultsOffAndPersists(t *testing.T) {
	repo := &requestObservabilityTestRepo{}
	svc := NewSettingService(repo, &config.Config{})
	svc.InitializeUpstreamRetryGuardrails()
	require.False(t, svc.UpstreamRetryGuardrailsEnabled())
	require.NoError(t, svc.SetUpstreamRetryGuardrailsSettings(t.Context(), UpstreamRetryGuardrailsSettings{Enabled: true}))
	require.True(t, svc.UpstreamRetryGuardrailsEnabled())

	restarted := NewSettingService(repo, &config.Config{})
	restarted.InitializeUpstreamRetryGuardrails()
	require.True(t, restarted.UpstreamRetryGuardrailsEnabled())
}

func TestUpstreamRetryGuardrailsSaveFailureKeepsLastKnownState(t *testing.T) {
	repo := &requestObservabilityTestRepo{}
	svc := NewSettingService(repo, &config.Config{})
	require.NoError(t, svc.SetUpstreamRetryGuardrailsSettings(t.Context(), UpstreamRetryGuardrailsSettings{Enabled: true}))
	repo.failSet.Store(true)
	require.Error(t, svc.SetUpstreamRetryGuardrailsSettings(t.Context(), UpstreamRetryGuardrailsSettings{Enabled: false}))
	require.True(t, svc.UpstreamRetryGuardrailsEnabled())
}
