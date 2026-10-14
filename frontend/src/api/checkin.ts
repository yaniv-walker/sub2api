import { apiClient } from './client'

export interface RewardOption { amount: string; weight: number }
export interface BonusTier { threshold: string; bonus_bps: number }
export interface CheckinBonus { enabled: boolean; locked: boolean; recharge_bps: number; balance_bps: number; requests_bps: number; total_bps: number }
export interface CheckinRecord { id: number; user_id: number; business_date: string; reward_type: string; reward_amount: string; balance_after: string; rule_version: number; claimed_at: string; dynamic_bonus: CheckinBonus }
export interface CheckinCycle { id: number; state: string; start_date: string; end_date: string; rule_version: number; reward_amount: string; requests_threshold: number; qualified_days: number; settle_at: string; days: Array<{ date: string; requests: number; checked_in: boolean; met: boolean; future: boolean; finalized: boolean }> }
export interface CheckinAdminCycle extends CheckinCycle { user_id: number }
export interface CheckinStatus { server_time: string; business_date: string; timezone: string; next_reset_at: string; enabled: boolean; eligible: boolean; eligibility_reason?: string; minimum_paid_credit_usd: string; eligible_paid_credit_usd: string; balance: string; checked_in: boolean; can_claim: boolean; state: string; rule_version: number; today_valid_requests: number; reward_options: RewardOption[]; dynamic_bonus: CheckinBonus; record: CheckinRecord | null; cycle: CheckinCycle | null; cycle_enabled: boolean; cycle_days: number; requests_threshold: number; cycle_reward_usd: string }
export interface CheckinConfig { version: number; enabled: boolean; timezone: string; effective_at: string; minimum_paid_credit_usd: string; daily_budget_usd: string; rewards: RewardOption[]; dynamic_bonus_enabled: boolean; maximum_bonus_bps: number; recharge_bonus_tiers: BonusTier[]; balance_bonus_tiers: BonusTier[]; requests_bonus_tiers: BonusTier[]; cycle_enabled: boolean; cycle_days: number; requests_threshold: number; cycle_reward_usd: string }
export interface CheckinRewardFilters { user_id?: number; date_from?: string; date_to?: string; reward_type?: 'daily' | 'cycle' }
export interface CheckinBudget { business_date: string; limit_usd: string; spent_usd: string; reserved_usd: string; remaining_usd: string }
export interface CheckinHistory { items: CheckinRecord[]; next_cursor: number }
export const checkinAPI = {
  status: async () => (await apiClient.get<CheckinStatus>('/checkin/status')).data,
  claim: async () => (await apiClient.post<{ created: boolean; record: CheckinRecord | null; status?: CheckinStatus }>('/checkin/claim', {})).data,
  history: async (cursor = 0, limit = 20) => (await apiClient.get<CheckinHistory>('/checkin/history', { params: { cursor, limit } })).data,
  cycle: async () => (await apiClient.get<CheckinCycle | null>('/checkin/cycle')).data,
  config: async () => (await apiClient.get<CheckinConfig>('/admin/settings/checkin')).data,
  saveConfig: async (config: CheckinConfig, expected_version: number) => (await apiClient.put<CheckinConfig>('/admin/settings/checkin', { expected_version, config })).data,
  cycles: async (user_id?: number, cursor = 0, limit = 20) => (await apiClient.get<{ items: CheckinAdminCycle[]; next_cursor: number }>('/admin/checkin/cycles', { params: { user_id, cursor, limit } })).data,
  resolveCycle: async (id: number, action: 'retry' | 'cancel', reason: string) => (await apiClient.post('/admin/checkin/settlements/' + id + '/resolve', { action, reason })).data,
  rewards: async (filters: CheckinRewardFilters = {}, cursor = 0, limit = 20) => (await apiClient.get<CheckinHistory>('/admin/checkin/rewards', { params: { ...filters, cursor, limit } })).data,
  budgets: async (filters: { date_from?: string; date_to?: string } = {}, limit = 30) => (await apiClient.get<{ items: CheckinBudget[] }>('/admin/checkin/budgets', { params: { ...filters, limit } })).data
}
