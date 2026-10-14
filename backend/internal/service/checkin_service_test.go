package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type checkinServiceRepoStub struct {
	CheckinRepository
	config   CheckinConfig
	input    CheckinInputs
	stateErr error
	cycleErr error
	acked    int64
}

func (r *checkinServiceRepoStub) GetConfig(context.Context, time.Time, bool) (*CheckinConfig, error) {
	return &r.config, nil
}
func (r *checkinServiceRepoStub) GetUserState(context.Context, int64, time.Time, CheckinConfig) (*CheckinInputs, error) {
	return &r.input, r.stateErr
}
func (r *checkinServiceRepoStub) Claim(context.Context, int64, time.Time, CheckinDrawFunc) (*CheckinClaimResult, error) {
	return &CheckinClaimResult{Created: true, Record: &CheckinRecord{ID: 7, RewardAmount: "0.15"}}, nil
}
func (r *checkinServiceRepoStub) ProcessCycles(context.Context, time.Time) error { return r.cycleErr }
func (r *checkinServiceRepoStub) PendingInvalidations(context.Context, int) ([]CheckinInvalidation, error) {
	return []CheckinInvalidation{{ID: 9, UserID: 11}}, nil
}
func (r *checkinServiceRepoStub) AckInvalidation(_ context.Context, id int64) error {
	r.acked = id
	return nil
}

type checkinBillingStub struct {
	err   error
	calls int
}

func (b *checkinBillingStub) InvalidateUserBalance(context.Context, int64) error {
	b.calls++
	return b.err
}

func TestCheckinCommittedRewardSurvivesCacheAndStatusFailures(t *testing.T) {
	r := &checkinServiceRepoStub{config: DefaultCheckinConfig(), stateErr: errors.New("read failed")}
	b := &checkinBillingStub{err: errors.New("redis unavailable")}
	s := NewCheckinService(r, nil, nil)
	s.billing = b
	result, err := s.Claim(context.Background(), 11)
	require.NoError(t, err)
	require.True(t, result.Created)
	require.Equal(t, "0.15", result.Record.RewardAmount)
	require.Nil(t, result.Status)
	require.NoError(t, s.Maintain(context.Background()))
	require.Zero(t, r.acked)
	b.err = nil
	require.NoError(t, s.Maintain(context.Background()))
	require.Equal(t, int64(9), r.acked)
}

func TestCheckinCycleFailureDoesNotStarveCacheOutbox(t *testing.T) {
	r := &checkinServiceRepoStub{config: DefaultCheckinConfig(), cycleErr: errors.New("settlement unavailable")}
	s := NewCheckinService(r, nil, nil)
	require.Error(t, s.Maintain(context.Background()))
	require.Equal(t, int64(9), r.acked)
}

func TestCheckinStatusKeepsOriginalRewardAfterClosure(t *testing.T) {
	r := &checkinServiceRepoStub{config: DefaultCheckinConfig(), input: CheckinInputs{Active: true, PaidCreditUSD: "0", Balance: "0.15", BudgetRemaining: "0", Record: &CheckinRecord{ID: 7, RewardAmount: "0.15", Bonus: CheckinBonus{Locked: true, TotalBPS: 200}}}}
	s := NewCheckinService(r, nil, nil)
	status, err := s.Status(context.Background(), 11)
	require.NoError(t, err)
	require.Equal(t, "checked_in", status.State)
	require.False(t, status.CanClaim)
	require.Equal(t, int64(200), status.DynamicBonus.TotalBPS)
}

func TestCheckinRequestBonusCannotOverflow(t *testing.T) {
	cfg := DefaultCheckinConfig()
	cfg.DynamicBonusEnabled = true
	cfg.RequestsBonusTiers = []CheckinBonusTier{{Threshold: "300", BonusBPS: 400}}
	bonus, _, err := CheckinEffectiveWeights(cfg, CheckinInputs{Balance: "0", PaidCreditUSD: "0", TodayRequests: 9223372036854775807})
	require.NoError(t, err)
	require.Equal(t, int64(400), bonus.RequestsBPS)
}
