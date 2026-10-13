import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CheckinView from '../CheckinView.vue'
const mocks = vi.hoisted(() => ({ status: vi.fn(), claim: vi.fn(), history: vi.fn(), refreshUser: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn() }))
vi.mock('@/api/checkin', () => ({ checkinAPI: mocks }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ refreshUser: mocks.refreshUser }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.showSuccess, showWarning: mocks.showWarning }) }))
vi.mock('@/utils/featureFlags', () => ({ FeatureFlags: { checkin: {} }, isFeatureFlagEnabled: () => true }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ locale: 'zh', t: (key: string) => key }) }))
const baseStatus = { can_claim: true, enabled: true, checked_in: false, eligible: true, state: 'ready', balance: '5.50', reward_options: [], dynamic_bonus: { enabled: false }, record: null, cycle: null, next_reset_at: '2026-10-14T00:00:00+08:00' }
const record = { reward_amount: '0.15' }
function render() { return mount(CheckinView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, RewardHistory: true, CheckinCycleCard: true } } }) }
describe('Check-in claiming', () => {
  beforeEach(() => { vi.clearAllMocks(); mocks.status.mockResolvedValue(baseStatus); mocks.history.mockResolvedValue({ items: [], next_cursor: 0 }); mocks.refreshUser.mockResolvedValue({}) })
  it('reconciles a timed-out request that actually credited and refreshes balance', async () => {
    mocks.claim.mockRejectedValue(new Error('timeout'))
    const view = render(); await flushPromises()
    mocks.status.mockResolvedValue({ ...baseStatus, checked_in: true, can_claim: false, record })
    await view.get('[data-testid="claim"]').trigger('click'); await flushPromises()
    expect(mocks.claim).toHaveBeenCalledTimes(1); expect(mocks.refreshUser).toHaveBeenCalledTimes(1); expect(mocks.showSuccess).toHaveBeenCalledWith('checkin.already'); expect(view.text()).toContain('+$0.15'); view.unmount()
  })
  it('prevents a second request while the first is pending', async () => {
    let resolve!: (value: unknown) => void
    mocks.claim.mockReturnValue(new Promise(r => { resolve = r }))
    const view = render(); await flushPromises()
    await view.get('[data-testid="claim"]').trigger('click'); await view.get('[data-testid="claim"]').trigger('click')
    expect(mocks.claim).toHaveBeenCalledTimes(1)
    resolve({ created: true, record }); await flushPromises(); view.unmount()
  })
  it('does not misreport a confirmed reward when balance refresh fails', async () => {
    mocks.claim.mockResolvedValue({ created: true, record }); mocks.refreshUser.mockRejectedValue(new Error('offline'))
    const view = render(); await flushPromises(); await view.get('[data-testid="claim"]').trigger('click'); await flushPromises()
    expect(mocks.showSuccess).toHaveBeenCalledWith('checkin.success'); expect(mocks.showWarning).toHaveBeenCalled(); expect(view.text()).not.toContain('checkin.failed'); view.unmount()
  })
  it('keeps today’s server request count visible when cycles are disabled', async () => {
    mocks.status.mockResolvedValue({ ...baseStatus, today_valid_requests: 215, cycle_enabled: false })
    const view = render(); await flushPromises()
    expect(view.get('[data-testid="today-requests"]').text()).toBe('215')
    expect(view.find('checkin-cycle-card-stub').exists()).toBe(false)
    expect(view.get('[data-testid="account-summary"]').findAll('section')).toHaveLength(3)
    view.unmount()
  })
  it('hides balance after two seconds and disposes its timer', async () => {
    vi.useFakeTimers(); const view = render(); await flushPromises(); await view.get('[data-testid="balance"]').trigger('click'); expect(view.text()).toContain('$5.50'); vi.advanceTimersByTime(2000); await view.vm.$nextTick(); expect(view.get('[data-testid="balance"]').text()).toBe('••••••'); view.unmount(); vi.useRealTimers()
  })
  it('locks retry while post-timeout status remains unknown until an explicit refresh', async () => {
    mocks.claim.mockRejectedValue(new Error('timeout')); const view = render(); await flushPromises()
    mocks.status.mockRejectedValue(new Error('offline')); await view.get('[data-testid="claim"]').trigger('click'); await flushPromises()
    expect(view.get('[data-testid="claim"]').attributes('disabled')).toBeDefined(); expect(view.text()).toContain('checkin.uncertain')
    mocks.status.mockResolvedValue(baseStatus); await view.findAll('button').find(b => b.text() === 'checkin.refresh')!.trigger('click'); await flushPromises()
    expect(view.get('[data-testid="claim"]').attributes('disabled')).toBeUndefined(); view.unmount()
  })
  it('refreshes using server reset interval rather than the local wall clock', async () => {
    vi.useFakeTimers(); mocks.status.mockResolvedValue({ ...baseStatus, server_time: '2030-01-01T00:00:00Z', next_reset_at: '2030-01-01T00:00:01Z' })
    const view = render(); await flushPromises(); expect(mocks.status).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(2000); await flushPromises(); expect(mocks.status).toHaveBeenCalledTimes(2)
    view.unmount(); expect(vi.getTimerCount()).toBe(0); vi.useRealTimers()
  })
  it('shows confirmed success without inventing an amount when follow-up data is unavailable', async () => {
    mocks.claim.mockResolvedValue({ created: true, record: null }); const view = render(); await flushPromises()
    mocks.status.mockRejectedValue(new Error('offline')); await view.get('[data-testid="claim"]').trigger('click'); await flushPromises()
    expect(mocks.showSuccess).toHaveBeenCalledWith('checkin.success'); expect(view.get('[data-testid="claim"]').attributes('disabled')).toBeDefined(); expect(view.text()).toContain('checkin.already'); view.unmount()
  })
  it('explicitly presents budget exhaustion after rechecking status', async () => {
    mocks.claim.mockRejectedValue(new Error('budget')); const view = render(); await flushPromises(); mocks.status.mockResolvedValue({ ...baseStatus, state: 'budget_exhausted', can_claim: false }); await view.get('[data-testid="claim"]').trigger('click'); await flushPromises(); expect(view.find('[role="alert"]').text()).toBe('checkin.states.budget_exhausted'); expect(mocks.refreshUser).not.toHaveBeenCalled(); view.unmount()
  })
})
