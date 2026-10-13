package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type checkinMenuRepo struct {
	CheckinRepository
	active, published CheckinConfig
}

func (r *checkinMenuRepo) GetConfig(_ context.Context, _ time.Time, latest bool) (*CheckinConfig, error) {
	if latest {
		return &r.published, nil
	}
	return &r.active, nil
}

func TestCheckinMenuVisibleBeforeScheduledActivation(t *testing.T) {
	r := &checkinMenuRepo{active: DefaultCheckinConfig(), published: DefaultCheckinConfig()}
	r.published.Enabled = true
	r.published.EffectiveAt = time.Now().Add(24 * time.Hour)
	settings := &SettingService{checkinRepo: r}
	require.True(t, settings.CheckinEnabled(context.Background()), "published activity must have a menu even while claims await activation")
	s := NewCheckinService(r, nil, nil)
	cfg, err := s.Config(context.Background(), false)
	require.NoError(t, err)
	require.False(t, cfg.Enabled, "menu visibility must not enable claims early")
	r.published.Enabled = false
	require.False(t, settings.CheckinEnabled(context.Background()))
}
