package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type checkinBalanceInvalidator interface {
	InvalidateUserBalance(context.Context, int64) error
}

type CheckinService struct {
	repo            CheckinRepository
	billing         checkinBalanceInvalidator
	auth            APIKeyAuthCacheInvalidator
	now             func() time.Time
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	startOnce       sync.Once
	stopOnce        sync.Once
	onRulesChanged  func()
	observedVersion int64
	observedEnabled bool
}

func NewCheckinService(repo CheckinRepository, billing *BillingCacheService, auth APIKeyAuthCacheInvalidator) *CheckinService {
	s := &CheckinService{repo: repo, auth: auth, now: time.Now}
	if billing != nil {
		s.billing = billing
	}
	return s
}

func (s *CheckinService) Config(ctx context.Context, latest bool) (*CheckinConfig, error) {
	return s.repo.GetConfig(ctx, s.now(), latest)
}

func (s *CheckinService) UpdateConfig(ctx context.Context, cfg CheckinConfig, expected, actor int64) (*CheckinConfig, error) {
	if err := ValidateCheckinConfig(cfg); err != nil {
		return nil, infraerrors.BadRequest("CHECKIN_RULE_INVALID", err.Error())
	}
	if expected < 0 {
		return nil, infraerrors.BadRequest("CHECKIN_RULE_INVALID", "invalid expected version")
	}
	now := s.now()
	if cfg.EffectiveAt.IsZero() {
		_, cfg.EffectiveAt = CheckinBusinessDay(now)
	}
	if cfg.Enabled && cfg.EffectiveAt.Before(now) {
		return nil, infraerrors.BadRequest("CHECKIN_RULE_INVALID", "rule effective time must not be in the past")
	}
	if !cfg.Enabled {
		cfg.EffectiveAt = now
	}
	saved, err := s.repo.SaveConfig(ctx, cfg, expected, actor, now)
	if err == nil && s.onRulesChanged != nil {
		s.onRulesChanged()
	}
	return saved, err
}

func (s *CheckinService) Status(ctx context.Context, userID int64) (*CheckinStatus, error) {
	now := s.now()
	cfg, err := s.repo.GetConfig(ctx, now, false)
	if err != nil {
		return nil, err
	}
	in, err := s.repo.GetUserState(ctx, userID, now, *cfg)
	if err != nil {
		return nil, err
	}
	if !in.Active {
		return nil, ErrUserNotFound
	}
	date, reset := CheckinBusinessDay(now)
	paid, err := CheckinUSDUnits(in.PaidCreditUSD)
	if err != nil {
		return nil, err
	}
	minimum, _ := CheckinUSDUnits(cfg.MinimumPaidCreditUSD)
	bonus, _, err := CheckinEffectiveWeights(*cfg, *in)
	if err != nil {
		return nil, err
	}
	status := &CheckinStatus{ServerTime: now, BusinessDate: date, Timezone: cfg.Timezone, NextResetAt: reset, Enabled: cfg.Enabled, Eligible: paid >= minimum, MinimumPaidCreditUSD: cfg.MinimumPaidCreditUSD, EligiblePaidCreditUSD: in.PaidCreditUSD, Balance: in.Balance, CheckedIn: in.Record != nil, RuleVersion: cfg.Version, TodayValidRequests: in.TodayRequests, RewardOptions: cfg.Rewards, DynamicBonus: bonus, Record: in.Record, Cycle: in.Cycle, CycleEnabled: cfg.CycleEnabled, CycleDays: cfg.CycleDays, RequestsThreshold: cfg.RequestsThreshold, CycleRewardUSD: cfg.CycleRewardUSD, State: "ready"}
	if !status.Eligible {
		status.EligibilityReason = "minimum_paid_credit"
	}
	remaining, err := CheckinUSDUnits(in.BudgetRemaining)
	if err != nil {
		return nil, err
	}
	maximum, _ := CheckinUSDUnits(cfg.Rewards[len(cfg.Rewards)-1].Amount)
	switch {
	case status.CheckedIn:
		status.State = "checked_in"
		status.DynamicBonus = in.Record.Bonus
	case !status.Enabled:
		status.State = "disabled"
	case !status.Eligible:
		status.State = "ineligible"
	case remaining < maximum:
		status.State = "budget_exhausted"
	default:
		status.CanClaim = true
	}
	return status, nil
}

