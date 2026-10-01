import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import RequestObservabilitySettings from '../RequestObservabilitySettings.vue'

const api = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('@/api/admin/requestObservability', () => ({
  getRequestObservabilitySettings: api.get,
  updateRequestObservabilitySettings: api.put,
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const toggle = '[data-testid="request-observability-enabled"]'
const save = '[data-testid="request-observability-save"]'
const status = '[data-testid="request-observability-status"]'

describe('Request observability system setting', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.get.mockResolvedValue({ enabled: false })
    api.put.mockImplementation(async (enabled: boolean) => ({ enabled }))
  })

  it('loads persisted state and independently saves a runtime change', async () => {
    api.get.mockResolvedValue({ enabled: true })
    const wrapper = mount(RequestObservabilitySettings)
    await flushPromises()
    expect(wrapper.get(toggle).attributes('aria-checked')).toBe('true')
    expect(wrapper.get(save).attributes()).toHaveProperty('disabled')
    await wrapper.get(toggle).trigger('click')
    await wrapper.get(save).trigger('click')
    await flushPromises()
    expect(api.put).toHaveBeenCalledWith(false)
    expect(wrapper.get(status).text()).toContain('currentDisabled')
    expect(wrapper.get(save).attributes()).toHaveProperty('disabled')
  })

  it('keeps the saved status on failure and allows a retry', async () => {
    api.put.mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = mount(RequestObservabilitySettings)
    await flushPromises()
    await wrapper.get(toggle).trigger('click')
    await wrapper.get(save).trigger('click')
    await flushPromises()
    expect(wrapper.get(status).text()).toContain('currentDisabled')
    expect(wrapper.get(toggle).attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[role="status"]').text()).toContain('saveFailed')
    await wrapper.get(save).trigger('click')
    await flushPromises()
    expect(wrapper.get(status).text()).toContain('currentEnabled')
    expect(api.put).toHaveBeenCalledTimes(2)
  })

  it('prevents changing an unknown state until loading succeeds', async () => {
    api.get.mockRejectedValueOnce(new Error('load failed'))
    const wrapper = mount(RequestObservabilitySettings)
    await flushPromises()
    expect(wrapper.get(toggle).attributes()).toHaveProperty('disabled')
    expect(wrapper.get('[role="alert"]').text()).toContain('loadFailed')
    await wrapper.get('button.btn').trigger('click')
    await flushPromises()
    expect(wrapper.get(toggle).attributes()).not.toHaveProperty('disabled')
  })

  it('disables controls while saving so one click produces one write', async () => {
    let resolve!: (value: { enabled: boolean }) => void
    api.put.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const wrapper = mount(RequestObservabilitySettings)
    await flushPromises()
    await wrapper.get(toggle).trigger('click')
    await wrapper.get(save).trigger('click')
    expect(wrapper.get(toggle).attributes()).toHaveProperty('disabled')
    expect(wrapper.get(save).attributes()).toHaveProperty('disabled')
    resolve({ enabled: true })
    await flushPromises()
    expect(wrapper.get(status).text()).toContain('currentEnabled')
    expect(api.put).toHaveBeenCalledTimes(1)
  })
})
