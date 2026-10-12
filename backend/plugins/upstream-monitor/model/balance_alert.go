package model

import (
	"fmt"
	"math"
	"math/big"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var alertThresholdPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,13})(?:\.[0-9]{1,6})?$`)

type BalanceAlertConfig struct {
	Enabled               bool     `json:"enabled"`
	Threshold             string   `json:"threshold"`
	Recipients            []string `json:"recipients"`
	RepeatIntervalSeconds int      `json:"repeat_interval_seconds"`
	Locale                string   `json:"locale"`
}

func DefaultBalanceAlertConfig() BalanceAlertConfig {
	return BalanceAlertConfig{Threshold: "10", Recipients: []string{}, Locale: "zh"}
}

func (c *BalanceAlertConfig) Validate() error {
	c.Threshold = strings.TrimSpace(c.Threshold)
	if !alertThresholdPattern.MatchString(c.Threshold) {
		return fmt.Errorf("threshold must be a non-negative decimal with at most 14 integer and 6 fractional digits")
	}
	if strings.Contains(c.Threshold, ".") {
		c.Threshold = strings.TrimRight(strings.TrimRight(c.Threshold, "0"), ".")
	}
	if c.RepeatIntervalSeconds != 0 && c.RepeatIntervalSeconds != 86400 {
		return fmt.Errorf("repeat_interval_seconds must be 0 or 86400")
	}
	if c.Locale != "zh" && c.Locale != "en" {
		return fmt.Errorf("locale must be zh or en")
	}
	if len(c.Recipients) > 10 {
		return fmt.Errorf("at most 10 recipients are allowed")
	}
	recipients := []string{}
	seen := map[string]bool{}
	for _, raw := range c.Recipients {
		value := strings.TrimSpace(raw)
		addr, err := mail.ParseAddress(value)
		if err != nil || addr.Address != value || strings.ContainsAny(value, "\r\n") || len(value) > 254 {
			return fmt.Errorf("invalid recipient email")
		}
		key := strings.ToLower(value)
		if !seen[key] {
			recipients = append(recipients, value)
			seen[key] = true
		}
	}
	if c.Enabled && len(recipients) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}
	c.Recipients = recipients
	return nil
}

func (c BalanceAlertConfig) IsLow(balance float64) bool {
	if math.IsNaN(balance) || math.IsInf(balance, 0) {
		return false
	}
	threshold, ok := new(big.Rat).SetString(c.Threshold)
	if !ok {
		return false
	}
	value, ok := new(big.Rat).SetString(strconv.FormatFloat(balance, 'f', -1, 64))
	return ok && value.Cmp(threshold) <= 0
}

type RecipientDelivery struct {
	SentAt        *time.Time `json:"sent_at,omitempty"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	Failures      int        `json:"failures"`
	Sequence      int        `json:"sequence"`
}

type BalanceAlertState struct {
	EpisodeID             string                       `json:"episode_id,omitempty"`
	RecoveryConfirmations int                          `json:"recovery_confirmations"`
	LastCheckedAt         *time.Time                   `json:"last_checked_at,omitempty"`
	LastBalance           *float64                     `json:"last_balance,omitempty"`
	LastError             string                       `json:"last_error,omitempty"`
	Deliveries            map[string]RecipientDelivery `json:"deliveries,omitempty"`
}

type BalanceAlertRule struct {
	UpstreamID int64 `json:"upstream_id"`
	BalanceAlertConfig
	State BalanceAlertState `json:"state"`
}