func (s *CheckinService) Claim(ctx context.Context, userID int64) (*CheckinClaimResult, error) {
	result, err := s.repo.Claim(ctx, userID, s.now(), func(cfg CheckinConfig, in CheckinInputs) (CheckinDraw, error) {
		return DrawCheckinReward(cfg, in, rand.Reader)
	})
	if err != nil {
		return nil, err
	}
	if result == nil || result.Record == nil {
		return nil, fmt.Errorf("checkin repository returned no persisted record")
	}
	// A committed reward remains successful if cache refresh or the status read fails.
	if result.Created {
		s.invalidate(ctx, userID)
	}
	status, statusErr := s.Status(ctx, userID)
	if statusErr == nil {
		result.Status = status
	} else {
		slog.Warn("checkin post-commit status refresh failed", "user_id", userID, "error", statusErr)
	}
	return result, nil
}

func (s *CheckinService) History(ctx context.Context, userID, cursor int64, limit int) (*CheckinHistory, error) {
	if userID < 0 || cursor < 0 || limit < 1 || limit > 100 {
		return nil, infraerrors.BadRequest("CHECKIN_QUERY_INVALID", "invalid history pagination")
	}
	return s.repo.History(ctx, userID, cursor, limit)
}

func (s *CheckinService) invalidate(ctx context.Context, userID int64) bool {
	if s.auth != nil {
		s.auth.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billing != nil {
		if err := s.billing.InvalidateUserBalance(ctx, userID); err != nil {
			slog.Warn("checkin balance invalidation will retry", "user_id", userID, "error", err)
			return false
		}
	}
	return true
}

// Maintain retries durable invalidations and due cycle settlements even when new
// claims are disabled. Every repository settlement must be idempotent.
func (s *CheckinService) Maintain(ctx context.Context) error {
	if cfg, err := s.Config(ctx, false); err == nil && (cfg.Version != s.observedVersion || cfg.Enabled != s.observedEnabled) {
		s.observedVersion = cfg.Version
		s.observedEnabled = cfg.Enabled
		if s.onRulesChanged != nil {
			s.onRulesChanged()
		}
	}
	cycleErr := s.repo.ProcessCycles(ctx, s.now())
	events, err := s.repo.PendingInvalidations(ctx, 100)
	if err != nil {
		return err
	}
	for _, event := range events {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if s.invalidate(ctx, event.UserID) {
			if err := s.repo.AckInvalidation(ctx, event.ID); err != nil {
				return err
			}
		}
	}
	return cycleErr
}

func (s *CheckinService) Cycles(ctx context.Context, userID, cursor int64, limit int) (*CheckinCycleHistory, error) {
	if userID < 0 || cursor < 0 || limit < 1 || limit > 100 {
		return nil, infraerrors.BadRequest("CHECKIN_QUERY_INVALID", "invalid cycle pagination")
	}
	return s.repo.ListCycles(ctx, userID, cursor, limit)
}

func (s *CheckinService) ResolveCycle(ctx context.Context, id, actor int64, action, reason string) error {
	reason = strings.TrimSpace(reason)
	if id <= 0 || actor <= 0 || (action != "retry" && action != "cancel") || len([]rune(reason)) < 5 || len([]rune(reason)) > 500 {
		return infraerrors.BadRequest("CHECKIN_RESOLUTION_INVALID", "action must be retry or cancel, with a 5–500 character audit reason")
	}
	return s.repo.ResolveCycle(ctx, id, actor, action, reason, s.now())
}

func (s *CheckinService) Start() {
	s.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				runCtx, cancelRun := context.WithTimeout(ctx, 45*time.Second)
				err := s.Maintain(runCtx)
				cancelRun()
				if err != nil && ctx.Err() == nil {
					slog.Warn("checkin maintenance failed", "error", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

func (s *CheckinService) Stop() {
	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		s.wg.Wait()
	})
}
