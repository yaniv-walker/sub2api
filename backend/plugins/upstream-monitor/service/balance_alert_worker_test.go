package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	hostservice "github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/model"
	"github.com/stretchr/testify/require"
)

type alertStoreFake struct {
	rule     *model.BalanceAlertRule
	released bool
}

func (s *alertStoreFake) DueIDs(context.Context) ([]int64, error) { return []int64{1}, nil }
func (s *alertStoreFake) Claim(context.Context, int64, string) (*model.BalanceAlertRule, error) {
	return s.rule, nil
}
func (s *alertStoreFake) StoreState(_ context.Context, _ int64, _ string, state model.BalanceAlertState, release bool) error {
	s.rule.State = state
	s.released = release
	return nil
}

func TestBalanceAlertWorkerSkipsUnavailableSampleAndReleasesLease(t *testing.T) {
	cfg := &config.Config{}
	cfg.Plugins.UpstreamMonitor.Enabled = true
	store := &alertStoreFake{rule: &model.BalanceAlertRule{BalanceAlertConfig: model.BalanceAlertConfig{Enabled: true, Threshold: "100", Recipients: []string{"ops@example.com"}, Locale: "zh"}}}
	sender := &alertSenderFake{}
	checker := NewBalanceAlertChecker(store, nil, sender, hostservice.NewSettingService(nil, cfg))
	checker.checkOne(context.Background(), 1, func(context.Context, int64) (BalanceAlertSample, error) {
		return BalanceAlertSample{}, errors.New("missing quota")
	})
	require.Empty(t, sender.calls)
	require.True(t, store.released)
	require.Equal(t, "balance_unavailable", store.rule.State.LastError)
}

func TestBalanceAlertWorkerDoesNotRunWhenPluginDisabled(t *testing.T) {
	store := &alertStoreFake{}
	checker := NewBalanceAlertChecker(store, nil, nil, hostservice.NewSettingService(nil, &config.Config{}))
	checker.checkOne(context.Background(), 1, func(context.Context, int64) (BalanceAlertSample, error) {
		t.Fatal("should not fetch")
		return BalanceAlertSample{}, nil
	})
	require.False(t, store.released)
}
