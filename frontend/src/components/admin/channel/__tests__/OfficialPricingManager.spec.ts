import { mount, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import OfficialPricingManager from '../OfficialPricingManager.vue'
import { listOfficialPrices, saveOfficialPrice } from '@/api/admin/officialPricing'

vi.mock('vue-i18n', async original => ({
  ...await original<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn() }) }))
vi.mock('@/api/admin/officialPricing', () => ({ listOfficialPrices: vi.fn(), getOfficialPrice: vi.fn(), saveOfficialPrice: vi.fn(), resetOfficialPrice: vi.fn() }))
const entry = { platform: 'openai', model: 'test-model', source: 'manual' as const, override: { input_price: 2e-6 }, automatic: { output_price: 8e-6 }, pricing: { input_price: 2e-6, output_price: 8e-6 } }
async function setup() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
  await router.push('/'); await router.isReady()
  const wrapper = mount(OfficialPricingManager, { global: { plugins: [router], stubs: { IntervalRow: true } } })
  await flushPromises()
  await wrapper.get('tbody button').trigger('click')
  return wrapper
}
beforeEach(() => {
  vi.mocked(listOfficialPrices).mockResolvedValue({ items: [entry], total: 1, limit: 200 })
  vi.mocked(saveOfficialPrice).mockResolvedValue(entry)
})
describe('official price maintenance', () => {
  it('converts million-token input, preserves explicit zero, and sends empty fields as fallback', async () => {
    const wrapper = await setup()
    const fields = wrapper.findAll('form input[type=number]')
    await fields[0]!.setValue('0')
    await fields[1]!.setValue('12')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(saveOfficialPrice).toHaveBeenCalledWith('openai', 'test-model', expect.objectContaining({ input_price: 0, output_price: 12e-6, cache_read_price: null, intervals: null }))
    wrapper.unmount()
  })
  it('retains a dirty draft after rejecting a platform change', async () => {
    const wrapper = await setup()
    await wrapper.findAll('form input[type=number]')[0]!.setValue('7')
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    await wrapper.get('select').setValue('anthropic'); await flushPromises()
    expect((wrapper.get('select').element as HTMLSelectElement).value).toBe('openai')
    expect((wrapper.findAll('form input[type=number]')[0]!.element as HTMLInputElement).value).toBe('7')
    expect(wrapper.find('form').exists()).toBe(true)
    confirm.mockRestore(); wrapper.unmount()
  })
})
