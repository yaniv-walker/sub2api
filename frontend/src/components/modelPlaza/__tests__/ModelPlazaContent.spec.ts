import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ModelPlazaContent from '../ModelPlazaContent.vue'
import type { ModelPlazaGroup } from '@/api/modelPlaza'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAuthenticated: true }) }))
const group = (id: number, platform: string, name: string, rate: number): ModelPlazaGroup => ({
  id, name: `Group ${id}`, platform, rate_multiplier: rate,
  models: [{ name, platform, pricing: { billing_mode: 'token', input_price: 2e-6, output_price: 10e-6, cache_read_price: null, cache_write_price: null, intervals: [] }, official_pricing: null }]
} as unknown as ModelPlazaGroup)
const groups = [group(1, 'openai', 'model-a', 0.5), group(2, 'openai', 'model-a', 0.2), group(3, 'anthropic', 'model-b', 1)]
const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => wrappers.splice(0).forEach(w => w.unmount()))
async function setup(query = '') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] })
  await router.push(`/${query}`); await router.isReady()
  const wrapper = mount(ModelPlazaContent, { props: { response: { description: '', groups }, loading: false }, global: { plugins: [router], stubs: { Icon: true, PlazaGroupSection: { props: ['group'], template: '<p class="detail-group">{{ group.name }}</p>' }, BaseDialog: { props: ['show', 'title'], template: '<section v-if="show" role="dialog"><h3>{{ title }}</h3><slot /></section>' } } } })
  wrappers.push(wrapper); await flushPromises()
  return { wrapper, router }
}
describe('model square interaction', () => {
  it('restores URL filters, uses selected group price, and disables model group actions', async () => {
    const { wrapper, router } = await setup('?provider=openai&group=1&model=model-a&embedded=1')
    expect(wrapper.findAll('.model-card')).toHaveLength(1)
    expect(wrapper.find('.model-card').text()).toContain('$1')
    expect(wrapper.find('.group-chip').attributes('disabled')).toBeDefined()
    expect(wrapper.find('.detail-button').exists()).toBe(false)
    await wrapper.get('input[type=search]').setValue('not-found'); await flushPromises()
    expect(wrapper.findAll('.model-card')).toHaveLength(0)
    expect(router.currentRoute.value.query.embedded).toBe('1')
  })
  it('opens distinct group and price dialogs, switches views, and clears incompatible group on provider change', async () => {
    const { wrapper } = await setup()
    const card = wrapper.findAll('.model-card').find(c => c.text().includes('model-a'))!
    await card.get('.group-chip').trigger('click')
    expect(wrapper.get('[role=dialog]').text()).toContain('#1')
    expect(wrapper.findAll('.detail-group')).toHaveLength(0)
    await card.get('.detail-button').trigger('click')
    expect(wrapper.findAll('.detail-group')).toHaveLength(2)
    await wrapper.get('select[aria-label="modelPlaza.square.groups"]').setValue('1'); await flushPromises()
    const provider = wrapper.findAll('.catalog-item').find(c => c.text().includes('Anthropic'))!
    await provider.trigger('click'); await flushPromises()
    expect((wrapper.get('select[aria-label="modelPlaza.square.groups"]').element as HTMLSelectElement).value).toBe('')
    expect(wrapper.findAll('.model-card')).toHaveLength(1)
    await wrapper.get('[aria-label="modelPlaza.square.list"]').trigger('click')
    expect(wrapper.find('table').exists()).toBe(true)
  })
  it('copies model ID without copying prices and reports clipboard failure accurately', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    const { wrapper } = await setup('?provider=openai')
    await wrapper.get('.model-card').trigger('keydown.enter'); await flushPromises()
    expect(writeText).toHaveBeenCalledWith('model-a')
    expect(wrapper.get('[role=status]').text()).toContain('modelPlaza.square.copied')
    writeText.mockRejectedValueOnce(new Error('denied'))
    await wrapper.get('.model-card').trigger('click'); await flushPromises()
    expect(wrapper.get('[role=status]').text()).toContain('modelPlaza.square.copyFailed')
  })
})
