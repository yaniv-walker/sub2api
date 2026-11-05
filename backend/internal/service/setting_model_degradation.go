package service

import (
	"context"
	"errors"
)

// ModelDegradationEnabled preserves the formerly always-on behavior when the
// setting does not yet exist. Storage failures are returned so APIs fail closed.
func (s *SettingService) ModelDegradationEnabled(ctx context.Context) (bool, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyModelDegradationEnabled)
	if errors.Is(err, ErrSettingNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return value != "false", nil
}
