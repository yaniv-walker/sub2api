package service

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCheckinMoneyExact(t *testing.T) {
	for _, amount := range []string{"0.01", "0.00000001", "30.12345678", "-5.00000001"} {
		units, err := CheckinUSDUnits(amount)
		require.NoError(t, err)
		roundtrip, err := CheckinUSDUnits(CheckinUnitsUSD(units))
		require.NoError(t, err)
		require.Equal(t, units, roundtrip)
	}
	for _, amount := range []string{"1e3", "NaN", "0.000000001", "92233720369", "1.2.3", ""} {
		_, err := CheckinUSDUnits(amount)
		require.Error(t, err, amount)
	}
}

func TestCheckinRuleValidation(t *testing.T) {
	cfg := DefaultCheckinConfig()
	require.False(t, cfg.Enabled)
	require.NoError(t, ValidateCheckinConfig(cfg))
	cfg.Rewards[0].Weight++
	require.Error(t, ValidateCheckinConfig(cfg))
	cfg = DefaultCheckinConfig()
	cfg.Timezone = "UTC"
	require.Error(t, ValidateCheckinConfig(cfg))
	cfg = DefaultCheckinConfig()
	cfg.RechargeBonusTiers = []CheckinBonusTier{{Threshold: "30", BonusBPS: 200}, {Threshold: "5", BonusBPS: 100}}
	require.Error(t, ValidateCheckinConfig(cfg))
}

func TestCheckinBonusChangesProbabilitiesNotAmounts(t *testing.T) {
	cfg := DefaultCheckinConfig()
	cfg.Enabled = true
	cfg.DynamicBonusEnabled = true
	cfg.RechargeBonusTiers = []CheckinBonusTier{{Threshold: "5", BonusBPS: 200}}
	cfg.BalanceBonusTiers = []CheckinBonusTier{{Threshold: "10", BonusBPS: 500}}
	cfg.RequestsBonusTiers = []CheckinBonusTier{{Threshold: "100", BonusBPS: 400}}
	in := CheckinInputs{Active: true, Balance: "13.00", PaidCreditUSD: "30", TodayRequests: 243, BudgetRemaining: "100"}
	bonus, weights, err := CheckinEffectiveWeights(cfg, in)
	require.NoError(t, err)
	require.Equal(t, int64(1100), bonus.TotalBPS)
	var total int64
	for _, weight := range weights {
		total += weight
	}
	require.Equal(t, int64(113475000000), weights[0])
	require.Equal(t, int64(150000000000), total)
	draw, err := DrawCheckinReward(cfg, in, bytes.NewReader(make([]byte, 128)))
	require.NoError(t, err)
	require.Equal(t, "0.01", draw.Amount)
	require.Equal(t, bonus.TotalBPS, draw.Bonus.TotalBPS)
}

func TestCheckinClaimEligibilityAndConservativeBudget(t *testing.T) {
	cfg := DefaultCheckinConfig()
	cfg.Enabled = true
	in := CheckinInputs{Active: true, Balance: "-10", PaidCreditUSD: "5", BudgetRemaining: "100"}
	_, err := DrawCheckinReward(cfg, in, bytes.NewReader(make([]byte, 128)))
	require.NoError(t, err, "negative balances can receive rewards")
	in.PaidCreditUSD = "4.99"
	_, err = DrawCheckinReward(cfg, in, bytes.NewReader(make([]byte, 128)))
	require.ErrorIs(t, err, ErrCheckinIneligible)
	in.PaidCreditUSD = "5"
	in.BudgetRemaining = "0.99"
	_, err = DrawCheckinReward(cfg, in, bytes.NewReader(make([]byte, 128)))
	require.ErrorIs(t, err, ErrCheckinBudget, "do not allow retries to reroll when only low tiers fit")
}

func TestCheckinDayUsesActivityTimezone(t *testing.T) {
	date, reset := CheckinBusinessDay(time.Date(2026, 10, 13, 16, 0, 0, 0, time.UTC))
	require.Equal(t, "2026-10-14", date)
	require.Equal(t, "2026-10-14T16:00:00Z", reset.UTC().Format(time.RFC3339))
}
