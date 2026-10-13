package service

import (
	"context"
	"time"
)

func ProvideCheckinService(repo CheckinRepository, billing *BillingCacheService, auth APIKeyAuthCacheInvalidator, settings *SettingService) *CheckinService {
	s := NewCheckinService(repo, billing, auth)
	settings.checkinRepo = repo
	s.onRulesChanged = func() {
		if settings.onUpdate != nil {
			settings.onUpdate()
		}
	}
	s.Start()
	return s
}

func (s *SettingService) CheckinEnabled(ctx context.Context) bool {
	if s == nil || s.checkinRepo == nil {
		return false
	}
	cfg, err := s.checkinRepo.GetConfig(ctx, time.Now(), false)
	return err == nil && cfg.Enabled
}
