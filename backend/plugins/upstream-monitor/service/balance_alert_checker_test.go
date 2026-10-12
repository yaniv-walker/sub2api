package service

import (
	"context"
	"errors"
	"testing"
	"time"

	hostservice "github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/model"
	"github.com/stretchr/testify/require"
)

type alertSenderFake struct {
	calls []hostservice.NotificationEmailSendInput
	fail  string
}

func (s *alertSenderFake) Send(_ context.Context, in hostservice.NotificationEmailSendInput) error {
	s.calls = append(s.calls, in)
	if in.RecipientEmail == s.fail {
		return errors.New("SMTP failed")
	}
	return nil
}
func TestBalanceAlertEpisodeRecoveryAndPartialRetry(t *testing.T) {
	sender := &alertSenderFake{fail: "b@example.com"}
	checker := &BalanceAlertChecker{sender: sender}
	rule := model.BalanceAlertRule{UpstreamID: 42, BalanceAlertConfig: model.BalanceAlertConfig{Enabled: true, Threshold: "10", Recipients: []string{"a@example.com", "b@example.com"}, Locale: "zh"}}
	sample := BalanceAlertSample{Name: "test", URL: "https://example.com", Type: "sub2api", Balance: 10}
	now := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	checker.evaluate(context.Background(), &rule, sample, now)
	require.Len(t, sender.calls, 2)
	require.NotEmpty(t, rule.State.EpisodeID)
	require.Contains(t, sender.calls[0].Variables, "upstream_url")
	checker.evaluate(context.Background(), &rule, sample, now.Add(time.Minute))
	require.Len(t, sender.calls, 2)
	sender.fail = ""
	checker.evaluate(context.Background(), &rule, sample, now.Add(5*time.Minute))
	require.Len(t, sender.calls, 3)
	require.Equal(t, "b@example.com", sender.calls[2].RecipientEmail)
	checker.evaluate(context.Background(), &rule, sample, now.Add(10*time.Minute))
	require.Len(t, sender.calls, 3)
	sample.Balance = 11
	checker.evaluate(context.Background(), &rule, sample, now.Add(15*time.Minute))
	require.NotEmpty(t, rule.State.EpisodeID)
	checker.evaluate(context.Background(), &rule, sample, now.Add(20*time.Minute))
	require.Empty(t, rule.State.EpisodeID)
	sample.Balance = 0
	checker.evaluate(context.Background(), &rule, sample, now.Add(25*time.Minute))
	require.Len(t, sender.calls, 5)
	require.NotEqual(t, sender.calls[0].ReminderKey, sender.calls[3].ReminderKey)
}
func TestBalanceAlertRepeatWaits24HoursSinceSuccess(t *testing.T) {
	sender := &alertSenderFake{}
	checker := &BalanceAlertChecker{sender: sender}
	rule := model.BalanceAlertRule{BalanceAlertConfig: model.BalanceAlertConfig{Threshold: "10", Recipients: []string{"a@example.com"}, Locale: "en", RepeatIntervalSeconds: 86400}}
	now := time.Now()
	sample := BalanceAlertSample{Balance: 1}
	checker.evaluate(context.Background(), &rule, sample, now)
	checker.evaluate(context.Background(), &rule, sample, now.Add(23*time.Hour))
	require.Len(t, sender.calls, 1)
	checker.evaluate(context.Background(), &rule, sample, now.Add(24*time.Hour))
	require.Len(t, sender.calls, 2)
	require.NotEqual(t, sender.calls[0].ReminderKey, sender.calls[1].ReminderKey)
}
