import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UpstreamMonitorSettings from '../UpstreamMonitorSettings.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('@/api/admin/upstreamMonitorSettings', () => ({
  getUpstreamMonitorSettings: api.get,
  updateUpstreamMonitorSettings: api.put,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

describe('UpstreamMonitorSettings', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.get.mockResolvedValue({ enabled: true })
    api.put.mockImplementation(async (enabled: boolean) => ({ enabled }))
  })

  it('loads and saves the plugin switch', async () => {
    const wrapper = mount(UpstreamMonitorSettings)
    await flushPromises()
    expect(wrapper.get('[data-testid="upstream-monitor-enabled"]').attributes('aria-checked')).toBe('true')
    await wrapper.get('[data-testid="upstream-monitor-enabled"]').trigger('click')
    await wrapper.get('[data-testid="upstream-monitor-save"]').trigger('click')
    await flushPromises()
    expect(api.put).toHaveBeenCalledWith(false)
    expect(wrapper.get('[data-testid="upstream-monitor-status"]').text()).toContain('currentDisabled')
  })

  it('does not expose an unknown state when loading fails', async () => {
    api.get.mockRejectedValueOnce(new Error('storage unavailable'))
    const wrapper = mount(UpstreamMonitorSettings)
    await flushPromises()
    expect(wrapper.get('[data-testid="upstream-monitor-enabled"]').attributes()).toHaveProperty('disabled')
    expect(wrapper.get('[role="alert"]').text()).toContain('loadFailed')
  })
})
