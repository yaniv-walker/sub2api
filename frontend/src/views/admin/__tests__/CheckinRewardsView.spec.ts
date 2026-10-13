import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CheckinRewardsView from '../CheckinRewardsView.vue'
const mocks = vi.hoisted(() => ({ budgets: vi.fn(), rewards: vi.fn(), cycles: vi.fn(), resolveCycle: vi.fn(), showSuccess: vi.fn() }))
vi.mock('@/api/checkin', () => ({ checkinAPI: mocks }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.showSuccess }) }))
vi.mock('vue-i18n', async () => ({ ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'), useI18n: () => ({ t: (key: string) => key }) }))
const cycle = { id: 10, user_id: 100, state: 'pending_usage', start_date: '2026-10-13', end_date: '2026-10-19', reward_amount: '0.50' }
function render() { return mount(CheckinRewardsView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' }, RewardHistory: true } } }) }
describe('Check-in settlement review', () => {
  beforeEach(() => { vi.clearAllMocks(); mocks.budgets.mockResolvedValue({ items: [{ business_date: '2030-01-01', limit_usd: '100.00', spent_usd: '25.00', reserved_usd: '5.00', remaining_usd: '70.00' }], next_cursor: null }); mocks.rewards.mockResolvedValue({ items: [], next_cursor: 0 }); mocks.cycles.mockResolvedValue({ items: [cycle, { ...cycle, id: 11, state: 'completed' }, { ...cycle, id: 12, state: 'blocked_user' }], next_cursor: 0 }); mocks.resolveCycle.mockResolvedValue({}) })
  it('sends business dates and reward type to audit without affecting cycle filtering', async () => {
    const view = render(); await flushPromises()
    await view.get('[data-testid="date-from"]').setValue('2030-01-01')
    await view.get('[data-testid="date-to"]').setValue('2030-01-31')
    await view.get('[data-testid="reward-type"]').setValue('cycle')
    await view.get('form').trigger('submit'); await flushPromises()
    expect(mocks.rewards).toHaveBeenLastCalledWith({ user_id: undefined, date_from: '2030-01-01', date_to: '2030-01-31', reward_type: 'cycle' }, 0)
    expect(mocks.budgets).toHaveBeenLastCalledWith({ date_from: '2030-01-01', date_to: '2030-01-31' })
    expect(view.text()).toContain('$70.00'); view.unmount()
  })
  it('rejects reversed dates before requesting more records', async () => {
    const view = render(); await flushPromises()
    await view.get('[data-testid="date-from"]').setValue('2030-02-01')
    await view.get('[data-testid="date-to"]').setValue('2030-01-31')
    await view.get('form').trigger('submit'); await flushPromises()
    expect(mocks.rewards).toHaveBeenCalledTimes(1); expect(view.text()).toContain('checkin.invalidDates'); view.unmount()
  })
  it('only offers cancellation for pending usage and never for completed rewards', async () => {
    const view = render(); await flushPromises()
    expect(view.findAll('button').filter(b => b.text() === 'checkin.cancelSettlement')).toHaveLength(1)
    expect(view.findAll('button').filter(b => b.text() === 'checkin.retrySettlement')).toHaveLength(2)
    view.unmount()
  })
  it('requires explicit confirmation and nonblank reason before sending resolution', async () => {
    const view = render(); await flushPromises()
    await view.findAll('button').find(b => b.text() === 'checkin.cancelSettlement')!.trigger('click')
    const confirm = view.findAll('button').find(b => b.text() === 'checkin.confirm')!
    expect(confirm.attributes('disabled')).toBeDefined(); expect(mocks.resolveCycle).not.toHaveBeenCalled()
    await view.get('textarea').setValue('  usage requirements unmet  '); await confirm.trigger('click'); await flushPromises()
    expect(mocks.resolveCycle).toHaveBeenCalledWith(10, 'cancel', 'usage requirements unmet'); expect(mocks.cycles).toHaveBeenCalledTimes(2); view.unmount()
  })
  it('keeps a failed resolution open for review without declaring success', async () => {
    mocks.resolveCycle.mockRejectedValue(new Error('conflict')); const view = render(); await flushPromises()
    await view.findAll('button').find(b => b.text() === 'checkin.retrySettlement')!.trigger('click'); await view.get('textarea').setValue('recheck'); await view.findAll('button').find(b => b.text() === 'checkin.confirm')!.trigger('click'); await flushPromises()
    expect(view.find('[role="dialog"]').exists()).toBe(true); expect(view.text()).toContain('checkin.resolveFailed'); expect(mocks.showSuccess).not.toHaveBeenCalled(); view.unmount()
  })
})
