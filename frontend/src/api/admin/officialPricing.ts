import { apiClient } from '../client'
import type { BillingMode } from '@/constants/channel'
import type { UserPricingInterval } from '@/api/channels'

export interface OfficialPrice {
  billing_mode?: BillingMode | null
  input_price?: number | null
  output_price?: number | null
  cache_write_price?: number | null
  cache_write_1h_price?: number | null
  cache_read_price?: number | null
  image_input_price?: number | null
  image_output_price?: number | null
  per_request_price?: number | null
  intervals?: UserPricingInterval[] | null
}
export interface OfficialPriceEntry {
  platform: string
  model: string
  source: 'manual' | 'automatic' | 'missing'
  override: OfficialPrice | null
  automatic: OfficialPrice | null
  pricing: OfficialPrice | null
}
const path = '/admin/channels/pricing/official'
export async function listOfficialPrices(platform: string, search: string, signal?: AbortSignal) {
  const { data } = await apiClient.get<{ items: OfficialPriceEntry[]; total: number; limit: number }>(path, { params: { platform, search, limit: 200 }, signal })
  return data
}
export async function getOfficialPrice(platform: string, model: string) {
  const { data } = await apiClient.get<OfficialPriceEntry>(`${path}/model`, { params: { platform, model } })
  return data
}
export async function saveOfficialPrice(platform: string, model: string, price: OfficialPrice) {
  const { data } = await apiClient.put<OfficialPriceEntry>(`${path}/model`, price, { params: { platform, model } })
  return data
}
export async function resetOfficialPrice(platform: string, model: string) {
  await apiClient.delete(`${path}/model`, { params: { platform, model } })
}
