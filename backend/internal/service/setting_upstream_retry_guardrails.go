package service

import "context"

// SettingKeyUpstreamRetryGuardrails remains for source/API compatibility. The
// runtime gate is shared with request observability.
const SettingKeyUpstreamRetryGuardrails = "upstream_retry_guardrails"

type UpstreamRetryGuardrailsSettings = GatewayRemediationSettings

func (s *SettingService) GetUpstreamRetryGuardrailsSettings(ctx context.Context) (UpstreamRetryGuardrailsSettings, error) {
	return s.GetGatewayRemediationSettings(ctx)
}

func (s *SettingService) SetUpstreamRetryGuardrailsSettings(ctx context.Context, settings UpstreamRetryGuardrailsSettings) error {
	return s.SetGatewayRemediationSettings(ctx, settings)
}

func (s *SettingService) InitializeUpstreamRetryGuardrails() {
	s.InitializeGatewayRemediation()
}

func (s *SettingService) UpstreamRetryGuardrailsEnabled() bool {
	return s.GatewayRemediationEnabled()
}
