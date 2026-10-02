import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UpstreamRetryGuardrailsSettings from '../UpstreamRetryGuardrailsSettings.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('@/api/admin/upstreamRetryGuardrailsSettings', () => ({
  getUpstreamRetryGuardrailsSettings: api.get,
  updateUpstreamRetryGuardrailsSettings: api.put,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('UpstreamRetryGuardrailsSettings', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.get.mockResolvedValue({ enabled: false })
    api.put.mockImplementation(async (enabled: boolean) => ({ enabled }))
  })

  it('loads disabled state and saves the opt-in switch', async () => {
    const wrapper = mount(UpstreamRetryGuardrailsSettings)
    await flushPromises()
    expect(wrapper.get('[data-testid="upstream-retry-guardrails-enabled"]').attributes('aria-checked')).toBe('false')
    await wrapper.get('[data-testid="upstream-retry-guardrails-enabled"]').trigger('click')
    await wrapper.get('[data-testid="upstream-retry-guardrails-save"]').trigger('click')
    await flushPromises()
    expect(api.put).toHaveBeenCalledWith(true)
    expect(wrapper.get('[data-testid="upstream-retry-guardrails-status"]').text()).toContain('currentEnabled')
  })

  it('keeps the saved state when the update fails', async () => {
    api.put.mockRejectedValueOnce(new Error('storage unavailable'))
    const wrapper = mount(UpstreamRetryGuardrailsSettings)
    await flushPromises()
    await wrapper.get('[data-testid="upstream-retry-guardrails-enabled"]').trigger('click')
    await wrapper.get('[data-testid="upstream-retry-guardrails-save"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="upstream-retry-guardrails-status"]').text()).toContain('currentDisabled')
    expect(wrapper.get('[role="status"]').text()).toContain('saveFailed')
  })
})
