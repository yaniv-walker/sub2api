/**
 * 自测错误到用户可读文案的映射。
 *
 * 为什么单独一个文件而不是散在组件里：
 *   后端把「为什么拒绝」编码在 reason 里（HTTP 状态码不够用 —— 例如
 *   CASE_NOT_OPEN_FOR_USER_TEST 与 API_KEY_INACTIVE 都是 403，
 *   但用户该做的事完全不同）。这张表是**唯一的**映射点，
 *   组件只负责把 i18n key 渲染出来。
 *
 * 未知 reason 一律落到 UNKNOWN，不把后端原始 message 直接显示给用户：
 * message 是给运维看的英文句子，直接暴露既难懂又可能泄露内部细节。
 */
import { AxiosError } from 'axios'

/**
 * 后端 reason → i18n 子键。
 *
 * **左侧字符串必须与 backend/internal/iqbenchmark/handler/user_test_handler.go
 * 的 statusOfUserTestError 逐字一致**（userTestError.spec.ts 会校验这张表
 * 覆盖了后端全部 reason；漏一个就会让用户看到兜底文案）。
 */
const REASON_TO_KEY: Record<string, string> = {
  USER_TEST_DISABLED: 'USER_TEST_DISABLED',
  CASE_NOT_OPEN_FOR_USER_TEST: 'CASE_NOT_OPEN_FOR_USER_TEST',
  USER_NOT_ACTIVE: 'USER_NOT_ACTIVE',
  NOT_FOUND: 'NOT_FOUND',
  API_KEY_INACTIVE: 'API_KEY_INACTIVE',
  API_KEY_EXPIRED: 'API_KEY_EXPIRED',
  INSUFFICIENT_QUOTA: 'INSUFFICIENT_QUOTA',
  GROUP_IMAGE_GENERATION_DISABLED: 'GROUP_IMAGE_DISABLED',
  MODEL_NOT_AVAILABLE: 'TARGET_INVALID',
  MODEL_CAPABILITY_MISMATCH: 'TARGET_INVALID',
  TARGET_GROUP_UNAVAILABLE: 'TARGET_INVALID',
  USER_TEST_DAILY_LIMIT: 'DAILY_LIMIT',
  USER_TEST_CONCURRENT: 'CONCURRENT_LIMIT',
  USER_TEST_DUPLICATE: 'DUPLICATE',
  USER_TEST_BUDGET_EXCEEDED: 'BUDGET_EXCEEDED',
  UNKNOWN: 'UNKNOWN',
}

/**
 * 纯函数：把 reason 归一成 i18n 子键。
 *
 * 未知 reason 落到 UNKNOWN，不把后端英文 message 直接显示给用户。
 */
export function userTestErrorKey(reason: string | undefined | null): string {
  if (!reason) return 'UNKNOWN'
  return REASON_TO_KEY[reason] ?? 'UNKNOWN'
}

/** 从任意错误里取出后端 reason。 */
export function reasonOf(error: unknown): string | undefined {
  if (!(error instanceof AxiosError) || !error.response) return undefined
  const data = error.response.data
  if (!data || typeof data !== 'object') return undefined
  const reason = (data as { reason?: unknown }).reason
  return typeof reason === 'string' ? reason : undefined
}

/** 组合便捷函数：错误 → i18n 子键。 */
export function userTestErrorKeyOf(error: unknown): string {
  return userTestErrorKey(reasonOf(error))
}

/**
 * HTTP 状态 → 是否值得提示「稍后重试」。
 *
 * 429 与 5xx 是暂时的，其余（400/402/403/404）是配置或额度问题，
 * 重试同样的请求不会变好，应引导用户去改选择。
 */
export function isRetryableUserTestError(error: unknown): boolean {
  if (!(error instanceof AxiosError) || !error.response) return true
  const status = error.response.status
  return status === 429 || status >= 500
}
