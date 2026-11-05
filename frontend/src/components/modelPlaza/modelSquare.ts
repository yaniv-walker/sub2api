import type { ModelPlazaGroup, PlazaModel } from '@/api/modelPlaza'
import { resolveIntervalPrices } from '@/utils/pricing'

export interface ModelOffer { group: ModelPlazaGroup; model: PlazaModel }
export interface SquareModel { key: string; name: string; platform: string; offers: ModelOffer[] }
export interface PriceBox { key: string; value: number | null; reference: number | null; perToken: boolean }

export function effectiveRate(group: ModelPlazaGroup): number {
  return group.user_rate_multiplier ?? group.rate_multiplier
}

export function aggregateModels(groups: ModelPlazaGroup[]): SquareModel[] {
  const models = new Map<string, SquareModel>()
  for (const group of groups) for (const model of group.models) {
    const key = JSON.stringify([model.platform, model.name])
    let entry = models.get(key)
    if (!entry) {
      entry = { key, name: model.name, platform: model.platform, offers: [] }
      models.set(key, entry)
    }
    if (!entry.offers.some(offer => offer.group.id === group.id)) entry.offers.push({ group, model })
  }
  return [...models.values()]
}

export function modelPrices(offer: ModelOffer): PriceBox[] {
  const { group, model } = offer
  const pricing = model.pricing
  const mode = pricing?.billing_mode ?? 'token'
  let rate = effectiveRate(group)
  if (mode === 'image' && group.image_rate_independent) rate = group.image_rate_multiplier ?? 1
  if (mode === 'video' && group.video_rate_independent) rate = group.video_rate_multiplier ?? 1
  const tiers = [...pricing?.intervals ?? []].sort((a, b) => a.min_tokens - b.min_tokens)
  const firstTier = tiers[0]
  const basis = firstTier && pricing ? resolveIntervalPrices(firstTier, pricing) : pricing
  if (mode !== 'token') return [{ key: mode === 'image' ? 'perImage' : 'perRequest', value: (basis?.per_request_price ?? pricing?.per_request_price) == null ? null : (basis?.per_request_price ?? pricing!.per_request_price)! * rate, reference: null, perToken: false }]
  return (['input_price', 'output_price', 'cache_read_price', 'cache_write_price'] as const).map(key => {
    const value = basis?.[key] ?? pricing?.[key]
    return { key, value: value == null ? null : value * rate * 1e6, reference: model.official_pricing?.[key] == null ? null : model.official_pricing[key]! * 1e6, perToken: true }
  })
}

export function selectOffer(model: SquareModel, groupId = ''): ModelOffer | undefined {
  if (groupId) return model.offers.find(offer => String(offer.group.id) === groupId)
  return [...model.offers].sort((a, b) => {
    const pa = modelPrices(a)[0]?.value ?? Infinity
    const pb = modelPrices(b)[0]?.value ?? Infinity
    return pa - pb || effectiveRate(a.group) - effectiveRate(b.group) || a.group.id - b.group.id
  })[0]
}
