import { AxiosError, AxiosHeaders } from 'axios'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick } from 'vue'
import { FILTER_DEBOUNCE_MS, useArtifactFeed } from '../useArtifactFeed'
import type { IQArtifactFilters, IQPage, IQPublicArtifact } from '../types'

function item(id: number, overrides: Partial<IQPublicArtifact> = {}): IQPublicArtifact {
  return {
    id,
    case_id: 10,
    case_name: 'Prompt A',
    case_kind: 'text',
    model: 'gpt-4o',
    render_kind: 'text',
    content: 'x',
    status: 'success',
    duration_ms: 100,
    is_unsafe: false,
    created_at: '2026-11-01T00:00:00Z',
    ...overrides,
  }
}

function page(items: IQPublicArtifact[], total = items.length, pageNo = 1): IQPage<IQPublicArtifact> {
  return { items, total, page: pageNo, page_size: 20, pages: Math.max(1, Math.ceil(total / 20)) }
}

function axios429(retryAfter = '30'): AxiosError {
  return new AxiosError('limited', 'ERR', undefined, undefined, {
    status: 429,
    statusText: '',
    data: { code: 'RATE_LIMITED' },
    headers: new AxiosHeaders({ 'retry-after': retryAfter }),
    config: { headers: new AxiosHeaders() },
  })
}

/** 在独立 effectScope 中运行，以便 onScopeDispose 清理能被测试。 */
function mountFeed<T extends { id: number; model: string; case_id: number }>(
  fetcher: (filters: IQArtifactFilters) => Promise<IQPage<T>>,
  options: { immediate?: boolean } = {},
) {
  const scope = effectScope()
  const feed = scope.run(() =>
    useArtifactFeed<T>({ fetcher, pageSize: 20, debounceMs: FILTER_DEBOUNCE_MS, ...options }),
  )!
  return { feed, scope }
}

