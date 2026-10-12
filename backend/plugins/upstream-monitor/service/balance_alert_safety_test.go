package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/model"
	"github.com/stretchr/testify/require"
)

func TestBalanceAlertRestartUsesPersistedRecipientState(t *testing.T) {
	sender := &alertSenderFake{}
	checker := &BalanceAlertChecker{sender: sender}
	rule := model.BalanceAlertRule{BalanceAlertConfig: model.BalanceAlertConfig{Threshold: "0", Recipients: []string{"ops@example.com"}, Locale: "zh"}}
	now := time.Now()
	sample := BalanceAlertSample{Balance: -1}
	require.NoError(t, checker.evaluate(context.Background(), &rule, sample, now))
	encoded, err := json.Marshal(rule)
	require.NoError(t, err)
	var restored model.BalanceAlertRule
	require.NoError(t, json.Unmarshal(encoded, &restored))
	require.NoError(t, checker.evaluate(context.Background(), &restored, sample, now.Add(time.Hour)))
	require.Len(t, sender.calls, 1)
	restored.Recipients = append(restored.Recipients, "new@example.com")
	require.NoError(t, checker.evaluate(context.Background(), &restored, sample, now.Add(2*time.Hour)))
	require.Len(t, sender.calls, 2)
	require.Equal(t, "new@example.com", sender.calls[1].RecipientEmail)
}
func TestBalanceAlertDoesNotSendWithoutDurableEpisode(t *testing.T) {
	sender := &alertSenderFake{}
	checker := &BalanceAlertChecker{sender: sender}
	rule := model.BalanceAlertRule{BalanceAlertConfig: model.BalanceAlertConfig{Threshold: "10", Recipients: []string{"ops@example.com"}}}
	err := checker.evaluate(context.Background(), &rule, BalanceAlertSample{Balance: 0}, time.Now(), func() error { return errors.New("database unavailable") })
	require.Error(t, err)
	require.Empty(t, sender.calls)
	require.Equal(t, "https://example.com", safeAlertURL("https://user:password@example.com?token=secret#fragment"))
	require.Empty(t, safeAlertURL("javascript:alert(1)"))
}

func TestBalanceAlertStopsAfterCheckpointRejectsDisabledUpstream(t *testing.T) {
	sender := &alertSenderFake{}
	checker := &BalanceAlertChecker{sender: sender}
	rule := model.BalanceAlertRule{BalanceAlertConfig: model.BalanceAlertConfig{Threshold: "10", Recipients: []string{"a@example.com", "b@example.com"}, Locale: "zh"}}
	err := checker.evaluate(context.Background(), &rule, BalanceAlertSample{Balance: 0}, time.Now(), func() error {
		if len(sender.calls) > 0 {
			return errors.New("upstream disabled")
		}
		return nil
	})
	require.Error(t, err)
	require.Len(t, sender.calls, 1)
	require.NotNil(t, rule.State.Deliveries["a@example.com"].SentAt)
	_, sent := rule.State.Deliveries["b@example.com"]
	require.False(t, sent)
}
