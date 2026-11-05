import { describe, expect, it } from 'vitest'
import type { ModelPlazaGroup } from '@/api/modelPlaza'
import { aggregateModels, modelPrices, selectOffer } from '../modelSquare'

function group(id: number, rate: number, input: number, platform = 'openai'): ModelPlazaGroup {
  return { id, name: `group-${id}`, platform, rate_multiplier: rate,
    models: [{ name: 'same-id', platform, pricing: { billing_mode: 'token', input_price: input, output_price: input * 5, cache_write_price: null, cache_read_price: 0, intervals: [] }, official_pricing: null }]
  } as unknown as ModelPlazaGroup
}

describe('model square offers', () => {
  it('deduplicates within a platform while retaining each group price and isolating platforms', () => {
    const models = aggregateModels([group(1, 0.5, 8e-6), group(2, 1, 2e-6), group(3, 1, 1e-6, 'anthropic')])
    expect(models).toHaveLength(2)
    expect(models[0]!.offers).toHaveLength(2)
    expect(selectOffer(models[0]!)!.group.id).toBe(2)
    expect(selectOffer(models[0]!, '1')!.group.id).toBe(1)
    expect(selectOffer(models[0]!, '99')).toBeUndefined()
  })
  it('preserves zero prices and user rate instead of falling back to group rate', () => {
    const g = group(1, 0.5, 2e-6)
    g.user_rate_multiplier = 0
    const prices = modelPrices(aggregateModels([g])[0]!.offers[0]!)
    expect(prices[0]!.value).toBe(0)
    expect(prices[2]!.value).toBe(0)
    expect(prices[3]!.value).toBeNull()
  })
  it('uses image and video independent rates without applying token conversion', () => {
    for (const mode of ['image', 'video'] as const) {
      const g = group(1, 0.5, 2e-6)
      g.user_rate_multiplier = 0.2
      g.image_rate_independent = true; g.image_rate_multiplier = 0.3
      g.video_rate_independent = true; g.video_rate_multiplier = 0.4
      g.models[0]!.pricing!.billing_mode = mode
      g.models[0]!.pricing!.per_request_price = 2
      expect(modelPrices(aggregateModels([g])[0]!.offers[0]!)[0]!.value).toBe(mode === 'image' ? 0.6 : 0.8)
    }
  })
})
