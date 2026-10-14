import type { CheckinConfig } from '@/api/checkin'

// Preview only: the server uses fixed-point decimal arithmetic for actual awards.
export function expectedCost(config: CheckinConfig, bonusBPS = 0): number {
  const options = [...config.rewards].sort((a, b) => Number(a.amount) - Number(b.amount))
  if (!options.length) return 0
  const base = options.reduce((total, reward) => total + Number(reward.amount) * reward.weight / 10000, 0)
  const low = options[0]!, highWeight = 10000 - low.weight
  if (!highWeight || !bonusBPS) return base
  const highMean = options.slice(1).reduce((total, reward) => total + Number(reward.amount) * reward.weight, 0) / highWeight
  return base + low.weight / 10000 * bonusBPS / 10000 * (highMean - Number(low.amount))
}

export function validateConfig(config: CheckinConfig): boolean {
  const money = (value: string) => /^\d+(\.\d{1,8})?$/.test(value) && Number.isFinite(Number(value))
  if (config.timezone !== 'Asia/Shanghai' || config.rewards.length < 2 || config.rewards.length > 24) return false
  if (!config.rewards.every((r, i) => money(r.amount) && Number(r.amount) > 0 && Number(r.amount) <= 100 && Number.isInteger(r.weight) && r.weight >= 0 && r.weight <= 10000 && (i === 0 || Number(r.amount) > Number(config.rewards[i - 1]!.amount)))) return false
  if (config.rewards.reduce((sum, r) => sum + r.weight, 0) !== 10000) return false
  if (!money(config.minimum_paid_credit_usd) || Number(config.minimum_paid_credit_usd) > 1000000) return false
  if (!money(config.daily_budget_usd) || Number(config.daily_budget_usd) < Number(config.rewards.at(-1)!.amount) || Number(config.daily_budget_usd) > 1000000) return false
  if (!Number.isInteger(config.maximum_bonus_bps) || config.maximum_bonus_bps < 0 || config.maximum_bonus_bps > 2000) return false
  if (config.dynamic_bonus_enabled && (config.rewards[0]!.weight === 0 || config.rewards[0]!.weight === 10000)) return false
  if (![config.recharge_bonus_tiers, config.balance_bonus_tiers, config.requests_bonus_tiers].every(tiers => tiers.length <= 20 && tiers.every((tier, i) => money(tier.threshold) && Number.isInteger(tier.bonus_bps) && tier.bonus_bps >= 0 && tier.bonus_bps <= config.maximum_bonus_bps && (i === 0 || (Number(tier.threshold) > Number(tiers[i - 1]!.threshold) && tier.bonus_bps >= tiers[i - 1]!.bonus_bps))))) return false
  if (!config.requests_bonus_tiers.every(tier => /^\d+$/.test(tier.threshold) && Number(tier.threshold) <= 10000000)) return false
  if (!Number.isInteger(config.cycle_days) || config.cycle_days < 2 || config.cycle_days > 31 || !Number.isInteger(config.requests_threshold) || config.requests_threshold < 1 || config.requests_threshold > 10000000) return false
  return money(config.cycle_reward_usd) && Number(config.cycle_reward_usd) > 0 && Number(config.cycle_reward_usd) <= 1000
}

export function nextBeijingDay(): string {
  const day = new Date(Date.now() + 8 * 3600000 + 86400000).toISOString().slice(0, 10)
  return `${day}T00:00`
}
