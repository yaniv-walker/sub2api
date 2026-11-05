import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { useDegradationFeed } from '../useDegradationFeed'
import type { MDRunDetail } from '../types'

const api = vi.hoisted(() => ({
  cancelRun: vi.fn(), fetchRunDetail: vi.fn(), fetchRuns: vi.fn(),
  fetchStats: vi.fn(), fetchTaskBank: vi.fn(), startRun: vi.fn()
}))
vi.mock('../api', () => api)

function detail(overrides: Partial<MDRunDetail> = {}): MDRunDetail {
  return { id: 1, status: 'running', created_at: new Date(Date.now() - 10000).toISOString(),
    duration_ms: 1000, items: [], ...overrides } as MDRunDetail
}

let dispose: () => void
function setup() {
  let feed!: ReturnType<typeof useDegradationFeed>
  const wrapper = mount(defineComponent({ setup() { feed = useDegradationFeed(); return () => null } }))
  dispose = () => wrapper.unmount()
  return feed
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-11-04T08:00:00Z'))
  vi.resetAllMocks()
  api.fetchRuns.mockResolvedValue({ items: [], total: 0 })
  api.fetchStats.mockResolvedValue({ pass_rate: 0, last_duration_ms: 0, run_count: 0 })
})
afterEach(() => { dispose?.(); vi.useRealTimers() })

describe('useDegradationFeed elapsed time', () => {
  it('运行中的轮询不能把计时覆盖回后端的单题耗时', async () => {
    const run = detail()
    api.fetchRunDetail.mockResolvedValue(run)
    const feed = setup()
    await feed.select(1)
    expect(feed.elapsedMs.value).toBe(10000)
    for (let second = 1; second <= 9; second++) {
      await vi.advanceTimersByTimeAsync(1000)
      await flushPromises()
      expect(feed.elapsedMs.value).toBe(10000 + second * 1000)
    }
    api.fetchRunDetail.mockResolvedValue(detail({ status: 'completed', duration_ms: 18500 }))
    await vi.advanceTimersByTimeAsync(3000)
    expect(feed.elapsedMs.value).toBe(18500)
    await vi.advanceTimersByTimeAsync(5000)
    expect(feed.elapsedMs.value).toBe(18500)
  })

  it('切换记录会重新计时，缺失创建时间仍能连续计时', async () => {
    api.fetchRunDetail.mockResolvedValueOnce(detail()).mockResolvedValue(detail({ id: 2, created_at: '', duration_ms: 500 }))
    const feed = setup()
    await feed.select(1)
    await vi.advanceTimersByTimeAsync(2000)
    await feed.select(2)
    expect(feed.elapsedMs.value).toBe(500)
    await vi.advanceTimersByTimeAsync(3000)
    expect(feed.elapsedMs.value).toBe(3500)
  })
})
