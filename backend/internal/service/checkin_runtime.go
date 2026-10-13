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
	// This public flag controls discovery of the activity, not permission to
	// claim. A published, scheduled activity has an entry before it opens;
	// status and claim continue to use the effective rule at the server time.
	cfg, err := s.checkinRepo.GetConfig(ctx, time.Now(), true)
	return err == nil && cfg.Enabled
}
