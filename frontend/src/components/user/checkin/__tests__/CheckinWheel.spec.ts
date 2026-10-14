import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CheckinWheel from '../CheckinWheel.vue'
import type { CheckinRecord } from '@/api/checkin'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const options = ['0.01', '0.15', '0.20', '0.30', '0.40', '1.00'].map(amount => ({ amount, weight: 1 }))
const record = { id: 1, reward_amount: '0.30' } as CheckinRecord
beforeEach(() => vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: false }))))
afterEach(() => vi.unstubAllGlobals())

describe('CheckinWheel', () => {
  it('plays an explicitly enabled draw even when the browser requests reduced motion', () => {
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true })))
    const raf = vi.fn(() => 1)
    vi.stubGlobal('requestAnimationFrame', raf)
    vi.stubGlobal('cancelAnimationFrame', vi.fn())
    const view = mount(CheckinWheel, { props: { options, result: null, spinning: true, animate: true } })
    expect(raf).toHaveBeenCalled()
    view.unmount()
  })
  it('trims redundant decimal zeros and distinguishes simulated rewards', () => {
    const view = mount(CheckinWheel, { props: { options, result: { id: 2, reward_amount: '0.01000000' }, preview: true } })
    expect(view.get('[data-testid="wheel-amount"]').text()).toBe('+$0.01')
    expect(view.text()).toContain('checkin.testNotCredited')
    expect(view.text()).not.toContain('checkin.credited')
    view.unmount()
  })
  it('spins while the request is pending, then stops at the server reward', async () => {
    let callback!: FrameRequestCallback
    vi.stubGlobal('requestAnimationFrame', vi.fn((fn: FrameRequestCallback) => { callback = fn; return 1 }))
    const cancel = vi.fn()
    vi.stubGlobal('cancelAnimationFrame', cancel)
    const view = mount(CheckinWheel, { props: { options, result: null, spinning: false } })
    await view.setProps({ spinning: true })
    callback(100); callback(200)
    await view.vm.$nextTick()
    expect(view.get('[data-testid=checkin-wheel]').attributes('style')).toContain('rotate(72deg)')
    await view.setProps({ result: record })
    expect(view.get('[data-testid=checkin-wheel]').attributes('style')).not.toContain('transform 3s')
    await view.setProps({ spinning: false })
    expect(view.get('[data-testid=checkin-wheel]').attributes('data-spin-phase')).toBe('settling')
    callback(300); callback(1800)
    await view.vm.$nextTick()
    expect(view.get('[data-testid=checkin-wheel]').attributes('style')).not.toContain('rotate(72deg)')
    await view.setProps({ options: options.map(option => ({ ...option })) })
    expect(view.get('[data-testid=checkin-wheel]').attributes('data-spin-phase')).toBe('settling')
    callback(3300)
    await view.vm.$nextTick()
    const style = view.get('[data-testid=checkin-wheel]').attributes('style')
    // Fourth sector centre is 210 degrees clockwise from the top.
    // Final rotation 1950 modulo 360 is 150: 210 + 150 = 360.
    expect(style).toContain('rotate(1950deg)')
    expect(view.text()).toContain('+$0.30')
    view.unmount()
    expect(cancel).toHaveBeenCalled()
  })

  it('positions an existing reward without replaying the draw on page load', () => {
    const view = mount(CheckinWheel, { props: { options, result: record } })
    const style = view.get('[data-testid=checkin-wheel]').attributes('style')
    expect(style).toContain('rotate(150deg)')
    expect(style).toContain('transition: none')
    view.unmount()
  })

  it('does not replay a settled reward when refreshed options arrive', async () => {
    const view = mount(CheckinWheel, { props: { options, result: record } })
    await view.setProps({ options: options.map(option => ({ ...option })) })
    expect(view.get('[data-testid=checkin-wheel]').attributes('style')).toContain('transition: none')
    view.unmount()
  })

  it('allows the user to explicitly disable animation while still pointing to the reward', async () => {
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true })))
    const raf = vi.fn()
    vi.stubGlobal('requestAnimationFrame', raf)
    const view = mount(CheckinWheel, { props: { options, result: null, spinning: true, animate: false } })
    await view.setProps({ spinning: false, result: record })
    expect(raf).not.toHaveBeenCalled()
    expect(view.get('[data-testid=checkin-wheel]').attributes('style')).toContain('rotate(150deg)')
    expect(view.get('[data-testid=checkin-wheel]').attributes('style')).toContain('transition: none')
    view.unmount()
  })

  it('stops a failed request without inventing a reward and cancels on unmount', async () => {
    vi.stubGlobal('requestAnimationFrame', vi.fn(() => 42))
    const cancel = vi.fn()
    vi.stubGlobal('cancelAnimationFrame', cancel)
    const view = mount(CheckinWheel, { props: { options, result: null, spinning: true } })
    await view.setProps({ spinning: false })
    expect(cancel).toHaveBeenCalledWith(42)
    expect(view.text()).not.toContain('+$')
    expect(view.get('[data-testid=checkin-wheel]').attributes('style')).toContain('transition: none')
    await view.setProps({ spinning: true })
    view.unmount()
    expect(cancel).toHaveBeenCalledTimes(2)
  })
})
