import { AxiosError, AxiosHeaders } from 'axios'
import { describe, expect, it } from 'vitest'
import { isRetryableUserTestError, reasonOf, userTestErrorKey, userTestErrorKeyOf } from '../userTestError'
import zh from '@/i18n/locales/zh/iqBenchmark'
import en from '@/i18n/locales/en/iqBenchmark'

/** 构造一个真实的 AxiosError，以便命中 `instanceof` 判定。 */
function axiosError(status: number, data?: unknown): AxiosError {
  return new AxiosError('request failed', 'ERR_BAD_REQUEST', undefined, undefined, {
    status,
    statusText: '',
    data,
    headers: new AxiosHeaders(),
    config: { headers: new AxiosHeaders() },
  })
}

/**
 * 后端 statusOfUserTestError 会返回的全部 reason。
 *
 * 这份清单必须与 backend/internal/iqbenchmark/handler/user_test_handler.go
 * 手工保持同步 —— 它是「新增哨兵但忘了加前端文案」的唯一护栏。
 */
const BACKEND_REASONS = [
  'USER_TEST_DISABLED',
  'CASE_NOT_OPEN_FOR_USER_TEST',
  'USER_NOT_ACTIVE',
  'NOT_FOUND',
  'API_KEY_INACTIVE',
  'API_KEY_EXPIRED',
  'INSUFFICIENT_QUOTA',
  'GROUP_IMAGE_GENERATION_DISABLED',
  'TARGET_GROUP_UNAVAILABLE',
  'MODEL_NOT_AVAILABLE',
  'MODEL_CAPABILITY_MISMATCH',
  'USER_TEST_DAILY_LIMIT',
  'USER_TEST_CONCURRENT',
  'USER_TEST_DUPLICATE',
  'USER_TEST_BUDGET_EXCEEDED',
]

describe('userTestErrorKey', () => {
  it('maps every backend reason to a real i18n key', () => {
    for (const reason of BACKEND_REASONS) {
      const key = userTestErrorKey(reason)
      expect(key, `${reason} must not fall through to UNKNOWN`).not.toBe('UNKNOWN')
      // 文案必须真的存在，否则用户看到的是原始 i18n 键名。
      expect(zh.iqBenchmark.userTestError, `zh missing userTestError.${key}`).toHaveProperty(key)
      expect(en.iqBenchmark.userTestError, `en missing userTestError.${key}`).toHaveProperty(key)
    }
  })

  it('collapses the three target failures into one actionable message', () => {
    expect(userTestErrorKey('TARGET_GROUP_UNAVAILABLE')).toBe('TARGET_INVALID')
    expect(userTestErrorKey('MODEL_NOT_AVAILABLE')).toBe('TARGET_INVALID')
    expect(userTestErrorKey('MODEL_CAPABILITY_MISMATCH')).toBe('TARGET_INVALID')
  })

  it('falls back to UNKNOWN for missing or unknown reasons', () => {
    expect(userTestErrorKey(undefined)).toBe('UNKNOWN')
    expect(userTestErrorKey(null)).toBe('UNKNOWN')
    expect(userTestErrorKey('')).toBe('UNKNOWN')
    expect(userTestErrorKey('SOMETHING_NEW_FROM_THE_BACKEND')).toBe('UNKNOWN')
  })

  it('never echoes the backend message text', () => {
    // 未知 reason 的 key 是固定的 UNKNOWN，不含任何后端口径信息。
    const key = userTestErrorKey('BRAND_NEW_REASON')
    expect(key).toBe('UNKNOWN')
    expect(key).not.toContain('BRAND')
  })
})

describe('reasonOf', () => {
  it('reads reason from an axios error response body', () => {
    expect(reasonOf(axiosError(403, { reason: 'API_KEY_EXPIRED' }))).toBe('API_KEY_EXPIRED')
  })

  it('returns undefined for non-axios errors and malformed bodies', () => {
    expect(reasonOf(new Error('boom'))).toBeUndefined()
    expect(reasonOf(axiosError(500, undefined))).toBeUndefined()
    expect(reasonOf(axiosError(500, 'plain string'))).toBeUndefined()
    expect(reasonOf(axiosError(500, { reason: 42 }))).toBeUndefined()
  })
})

describe('userTestErrorKeyOf', () => {
  it('combines reason extraction and mapping', () => {
    expect(userTestErrorKeyOf(axiosError(429, { reason: 'USER_TEST_DAILY_LIMIT' }))).toBe('DAILY_LIMIT')
    expect(userTestErrorKeyOf(new Error('offline'))).toBe('UNKNOWN')
  })
})

describe('isRetryableUserTestError', () => {
  it('treats 429 and 5xx as retryable', () => {
    expect(isRetryableUserTestError(axiosError(429))).toBe(true)
    expect(isRetryableUserTestError(axiosError(500))).toBe(true)
    expect(isRetryableUserTestError(axiosError(503))).toBe(true)
  })

  it('treats configuration and quota failures as non-retryable', () => {
    // 重试同一个请求不会变好：用户必须换 Key / 换题目 / 等明天。
    for (const status of [400, 402, 403, 404]) {
      expect(isRetryableUserTestError(axiosError(status)), `status ${status}`).toBe(false)
    }
  })

  it('treats a network failure (no response) as retryable', () => {
    expect(isRetryableUserTestError(new AxiosError('timeout', 'ECONNABORTED'))).toBe(true)
  })
})

describe('userTestError i18n parity', () => {
  it('has identical keys in zh and en', () => {
    expect(Object.keys(zh.iqBenchmark.userTestError).sort()).toEqual(
      Object.keys(en.iqBenchmark.userTestError).sort(),
    )
  })

  it('has identical source badge keys in zh and en', () => {
    // 只比键，不比值：两种语言的值本来就该不同。
    expect(Object.keys(zh.iqBenchmark.source).sort()).toEqual(Object.keys(en.iqBenchmark.source).sort())
  })
})
