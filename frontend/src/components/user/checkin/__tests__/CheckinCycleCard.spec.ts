import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CheckinCycleCard from '../CheckinCycleCard.vue'
import zh from '@/i18n/locales/zh/checkin'
import type { CheckinCycle } from '@/api/checkin'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ locale: 'zh', t: (key: string, values: Record<string, unknown> = {}) => {
    const message = key.split('.').reduce<unknown>((value, part) => (value as Record<string, unknown>)[part], zh)
    return String(message).replace(/\{(\w+)\}/g, (_, name: string) => String(values[name]))
  } })
}))

function render(cycle: CheckinCycle | null) {
  return mount(CheckinCycleCard, {
    props: { cycle }
  })
}

const cycle: CheckinCycle = {
  id: 1, state: 'in_progress', start_date: '2026-10-13', end_date: '2026-10-19',
  rule_version: 1, reward_amount: '0.50', requests_threshold: 300, qualified_days: 0,
  settle_at: '2026-10-20T01:00:00+08:00',
  days: Array.from({ length: 7 }, (_, index) => ({
    date: `2026-10-${13 + index}`, requests: index === 0 ? 95 : 0,
    checked_in: index === 0, met: false, future: index > 0, finalized: false
  }))
}

describe('CheckinCycleCard', () => {
  it('shows calendar progress independently of qualified days, like the reference', () => {
    const view = render(cycle)
    expect(view.text()).toContain('本周期 1 / 7 天 · 距离奖励还有 6 天')
    expect(view.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('1')
    expect(view.text()).toContain('10/13')
    expect(view.text()).toContain('95 次')
    expect(view.text().match(/未开始/g)).toHaveLength(6)
    expect(view.text()).toContain('300 次有效请求')
    expect(view.text()).toContain('2026/10/20 01:00:00')
  })

  it('marks a finalized shortfall and keeps the server settlement state', () => {
    const view = render({ ...cycle, state: 'pending_usage', days: cycle.days.map(day => ({ ...day, future: false, finalized: true })) })
    expect(view.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('7')
    expect(view.text()).toContain(zh.checkin.states.pending_usage)
    expect(view.text().match(/未达标/g)).toHaveLength(7)
    expect(view.text()).not.toContain('未开始')
  })

  it('does not invent a cycle before the server creates one', () => {
    const view = render(null)
    expect(view.text()).toContain('暂无进行中的周期')
    expect(view.find('[role="progressbar"]').exists()).toBe(false)
  })
})
