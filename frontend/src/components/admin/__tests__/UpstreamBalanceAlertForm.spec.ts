import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UpstreamBalanceAlertForm from '../UpstreamBalanceAlertForm.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('@/api/admin/upstreamMonitor', () => ({ getBalanceAlert: api.get, saveBalanceAlert: api.put }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ locale: { value: 'zh' } }) }))

describe('UpstreamBalanceAlertForm', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.get.mockResolvedValue({ upstream_id: 1, enabled: true, threshold: '100', recipients: ['ops@example.com'], repeat_interval_seconds: 0, locale: 'zh', state: {} })
    api.put.mockImplementation(async (id, config) => ({ upstream_id: id, ...config, state: {} }))
  })
  it('loads and independently saves a zero threshold with deduplicated recipients', async () => {
    const wrapper = mount(UpstreamBalanceAlertForm, { props: { upstreamId: 1, balance: 82 } })
    await flushPromises()
    await wrapper.get('[data-testid="balance-alert-threshold"]').setValue('0')
    await wrapper.get('[data-testid="balance-alert-recipients"]').setValue('ops@example.com\nops@example.com')
    await wrapper.get('[data-testid="balance-alert-save"]').trigger('click')
    await flushPromises()
    expect(api.put).toHaveBeenCalledWith(1, expect.objectContaining({ threshold: '0', recipients: ['ops@example.com'] }))
    expect(wrapper.emitted('dirty')?.at(-1)).toEqual([false])
  })
  it('disables saving after load failure instead of overwriting unknown settings', async () => {
    api.get.mockRejectedValue(new Error('offline'))
    const wrapper = mount(UpstreamBalanceAlertForm, { props: { upstreamId: 1 } })
    await flushPromises()
    expect(wrapper.get('[data-testid="balance-alert-save"]').attributes()).toHaveProperty('disabled')
    expect(wrapper.get('[role="alert"]').text()).toBeTruthy()
  })
  it('rejects invalid recipient without calling the server', async () => {
    const wrapper = mount(UpstreamBalanceAlertForm, { props: { upstreamId: 1 } })
    await flushPromises()
    await wrapper.get('[data-testid="balance-alert-recipients"]').setValue('invalid')
    await wrapper.get('[data-testid="balance-alert-save"]').trigger('click')
    await flushPromises()
    expect(api.put).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBeTruthy()
  })
})
