/**
 * 限流响应处理工具。
 *
 * 设计原则：**绝不自动重试**。
 *
 * 后端限流是为了保护数据库，客户端自动重试只会让情况更糟（尤其是指数
 * 退避撞上固定窗口时，会持续把额度打满）。这里只做两件事：
 *   1. 把 429 识别出来，转成用户能懂的剩余等待时间；
 *   2. 让调用方决定是提示还是禁用按钮。
 */
import { AxiosError } from 'axios'

/** 429 的处理结果。 */
export interface RateLimitInfo {
  limited: boolean
  /** 建议等待秒数（来自 Retry-After，缺省 60）。 */
  retryAfterSeconds: number
  /** 是否为功能关闭（503）而非限流。 */
  featureDisabled: boolean
}

/** 默认等待秒数（后端缺省也是 60）。 */
const DEFAULT_RETRY_AFTER_SECONDS = 60

/** 功能关闭时后端返回的 reason 标识。 */
const DISABLED_REASON = 'IQ_BENCHMARK_DISABLED'

/**
 * 从任意错误中提取限流/关闭信息。
 *
 * 同时兼容两种 429 响应体：
 *   - 本模块的 `{code:"RATE_LIMITED", message:"..."}`；
 *   - 上游 panel 限流的同结构响应（前缀不同但 body 一致）。
 */
export function parseRateLimitError(error: unknown): RateLimitInfo {
  const fallback: RateLimitInfo = {
    limited: false,
    retryAfterSeconds: 0,
    featureDisabled: false,
  }
  if (!(error instanceof AxiosError) || !error.response) {
    return fallback
  }
  const { status, headers, data } = error.response

  if (status === 503 && isDisabledPayload(data)) {
    return { ...fallback, featureDisabled: true }
  }
  if (status !== 429) {
    return fallback
  }

  const headerValue = readHeader(headers, 'retry-after')
  const parsed = headerValue ? Number.parseInt(headerValue, 10) : NaN
  const retryAfterSeconds =
    Number.isFinite(parsed) && parsed > 0 ? parsed : DEFAULT_RETRY_AFTER_SECONDS

  return { limited: true, retryAfterSeconds, featureDisabled: false }
}

/** 判断响应体是否表示「功能已关闭」。 */
function isDisabledPayload(data: unknown): boolean {
  if (!data || typeof data !== 'object') return false
  const reason = (data as { reason?: unknown }).reason
  return typeof reason === 'string' && reason === DISABLED_REASON
}

/** 大小写无关地读取响应头。 */
function readHeader(headers: unknown, name: string): string | undefined {
  if (!headers || typeof headers !== 'object') return undefined
  const record = headers as Record<string, unknown>
  const direct = record[name] ?? record[name.toLowerCase()] ?? record[name.toUpperCase()]
  if (typeof direct === 'string') return direct
  if (typeof direct === 'number') return String(direct)
  return undefined
}

/**
 * 估算「还需等待多久」的展示文本。
 *
 * 返回的结构化数据让视图层自己决定措辞（i18n 在视图层处理）。
 */
export function describeRetryAfter(seconds: number): { seconds: number; minutes: number } {
  const safe = Number.isFinite(seconds) && seconds > 0 ? seconds : DEFAULT_RETRY_AFTER_SECONDS
  return { seconds: safe, minutes: Math.ceil(safe / 60) }
}

/**
 * 判断某个操作是否应当被禁用（限流冷却期内）。
 *
 * 传入上次被限流的时间戳与等待秒数；到点后自动恢复。
 */
export function isInCooldown(
  limitedAt: number | null,
  retryAfterSeconds: number,
  now: number = Date.now(),
): boolean {
  if (limitedAt === null) return false
  return now - limitedAt < retryAfterSeconds * 1000
}

/** 剩余冷却秒数（向上取整，最少 0）。 */
export function remainingCooldownSeconds(
  limitedAt: number | null,
  retryAfterSeconds: number,
  now: number = Date.now(),
): number {
  if (limitedAt === null) return 0
  const remaining = retryAfterSeconds * 1000 - (now - limitedAt)
  return remaining > 0 ? Math.ceil(remaining / 1000) : 0
}

/**
 * 极简单飞（single-flight）执行器。
 *
 * 用途：翻页/切筛选时用户可能连点，若每次都发请求会瞬间打满额度。
 * 相同 key 的并发请求共享同一个 Promise，只有最后一个结果被采信。
 */
export class SingleFlight {
  private inflight = new Map<string, Promise<unknown>>()

  /** 相同 key 复用进行中的请求。 */
  run<T>(key: string, task: () => Promise<T>): Promise<T> {
    const existing = this.inflight.get(key)
    if (existing) {
      return existing as Promise<T>
    }
    const promise = task().finally(() => {
      this.inflight.delete(key)
    })
    this.inflight.set(key, promise)
    return promise
  }

  /** 是否还有进行中的请求（测试用）。 */
  get size(): number {
    return this.inflight.size
  }

  /** 清空（组件卸载时调用，避免持有已卸载组件的闭包）。 */
  clear(): void {
    this.inflight.clear()
  }
}