describe('useArtifactFeed', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('loads immediately by default', async () => {
    const fetcher = vi.fn(async () => page([item(1)]))
    const { feed } = mountFeed(fetcher)

    await vi.runAllTimersAsync()
    expect(fetcher).toHaveBeenCalledTimes(1)
    expect(feed.items.value.length).toBe(1)
    expect(feed.total.value).toBe(1)
    expect(feed.error.value).toBeNull()
  })

  it('skips the initial load when immediate is false', async () => {
    const fetcher = vi.fn(async () => page([item(1)]))
    mountFeed(fetcher, { immediate: false })
    await vi.runAllTimersAsync()
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('debounces filter changes to a single request', async () => {
    const fetcher = vi.fn(async () => page([item(1)]))
    const { feed } = mountFeed(fetcher, { immediate: false })

    // 连续改三次筛选，间隔小于防抖窗口。
    feed.filters.model = 'a'
    await nextTick()
    await vi.advanceTimersByTimeAsync(100)
    feed.filters.model = 'ab'
    await nextTick()
    await vi.advanceTimersByTimeAsync(100)
    feed.filters.model = 'abc'
    await nextTick()

    // 防抖窗口未走完前不应发请求。
    expect(fetcher).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(FILTER_DEBOUNCE_MS)
    expect(fetcher).toHaveBeenCalledTimes(1)
    const lastCall = fetcher.mock.calls.at(-1)![0] as IQArtifactFilters
    expect(lastCall.model).toBe('abc')
  })

  it('debounce window is at least 300ms (rate-limit companion guarantee)', async () => {
    expect(FILTER_DEBOUNCE_MS).toBeGreaterThanOrEqual(300)

    const fetcher = vi.fn(async () => page([item(1)]))
    const { feed } = mountFeed(fetcher, { immediate: false })

    feed.filters.status = 'failed'
    await nextTick()
    // 299ms 时还不够。
    await vi.advanceTimersByTimeAsync(299)
    expect(fetcher).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it('resets to page 1 when filters change', async () => {
    const fetcher = vi.fn(async (f: IQArtifactFilters) => page([item(f.page ?? 1)], 100, f.page ?? 1))
    const { feed } = mountFeed(fetcher)
    await vi.runAllTimersAsync()

    feed.goToPage(3)
    await vi.runAllTimersAsync()
    expect(feed.page.value).toBe(3)

    feed.filters.model = 'gpt-4o'
    await nextTick()
    await vi.advanceTimersByTimeAsync(FILTER_DEBOUNCE_MS)
    expect(feed.page.value).toBe(1)
  })

  it('collapses identical concurrent requests into one call (single-flight)', async () => {
    let release: (value: IQPage<IQPublicArtifact>) => void = () => {}
    const fetcher = vi.fn(
      () =>
        new Promise<IQPage<IQPublicArtifact>>((resolve) => {
          release = resolve
        }),
    )
    const { feed } = mountFeed(fetcher, { immediate: false })

    // 同一页、同一筛选条件：两次立即加载应共享一个请求。
    feed.reloadNow(1)
    feed.reloadNow(1)
    expect(fetcher).toHaveBeenCalledTimes(1)

    release(page([item(1)]))
    await vi.runAllTimersAsync()
    expect(feed.items.value.length).toBe(1)
  })

  it('records a rate-limit cooldown and refuses to load during it', async () => {
    const fetcher = vi.fn(async () => {
      throw axios429('30')
    })
    const { feed } = mountFeed(fetcher)
    await vi.runAllTimersAsync()

    expect(feed.error.value).toBe('rate-limited')
    expect(feed.coolingDown.value).toBe(true)
    expect(feed.cooldownSeconds.value).toBe(30)

    // 冷却期内再点刷新：不再发请求（绝不自动重试）。
    const callsBefore = fetcher.mock.calls.length
    feed.reloadNow(1)
    await vi.runAllTimersAsync()
    expect(fetcher.mock.calls.length).toBe(callsBefore)
  })

  it('resumes loading after the cooldown expires', async () => {
    let shouldFail = true
    const fetcher = vi.fn(async () => {
      if (shouldFail) throw axios429('10')
      return page([item(1)])
    })
    const { feed } = mountFeed(fetcher)
    await vi.runAllTimersAsync()
    expect(feed.coolingDown.value).toBe(true)

    // 冷却结束（Date.now 也要跟着前进，因此用 setSystemTime）。
    shouldFail = false
    vi.setSystemTime(Date.now() + 11_000)
    feed.reloadNow(1)
    await vi.runAllTimersAsync()

    expect(feed.error.value).toBeNull()
    expect(feed.items.value.length).toBe(1)
  })

  it('maps a disabled-feature 503 to the disabled state', async () => {
    const fetcher = vi.fn(async () => {
      throw new AxiosError('off', 'ERR', undefined, undefined, {
        status: 503,
        statusText: '',
        data: { code: 503, reason: 'IQ_BENCHMARK_DISABLED' },
        headers: new AxiosHeaders(),
        config: { headers: new AxiosHeaders() },
      })
    })
    const { feed } = mountFeed(fetcher)
    await vi.runAllTimersAsync()
    expect(feed.error.value).toBe('disabled')
    expect(feed.coolingDown.value).toBe(false)
  })

  it('maps an unexpected error to the generic failure state', async () => {
    const fetcher = vi.fn(async () => {
      throw new Error('boom')
    })
    const { feed } = mountFeed(fetcher)
    await vi.runAllTimersAsync()
    expect(feed.error.value).toBe('failed')
  })

  it('derives model options from the loaded page', async () => {
    const fetcher = vi.fn(async () =>
      page([item(1, { model: 'zeta' }), item(2, { model: 'alpha' }), item(3, { model: 'alpha' })]),
    )
    const { feed } = mountFeed(fetcher)
    await vi.runAllTimersAsync()
    expect(feed.modelOptions.value).toEqual(['alpha', 'zeta'])
  })

  it('tracks default-filter state and resets it', async () => {
    const fetcher = vi.fn(async () => page([item(1)]))
    const { feed } = mountFeed(fetcher)
    await vi.runAllTimersAsync()
    expect(feed.showingDefaultFilters.value).toBe(true)

    feed.filters.model = 'gpt-4o'
    await nextTick()
    expect(feed.showingDefaultFilters.value).toBe(false)

    feed.resetFilters()
    await nextTick()
    expect(feed.showingDefaultFilters.value).toBe(true)
  })

  it('reports hasMore while fewer items than total are loaded', async () => {
    const fetcher = vi.fn(async () => page([item(1), item(2)], 40))
    const { feed } = mountFeed(fetcher)
    await vi.runAllTimersAsync()
    expect(feed.hasMore.value).toBe(true)
  })

  it('cancels a pending debounce on scope dispose', async () => {
    const fetcher = vi.fn(async () => page([item(1)]))
    const { feed, scope } = mountFeed(fetcher, { immediate: false })

    feed.filters.model = 'abc'
    await nextTick()
    scope.stop()
    await vi.advanceTimersByTimeAsync(FILTER_DEBOUNCE_MS * 2)
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('ignores goToPage with a page below 1', async () => {
    const fetcher = vi.fn(async () => page([item(1)]))
    const { feed } = mountFeed(fetcher)
    await vi.runAllTimersAsync()
    const calls = fetcher.mock.calls.length
    feed.goToPage(0)
    await vi.runAllTimersAsync()
    expect(fetcher.mock.calls.length).toBe(calls)
    expect(feed.page.value).toBe(1)
  })
})
