import { describe, expect, it } from 'vitest'
import { expectedCost, validateConfig } from './rules'
import type { CheckinConfig } from '@/api/checkin'
const config: CheckinConfig = { version: 1, enabled: false, timezone: 'Asia/Shanghai', effective_at: '', minimum_paid_credit_usd: '5', daily_budget_usd: '100', rewards: [{ amount: '0.01', weight: 8500 }, { amount: '0.15', weight: 700 }, { amount: '0.20', weight: 400 }, { amount: '0.30', weight: 200 }, { amount: '0.40', weight: 150 }, { amount: '1', weight: 50 }], dynamic_bonus_enabled: true, maximum_bonus_bps: 2000, recharge_bonus_tiers: [], balance_bonus_tiers: [], requests_bonus_tiers: [], cycle_enabled: false, cycle_days: 7, requests_threshold: 300, cycle_reward_usd: '0.5' }
describe('Check-in rules validation and estimates', () => {
  it('matches the agreed basic and maximum bonus expected costs', () => { expect(expectedCost(config)).toBeCloseTo(0.044, 6); expect(expectedCost(config, 2000)).toBeCloseTo(0.082533333, 6) })
  it('requires integral total weight and rejects duplicate amounts', () => { expect(validateConfig(config)).toBe(true); expect(validateConfig({ ...config, rewards: [{ amount: '1', weight: 9999 }, { amount: '2', weight: 0 }] })).toBe(false); expect(validateConfig({ ...config, rewards: [{ amount: '1', weight: 5000 }, { amount: '1.0', weight: 5000 }] })).toBe(false) })
  it('rejects unordered thresholds and nonfinite amounts', () => { expect(validateConfig({ ...config, recharge_bonus_tiers: [{ threshold: '10', bonus_bps: 200 }, { threshold: '5', bonus_bps: 300 }] })).toBe(false); expect(validateConfig({ ...config, daily_budget_usd: 'Infinity' })).toBe(false) })
})
