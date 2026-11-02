import { AxiosError, AxiosHeaders } from 'axios'
import { describe, expect, it } from 'vitest'
import {
  SingleFlight,
  describeRetryAfter,
  isInCooldown,
  parseRateLimitError,
  remainingCooldownSeconds,
} from '../rateLimit'

/** 构造一个真实的 AxiosError，以便命中 `instanceof` 判定。 */
function axiosErrorWithResponse(
  status: number,
  data: unknown = undefined,
  headers: Record<string, string> = {},
): AxiosError {
  return new AxiosError('request failed', 'ERR_BAD_REQUEST', undefined, undefined, {
    status,
    statusText: '',
    data,
    headers: new AxiosHeaders(headers),
    config: { headers: new AxiosHeaders() },
  })
}

describe('parseRateLimitError', () => {
  it('detects 429 and reads the Retry-After header', () => {
    const info = parseRateLimitError(axiosErrorWithResponse(429, { code: 'RATE_LIMITED' }, { 'retry-after': '30' }))
    expect(info.limited).toBe(true)
    expect(info.retryAfterSeconds).toBe(30)
    expect(info.featureDisabled).toBe(false)
  })

  it('detects the disabled-feature reason on 503', () => {
    const info = parseRateLimitError(
      axiosErrorWithResponse(503, { code: 503, reason: 'IQ_BENCHMARK_DISABLED' }),
    )
    expect(info.featureDisabled).toBe(true)
    expect(info.limited).toBe(false)
  })

  it('does not treat an unrelated 503 as a disabled feature', () => {
    const info = parseRateLimitError(axiosErrorWithResponse(503, { reason: 'SOMETHING_ELSE' }))
    expect(info.featureDisabled).toBe(false)
    expect(info.limited).toBe(false)
  })

  it('never treats unknown errors as rate limited', () => {
    expect(parseRateLimitError(new Error('boom')).limited).toBe(false)
    expect(parseRateLimitError(undefined).limited).toBe(false)
    expect(parseRateLimitError(null).limited).toBe(false)
    // 没有 response 的 AxiosError（网络错误）也不算限流。
    expect(parseRateLimitError(new AxiosError('network')).limited).toBe(false)
  })

  it('falls back to 60 seconds when Retry-After is missing or unparseable', () => {
    expect(parseRateLimitError(axiosErrorWithResponse(429)).retryAfterSeconds).toBe(60)
    expect(parseRateLimitError(axiosErrorWithResponse(429, undefined, { 'retry-after': 'soon' })).retryAfterSeconds).toBe(60)
    expect(parseRateLimitError(axiosErrorWithResponse(429, undefined, { 'retry-after': '-5' })).retryAfterSeconds).toBe(60)
    expect(parseRateLimitError(axiosErrorWithResponse(429, undefined, { 'retry-after': '0' })).retryAfterSeconds).toBe(60)
  })

  it('ignores non-429 statuses', () => {
    expect(parseRateLimitError(axiosErrorWithResponse(500)).limited).toBe(false)
    expect(parseRateLimitError(axiosErrorWithResponse(404)).limited).toBe(false)
  })
})

describe('describeRetryAfter', () => {
  it('returns seconds and rounded-up minutes', () => {
    expect(describeRetryAfter(5)).toEqual({ seconds: 5, minutes: 1 })
    expect(describeRetryAfter(61)).toEqual({ seconds: 61, minutes: 2 })
    expect(describeRetryAfter(120)).toEqual({ seconds: 120, minutes: 2 })
  })

  it('falls back to 60 seconds for non-positive or invalid input', () => {
    expect(describeRetryAfter(0).seconds).toBe(60)
    expect(describeRetryAfter(-1).seconds).toBe(60)
    expect(describeRetryAfter(Number.NaN).seconds).toBe(60)
  })
})

describe('cooldown helpers', () => {
  it('is in cooldown until the window elapses', () => {
    expect(isInCooldown(null, 60, 0)).toBe(false)
    expect(isInCooldown(1_000, 60, 2_000)).toBe(true)
    // 刚好到点即解除冷却。
    expect(isInCooldown(0, 60, 60_000)).toBe(false)
    expect(isInCooldown(0, 60, 60_001)).toBe(false)
  })

  it('computes remaining seconds and never goes negative', () => {
    expect(remainingCooldownSeconds(null, 60, 0)).toBe(0)
    expect(remainingCooldownSeconds(0, 60, 0)).toBe(60)
    expect(remainingCooldownSeconds(0, 60, 30_000)).toBe(30)
    expect(remainingCooldownSeconds(0, 60, 90_000)).toBe(0)
  })
})

describe('SingleFlight', () => {
  it('shares one in-flight promise per key', async () => {
    const flight = new SingleFlight()
    let calls = 0
    let release: (value: string) => void = () => {}
    const task = () =>
      new Promise<string>((resolve) => {
        calls += 1
        release = resolve
      })

    const first = flight.run('list', task)
    const second = flight.run('list', task)
    expect(calls).toBe(1)

    release('done')
    await expect(first).resolves.toBe('done')
    await expect(second).resolves.toBe('done')
  })

  it('keeps separate keys independent', async () => {
    const flight = new SingleFlight()
    let calls = 0
    const task = async () => {
      calls += 1
      return calls
    }
    await Promise.all([flight.run('a', task), flight.run('b', task)])
    expect(calls).toBe(2)
  })

  it('releases the key after success so a later call refetches', async () => {
    const flight = new SingleFlight()
    let calls = 0
    const task = async () => {
      calls += 1
      return calls
    }
    await flight.run('k', task)
    expect(flight.size).toBe(0)
    await flight.run('k', task)
    expect(calls).toBe(2)
  })

  it('releases the key when the task rejects', async () => {
    const flight = new SingleFlight()
    await expect(
      flight.run('k', async () => {
        throw new Error('nope')
      }),
    ).rejects.toThrow('nope')
    expect(flight.size).toBe(0)
  })

  it('clear() drops every tracked request', async () => {
    const flight = new SingleFlight()
    let release: (value: number) => void = () => {}
    const pending = flight.run(
      'k',
      () => new Promise<number>((resolve) => {
        release = resolve
      }),
    )
    expect(flight.size).toBe(1)
    flight.clear()
    expect(flight.size).toBe(0)
    release(1)
    await expect(pending).resolves.toBe(1)
  })
})
