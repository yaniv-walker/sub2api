package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestGatewayRemediationCompatibilityAPIsShareOneGate(t *testing.T) {
	repo := &requestObservabilityTestRepo{}
	svc := NewSettingService(repo, &config.Config{})
	svc.InitializeGatewayRemediation()
	require.False(t, svc.GatewayRemediationEnabled())
	require.NoError(t, svc.SetRequestObservabilitySettings(t.Context(), RequestObservabilitySettings{Enabled: true}))
	require.True(t, svc.UpstreamRetryGuardrailsEnabled())
	require.True(t, svc.GatewayRemediationEnabled())
	require.NoError(t, svc.SetUpstreamRetryGuardrailsSettings(t.Context(), UpstreamRetryGuardrailsSettings{Enabled: false}))
	require.False(t, svc.RequestObservabilityEnabled())
	settings, err := svc.GetGatewayRemediationSettings(t.Context())
	require.NoError(t, err)
	require.False(t, settings.Enabled)
	require.Contains(t, repo.values, SettingKeyGatewayRemediation)
	require.NotContains(t, repo.values, SettingKeyRequestObservability)
	require.NotContains(t, repo.values, SettingKeyUpstreamRetryGuardrails)
}

func TestGatewayRemediationLegacyKeysCannotEnableNewBehavior(t *testing.T) {
	repo := &requestObservabilityTestRepo{}
	require.NoError(t, repo.Set(t.Context(), SettingKeyRequestObservability, `{"enabled":true}`))
	require.NoError(t, repo.Set(t.Context(), SettingKeyUpstreamRetryGuardrails, `{"enabled":true}`))
	svc := NewSettingService(repo, &config.Config{})
	svc.InitializeGatewayRemediation()
	require.False(t, svc.GatewayRemediationEnabled())
	// Explicit opt-in prevents a telemetry-only legacy switch from silently
	// enabling behavior-changing guardrails after an upgrade.
}
