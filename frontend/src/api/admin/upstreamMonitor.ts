import { apiClient } from '../client'

export interface MonitorOverview {
  total_upstreams?: number
  total_accounts: number
  total_balance: number
  low_balance_count: number
  error_count_24h: number
  by_type?: Record<string, number>
  by_upstream?: Array<UpstreamSummary & { account_id?: number }>
}

export interface UpstreamSummary {
  id: number
  name: string
  base_url: string
  type?: string
  upstream_type?: string
  enabled?: boolean
  account_count?: number
  balance?: number
  has_access_token?: boolean
  has_personal_access_token?: boolean
  has_passkey?: boolean
  quota_divider?: number
  credential_error?: string
}

export interface MonitorAccount {
  id: number
  name: string
  description?: string | null
  base_url: string
  type: string
  status: string
  balance?: number | null
  concurrency?: number
  last_updated?: string | null
}

export interface AccountsResponse { accounts: MonitorAccount[]; total: number }
export interface UpstreamsResponse { upstreams?: UpstreamSummary[]; total?: number }
export interface RefreshResponse {
  refreshed_count: number
  failed_count: number
  total_balance: number
  refreshed_upstreams?: number
  by_upstream?: Array<UpstreamSummary & { account_id?: number }>
  errors?: Record<string, string>
}

export interface ErrorAnalytics {
  account_id?: number
  upstream_id?: number
  total_errors: number
  error_rate: number
  common_errors: Array<{ message?: string; error?: string; count: number }>
}

export interface UsageAnalytics {
  account_id?: number
  upstream_id?: number
  total_requests: number | null
  total_cost: number | null
  average_daily_cost: number | null
  daily_usage?: Array<{ date: string; requests?: number | null; cost?: number | null }>
}

export interface PredictionAnalytics {
  account_id?: number
  upstream_id?: number
  current_balance: number
  daily_burn_rate: number
  days_remaining?: number | null
  predicted_empty_at?: string | null
}

export interface AnalyticsRange { from: string; to: string; days: number }
export interface UpstreamErrorAnalytics {
  upstream_id: number; range: AnalyticsRange; total_errors: number
  error_rate: number | null; error_rate_unit: 'ratio' | 'unknown'; affected_account_count: number
  by_type: Array<{ type: string; count: number; last_seen: string }>
  daily: Array<{ date: string; count: number }>
  recent: Array<{ account_id: number; error_type: string; error_code: string | null; message: string; occurred_at: string }>
}
export interface UpstreamUsageAnalytics {
  upstream_id: number; range: AnalyticsRange; currency: string
  starting_balance: number | null; ending_balance: number | null
  total_cost: number; average_daily_cost: number; total_requests: number | null
  daily: Array<{ date: string; cost: number; balance: number; request_count: number | null }>
  anomalies: Array<{ date: string; kind: string; amount: number }>
  data_quality: { snapshot_count: number; request_events_available: boolean }
}
export interface UpstreamPredictionAnalytics {
  upstream_id: number; current_balance: number | null; daily_burn_rate: number | null
  estimated_days_left: number | null; estimated_depletion_date: string | null
  confidence: number | null; algorithm: string; data_points: number
}

// apiClient already uses `/api/v1` as its base URL. Keep module paths relative
// to that base so requests become `/api/v1/plugins/upstream-monitor/...`.
const base = '/plugins/upstream-monitor'

export async function getOverview() {
  const { data } = await apiClient.get<MonitorOverview>(`${base}/overview`)
  return data
}

export async function listUpstreams() {
  const { data } = await apiClient.get<UpstreamsResponse | UpstreamSummary[]>(`${base}/upstreams`)
  return Array.isArray(data) ? data : data.upstreams || []
}

export interface UpstreamConfigInput {
  base_url: string
  name?: string
  type: 'sub2api' | 'nexapi'
  enabled?: boolean
  access_token?: string
  personal_access_token?: string
  passkey?: string
  quota_divider?: number
}

export async function configureUpstream(input: UpstreamConfigInput) {
  const { data } = await apiClient.put(`${base}/upstreams`, input)
  return data as UpstreamSummary
}

export async function listAccounts() {
  const { data } = await apiClient.get<AccountsResponse>(`${base}/accounts`)
  return data
}

export async function refreshAll() {
  const { data } = await apiClient.post<RefreshResponse>(`${base}/refresh-all`)
  return data
}

export async function getAccount(id: number) {
  const { data } = await apiClient.get<MonitorAccount>(`${base}/accounts/${id}`)
  return data
}

export async function refreshAccount(id: number) {
  const { data } = await apiClient.post<MonitorAccount>(`${base}/accounts/${id}/refresh`)
  return data
}

export async function getAccountErrors(id: number, days = 30) {
  const { data } = await apiClient.get<ErrorAnalytics>(`${base}/accounts/${id}/errors`, { params: { days } })
  return data
}

export async function getAccountUsage(id: number, days = 30) {
  const { data } = await apiClient.get<UsageAnalytics>(`${base}/accounts/${id}/usage`, { params: { days } })
  return data
}

export async function getAccountPrediction(id: number) {
  const { data } = await apiClient.get<PredictionAnalytics>(`${base}/accounts/${id}/prediction`)
  return data
}

export async function getUpstreamErrors(id: number, days = 30) {
  const { data } = await apiClient.get<UpstreamErrorAnalytics>(`${base}/upstreams/${id}/errors`, { params: { days } })
  return data
}

export async function getUpstreamUsage(id: number, days = 30) {
  const { data } = await apiClient.get<UpstreamUsageAnalytics>(`${base}/upstreams/${id}/usage`, { params: { days } })
  return data
}

export async function getUpstreamPrediction(id: number) {
  const { data } = await apiClient.get<UpstreamPredictionAnalytics>(`${base}/upstreams/${id}/prediction`)
  return data
}

export default {
  getOverview, listUpstreams, listAccounts, refreshAll, getAccount, refreshAccount,
  getAccountErrors, getAccountUsage, getAccountPrediction,
  getUpstreamErrors, getUpstreamUsage, getUpstreamPrediction, configureUpstream
}
