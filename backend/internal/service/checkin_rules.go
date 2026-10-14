package service

import (
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

const checkinMoneyScale int64 = 100000000

func CheckinUSDUnits(value string) (int64, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return 0, fmt.Errorf("invalid decimal amount")
	}
	negative := strings.HasPrefix(value, "-")
	if negative {
		value = value[1:]
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || len(parts[0]) == 0 {
		return 0, fmt.Errorf("invalid decimal amount")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if len(fraction) == 0 || len(fraction) > 8 {
			return 0, fmt.Errorf("amount must have at most eight decimal places")
		}
	}
	for _, part := range parts {
		for _, char := range part {
			if char < '0' || char > '9' {
				return 0, fmt.Errorf("invalid decimal amount")
			}
		}
	}
	units, err := strconv.ParseInt(parts[0]+fraction+strings.Repeat("0", 8-len(fraction)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount out of range")
	}
	if negative {
		units = -units
	}
	return units, nil
}

func CheckinUnitsUSD(units int64) string {
	sign := ""
	if units < 0 {
		sign = "-"
		units = -units
	}
	fraction := strings.TrimRight(fmt.Sprintf("%08d", units%checkinMoneyScale), "0")
	if len(fraction) < 2 {
		fraction += strings.Repeat("0", 2-len(fraction))
	}
	return fmt.Sprintf("%s%d.%s", sign, units/checkinMoneyScale, fraction)
}

func DefaultCheckinConfig() CheckinConfig {
	return CheckinConfig{Timezone: "Asia/Shanghai", MinimumPaidCreditUSD: "5.00", DailyBudgetUSD: "100.00", Rewards: []CheckinRewardOption{{"0.01", 8500}, {"0.15", 700}, {"0.20", 400}, {"0.30", 200}, {"0.40", 150}, {"1.00", 50}}, MaximumBonusBPS: 2000, CycleDays: 7, RequestsThreshold: 300, CycleRewardUSD: "0.50", RechargeBonusTiers: []CheckinBonusTier{}, BalanceBonusTiers: []CheckinBonusTier{}, RequestsBonusTiers: []CheckinBonusTier{}}
}

func ValidateCheckinConfig(cfg CheckinConfig) error {
	if cfg.Timezone != "Asia/Shanghai" {
		return fmt.Errorf("activity timezone must be Asia/Shanghai")
	}
	if len(cfg.Rewards) < 2 || len(cfg.Rewards) > 24 {
		return fmt.Errorf("reward pool must contain 2 to 24 tiers")
	}
	var sum, previous int64
	for _, reward := range cfg.Rewards {
		amount, err := CheckinUSDUnits(reward.Amount)
		if err != nil || amount <= 0 || amount > 100*checkinMoneyScale || amount <= previous {
			return fmt.Errorf("rewards must be positive, unique and increasing, up to $100")
		}
		if reward.Weight < 0 || reward.Weight > 10000 {
			return fmt.Errorf("invalid reward weight")
		}
		sum += reward.Weight
		previous = amount
	}
	if sum != 10000 {
		return fmt.Errorf("reward weights must total 10000 basis points")
	}
	minimum, err := CheckinUSDUnits(cfg.MinimumPaidCreditUSD)
	if err != nil || minimum < 0 || minimum > 1000000*checkinMoneyScale {
		return fmt.Errorf("invalid minimum paid credit")
	}
	budget, err := CheckinUSDUnits(cfg.DailyBudgetUSD)
	if err != nil || budget < previous || budget > 1000000*checkinMoneyScale {
		return fmt.Errorf("daily budget must cover the maximum reward and be at most $1000000")
	}
	if cfg.MaximumBonusBPS < 0 || cfg.MaximumBonusBPS > 2000 {
		return fmt.Errorf("maximum bonus must be between 0 and 2000 basis points")
	}
	if cfg.DynamicBonusEnabled && (cfg.Rewards[0].Weight == 0 || cfg.Rewards[0].Weight == 10000) {
		return fmt.Errorf("dynamic bonus needs positive low and high tier probabilities")
	}
	for _, tiers := range [][]CheckinBonusTier{cfg.RechargeBonusTiers, cfg.BalanceBonusTiers, cfg.RequestsBonusTiers} {
		if len(tiers) > 20 {
			return fmt.Errorf("at most 20 bonus thresholds are allowed")
		}
		last := int64(-1)
		lastBonus := int64(-1)
		for _, tier := range tiers {
			threshold, err := CheckinUSDUnits(tier.Threshold)
			if err != nil || threshold < 0 || threshold <= last || tier.BonusBPS < 0 || tier.BonusBPS > cfg.MaximumBonusBPS || tier.BonusBPS < lastBonus {
				return fmt.Errorf("bonus thresholds and bonuses must be nonnegative and increasing")
			}
			last = threshold
			lastBonus = tier.BonusBPS
		}
	}
	for _, tier := range cfg.RequestsBonusTiers {
		n, err := strconv.ParseInt(tier.Threshold, 10, 64)
		if err != nil || n < 0 || n > 10000000 {
			return fmt.Errorf("request thresholds must be whole numbers up to 10000000")
		}
	}
	if cfg.CycleDays < 2 || cfg.CycleDays > 31 || cfg.RequestsThreshold < 1 || cfg.RequestsThreshold > 10000000 {
		return fmt.Errorf("invalid cycle length or request threshold")
	}
	cycle, err := CheckinUSDUnits(cfg.CycleRewardUSD)
	if err != nil || cycle <= 0 || cycle > 1000*checkinMoneyScale {
		return fmt.Errorf("cycle reward must be positive and at most $1000")
	}
	return nil
}

func checkinTierBonus(tiers []CheckinBonusTier, units int64) int64 {
	var bonus int64
	for _, tier := range tiers {
		threshold, err := CheckinUSDUnits(tier.Threshold)
		if err == nil && units >= threshold {
			bonus = tier.BonusBPS
		}
	}
	return bonus
}

func CheckinEffectiveWeights(cfg CheckinConfig, in CheckinInputs) (CheckinBonus, []int64, error) {
	if err := ValidateCheckinConfig(cfg); err != nil {
		return CheckinBonus{}, nil, err
	}
	bonus := CheckinBonus{Enabled: cfg.DynamicBonusEnabled}
	weights := make([]int64, len(cfg.Rewards))
	for i, reward := range cfg.Rewards {
		weights[i] = reward.Weight
	}
	if !cfg.DynamicBonusEnabled {
		return bonus, weights, nil
	}
	paid, err := CheckinUSDUnits(in.PaidCreditUSD)
	if err != nil {
		return bonus, nil, err
	}
	balance, err := CheckinUSDUnits(in.Balance)
	if err != nil {
		return bonus, nil, err
	}
	bonus.RechargeBPS = checkinTierBonus(cfg.RechargeBonusTiers, paid)
	bonus.BalanceBPS = checkinTierBonus(cfg.BalanceBonusTiers, balance)
	for _, tier := range cfg.RequestsBonusTiers {
		threshold, err := strconv.ParseInt(tier.Threshold, 10, 64)
		if err == nil && in.TodayRequests >= threshold {
			bonus.RequestsBPS = tier.BonusBPS
		}
	}
	bonus.TotalBPS = min(cfg.MaximumBonusBPS, bonus.RechargeBPS+bonus.BalanceBPS+bonus.RequestsBPS)
	low := weights[0]
	high := int64(10000) - low
	weights[0] = low * (10000 - bonus.TotalBPS) * high
	for i := 1; i < len(weights); i++ {
		weights[i] = cfg.Rewards[i].Weight * (high*10000 + low*bonus.TotalBPS)
	}
	return bonus, weights, nil
}

func DrawCheckinReward(cfg CheckinConfig, in CheckinInputs, random io.Reader) (CheckinDraw, error) {
	if !in.Active {
		return CheckinDraw{}, ErrUserNotFound
	}
	if !cfg.Enabled {
		return CheckinDraw{}, ErrCheckinDisabled
	}
	paid, err := CheckinUSDUnits(in.PaidCreditUSD)
	if err != nil {
		return CheckinDraw{}, err
	}
	minimum, err := CheckinUSDUnits(cfg.MinimumPaidCreditUSD)
	if err != nil {
		return CheckinDraw{}, err
	}
	if paid < minimum {
		return CheckinDraw{}, ErrCheckinIneligible
	}
	bonus, weights, err := CheckinEffectiveWeights(cfg, in)
	if err != nil {
		return CheckinDraw{}, err
	}
	remaining, err := CheckinUSDUnits(in.BudgetRemaining)
	if err != nil {
		return CheckinDraw{}, err
	}
	maximum, err := CheckinUSDUnits(cfg.Rewards[len(cfg.Rewards)-1].Amount)
	if err != nil {
		return CheckinDraw{}, err
	}
	if remaining < maximum {
		return CheckinDraw{}, ErrCheckinBudget
	}
	var total int64
	for _, weight := range weights {
		total += weight
	}
	choice, err := rand.Int(random, big.NewInt(total))
	if err != nil {
		return CheckinDraw{}, fmt.Errorf("draw reward: %w", err)
	}
	value := choice.Int64()
	for i, weight := range weights {
		if value < weight {
			bonus.Locked = true
			return CheckinDraw{Amount: cfg.Rewards[i].Amount, Bonus: bonus, Weights: weights}, nil
		}
		value -= weight
	}
	return CheckinDraw{}, fmt.Errorf("invalid reward weights")
}

func CheckinBusinessDay(now time.Time) (string, time.Time) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	local := now.In(location)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	return start.Format("2006-01-02"), start.AddDate(0, 0, 1)
}
