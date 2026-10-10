package service

import "context"

// SettingKeyRequestObservability remains for compatibility with clients that
// know the historical setting name. Runtime behavior uses the unified gate.
const SettingKeyRequestObservability = "request_observability"

type RequestObservabilitySettings = GatewayRemediationSettings

func (s *SettingService) GetRequestObservabilitySettings(ctx context.Context) (RequestObservabilitySettings, error) {
	return s.GetGatewayRemediationSettings(ctx)
}

func (s *SettingService) SetRequestObservabilitySettings(ctx context.Context, settings RequestObservabilitySettings) error {
	return s.SetGatewayRemediationSettings(ctx, settings)
}

func (s *SettingService) InitializeRequestObservability() {
	s.InitializeGatewayRemediation()
}

func (s *SettingService) RequestObservabilityEnabled() bool {
	return s.GatewayRemediationEnabled()
}

func (s *SettingService) refreshRequestObservability() {
	s.refreshGatewayRemediation()
}
