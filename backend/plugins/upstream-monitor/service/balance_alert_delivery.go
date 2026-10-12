package service

import (
	"context"
	"fmt"
	hostservice "github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/model"
	"github.com/google/uuid"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Persist the episode before sending; never call SMTP while holding a DB transaction.
func (c *BalanceAlertChecker) evaluate(ctx context.Context, rule *model.BalanceAlertRule, sample BalanceAlertSample, now time.Time, checkpoints ...func() error) error {
	state := &rule.State
	state.LastCheckedAt = &now
	state.LastBalance = &sample.Balance
	state.LastError = ""
	if !rule.IsLow(sample.Balance) {
		if state.EpisodeID != "" {
			state.RecoveryConfirmations++
			if state.RecoveryConfirmations >= 2 {
				state.EpisodeID = ""
				state.Deliveries = nil
				state.RecoveryConfirmations = 0
			}
		}
		return nil
	}
	state.RecoveryConfirmations = 0
	if state.EpisodeID == "" {
		state.EpisodeID = uuid.NewString()
		state.Deliveries = map[string]model.RecipientDelivery{}
	}
	if state.Deliveries == nil {
		state.Deliveries = map[string]model.RecipientDelivery{}
	}
	active := map[string]bool{}
	for _, recipient := range rule.Recipients {
		active[strings.ToLower(recipient)] = true
	}
	for key := range state.Deliveries {
		if !active[key] {
			delete(state.Deliveries, key)
		}
	}
	checkpoint := func() error {
		if len(checkpoints) > 0 {
			return checkpoints[0]()
		}
		return nil
	}
	if err := checkpoint(); err != nil {
		return err
	}
	for _, recipient := range rule.Recipients {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if c.settings != nil && !c.allowed(ctx) {
			return nil
		}
		key := strings.ToLower(recipient)
		delivery := state.Deliveries[key]
		if delivery.NextAttemptAt != nil && now.Before(*delivery.NextAttemptAt) {
			continue
		}
		if delivery.SentAt != nil && (rule.RepeatIntervalSeconds == 0 || now.Before(delivery.SentAt.Add(time.Duration(rule.RepeatIntervalSeconds)*time.Second))) {
			continue
		}
		name := sample.Name
		if name == "" {
			name = safeAlertURL(sample.URL)
		}
		// Fence against an upstream disabled during sampling or a previous delivery.
		if err := checkpoint(); err != nil {
			return err
		}
		err := c.sender.Send(ctx, hostservice.NotificationEmailSendInput{
			Event: hostservice.NotificationEmailEventUpstreamBalanceLow, Locale: rule.Locale, RecipientEmail: recipient,
			SourceType: "upstream_monitor", SourceID: strconv.FormatInt(rule.UpstreamID, 10),
			ReminderKey: fmt.Sprintf("%s:%d", state.EpisodeID, delivery.Sequence+1),
			Variables: map[string]string{"upstream_name": name, "upstream_url": safeAlertURL(sample.URL), "upstream_type": sample.Type,
				"balance": strconv.FormatFloat(sample.Balance, 'f', 2, 64), "threshold": rule.Threshold,
				"checked_at": now.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05 -07:00")},
		})
		if err == nil {
			sentAt := now
			if c.clock != nil {
				sentAt = c.clock().UTC()
			}
			delivery.SentAt = &sentAt
			delivery.NextAttemptAt = nil
			delivery.Failures = 0
			delivery.Sequence++
		} else {
			delivery.Failures++
			minutes := 5 << min(delivery.Failures-1, 4)
			retry := now.Add(time.Duration(min(minutes, 60)) * time.Minute)
			delivery.NextAttemptAt = &retry
		}
		state.Deliveries[key] = delivery
		if err := checkpoint(); err != nil {
			return err
		}
	}
	for _, delivery := range state.Deliveries {
		if delivery.Failures > 0 {
			state.LastError = "email_delivery_failed"
			break
		}
	}
	return nil
}

func safeAlertURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}
