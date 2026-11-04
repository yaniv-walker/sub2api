/**
 * 「模型降智检测」的展示层映射。
 *
 * 这里只做「枚举值 → i18n key / CSS 修饰类」的翻译，
 * 不包含任何业务判断 —— 判定逻辑在后端，前端只负责呈现。
 */
import type {
  MDErrorKind,
  MDItemVerdict,
  MDProtocol,
  MDRenderVerdict,
  MDRun,
  MDRunStatus,
  MDTaskType,
  MDVerdict
} from './types'

/** 运行级判定 → i18n key。 */
export function verdictKey(verdict: MDVerdict): string {
  return `modelDegradation.verdict.${verdict}`
}

/** 运行级判定 → CSS 修饰类后缀。 */
export function verdictClass(verdict: MDVerdict): string {
  return `is-${verdict}`
}

/**
 * 运行状态 + 判定 → 历史卡片缎带文案的 i18n key。
 *
 * 状态优先于判定：运行中/已中止不是「判定」，不该显示成 unknown。
 */
export function ribbonKey(run: MDRun): string {
  if (run.status === 'running') {
    return run.retrying ? 'modelDegradation.history.ribbon.retrying' : 'modelDegradation.history.ribbon.running'
  }
  if (run.status === 'cancelled') {
    return 'modelDegradation.history.ribbon.cancelled'
  }
  // 已结束：按判定走。
  switch (run.verdict) {
    case 'normal':
      return 'modelDegradation.history.ribbon.passed'
    case 'degraded':
      return 'modelDegradation.history.ribbon.degraded'
    case 'generated':
      return 'modelDegradation.history.ribbon.generated'
    default:
      // unknown 且带错误信息 → 显示「失败」，否则显示「无法判定」。
      return run.error_message
        ? 'modelDegradation.history.ribbon.failed'
        : 'modelDegradation.history.ribbon.unknown'
  }
}

/** 缎带的 CSS 修饰类。 */
export function ribbonClass(run: MDRun): string {
  if (run.status === 'running') return run.retrying ? 'is-retrying' : 'is-running'
  if (run.status === 'cancelled') return 'is-cancelled'
  if (run.verdict === 'unknown' && run.error_message) return 'is-failed'
  return `is-${run.verdict}`
}

/** 单题判定 → i18n key。 */
export function itemVerdictKey(verdict: MDItemVerdict): string {
  switch (verdict) {
    case 'ok':
      return 'modelDegradation.item.passed'
    case 'degraded':
      return 'modelDegradation.item.failed'
    case 'generated':
      return 'modelDegradation.item.generated'
    default:
      return 'modelDegradation.item.unknown'
  }
}

/**
 * 12 类归一化错误 → i18n key。
 *
 * 后端 ErrorKinds 的名字与参考站点的 upstream.* 键完全同集合，
 * 因此这里可以直接拼接；未知值兜底到 generic，不显示原始英文码。
 */
export function errorKindKey(kind: MDErrorKind | string): string {
  const known: Record<string, string> = {
    auth_failed: 'authFailed',
    forbidden: 'forbidden',
    not_found: 'notFound',
    timeout: 'timeout',
    rate_limited: 'rateLimited',
    gateway_timeout: 'gatewayTimeout',
    server_error: 'serverError',
    generic: 'generic',
    stream_broken: 'streamBroken',
    response_incomplete: 'responseIncomplete',
    quota_exceeded: 'quotaExceeded',
    empty_response: 'emptyResponse'
  }
  const key = known[kind] ?? 'generic'
  return `modelDegradation.upstream.${key}`
}

/** 协议 → i18n key。 */
export function protocolKey(protocol: MDProtocol): string {
  return `modelDegradation.form.protocolLabel.${protocol}`
}

/** 协议 → 简短展示标签（用于历史卡片上的小标签）。 */
export function protocolShort(protocol: MDProtocol): string {
  switch (protocol) {
    case 'chat_completions':
      return 'Chat'
    case 'responses':
      return 'Responses'
    case 'messages':
      return 'Messages'
    default:
      return protocol
  }
}

/** 题型 → i18n key。 */
export function taskTypeKey(type: MDTaskType): string {
  return type === 'logic' ? 'modelDegradation.form.taskLogic' : 'modelDegradation.form.taskRender'
}

/** 运行状态 → i18n key。 */
export function statusKey(status: MDRunStatus): string {
  switch (status) {
    case 'running':
      return 'modelDegradation.running'
    case 'cancelled':
      return 'modelDegradation.resultPanel.cancelled'
    default:
      return 'modelDegradation.resultPanel.title'
  }
}

/** 生成题判定 → i18n key。 */
export function renderVerdictKey(verdict: MDRenderVerdict): string {
  switch (verdict) {
    case 'ok':
      return 'modelDegradation.metrics.renderOk'
    case 'generated':
      return 'modelDegradation.metrics.renderGenerated'
    case 'degraded':
      return 'modelDegradation.metrics.renderDegraded'
    case 'unknown':
      return 'modelDegradation.metrics.renderUnknown'
    default:
      return 'modelDegradation.metrics.renderSkipped'
  }
}

/**
 * 耗时格式化：毫秒 → 人类可读。
 *
 * 1 分钟以内用秒（保留一位小数），1 分钟以上用「Xm Ys」。
 *
 * 先在总秒数上取整再拆分分钟/秒，而不是分别对分钟四舍五入、
 * 对秒取余 —— 后者会把 119.6 秒渲染成「1m 60s」。
 */
export function formatDuration(ms: number): string {
  if (!ms || ms <= 0) return '—'
  if (ms < 1000) return `${ms}ms`

  const totalSeconds = ms / 1000
  if (totalSeconds < 60) {
    return `${totalSeconds.toFixed(1)}s`
  }

  const rounded = Math.round(totalSeconds)
  const minutes = Math.floor(rounded / 60)
  const seconds = rounded % 60
  return `${minutes}m ${seconds}s`
}

/** 秒 → 「X 分」展示（生成题超时选择用）。 */
export function formatTimeoutMinutes(seconds: number): string {
  const minutes = Math.round(seconds / 60)
  return `${minutes}`
}

/**
 * 时间 → 相对/绝对展示，用于历史墙的时间轴。
 *
 * 与参考实现一致：今天显示时:分，昨天显示「昨天 时:分」，
 * 更早显示月-日。
 */
export function formatRunTime(
  iso: string,
  t: (key: string, params?: Record<string, unknown>) => string
): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''

  const now = new Date()
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const startOfYesterday = new Date(startOfToday.getTime() - 24 * 60 * 60 * 1000)

  const hhmm = `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`

  if (date >= startOfToday) {
    return hhmm
  }
  if (date >= startOfYesterday) {
    return `${t('modelDegradation.history.timeline.yesterday')} ${hhmm}`
  }
  const mm = String(date.getMonth() + 1).padStart(2, '0')
  const dd = String(date.getDate()).padStart(2, '0')
  return `${mm}-${dd}`
}

/** 通过率 → 百分比整数。 */
export function formatPassRate(rate: number): string {
  if (!Number.isFinite(rate) || rate < 0) return '—'
  return `${Math.round(rate * 100)}%`
}
