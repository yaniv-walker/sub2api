import { describe, expect, it } from 'vitest'
import {
  errorKindKey,
  formatDuration,
  formatPassRate,
  formatTimeoutMinutes,
  itemVerdictKey,
  protocolShort,
  renderVerdictKey,
  ribbonClass,
  ribbonKey,
  verdictClass,
  verdictKey
} from '../labels'
import type { MDRun } from '../types'

/** 造一条运行记录，只覆盖判定相关字段。 */
function makeRun(over: Partial<MDRun> = {}): MDRun {
  return {
    id: 1,
    base_url: 'https://a.example.com/***',
    model_id: 'gpt-4o',
    protocol: 'chat_completions',
    reasoning_effort: '',
    render_timeout_seconds: 0,
    status: 'completed',
    verdict: 'normal',
    logic_passed: 1,
    logic_total: 1,
    render_verdict: 'none',
    error_message: '',
    error_kind: '',
    duration_ms: 1200,
    created_at: '2026-01-01T00:00:00Z',
    completed_at: '2026-01-01T00:00:02Z',
    retrying: false,
    owner_email: 'a***@example.com',
    group_id: 0,
    group_name: '',
    is_owner: false,
    ...over
  }
}

describe('verdictKey / verdictClass', () => {
  it('四种判定都映射到 verdict.*', () => {
    expect(verdictKey('normal')).toBe('modelDegradation.verdict.normal')
    expect(verdictKey('degraded')).toBe('modelDegradation.verdict.degraded')
    expect(verdictKey('unknown')).toBe('modelDegradation.verdict.unknown')
    expect(verdictKey('generated')).toBe('modelDegradation.verdict.generated')
  })

  it('修饰类名带 is- 前缀', () => {
    expect(verdictClass('normal')).toBe('is-normal')
    expect(verdictClass('degraded')).toBe('is-degraded')
  })
})

describe('ribbonKey', () => {
  it('运行中显示「检测中」，不带判定', () => {
    expect(ribbonKey(makeRun({ status: 'running', verdict: 'unknown' }))).toBe(
      'modelDegradation.history.ribbon.running'
    )
  })

  it('重试中优先于运行中', () => {
    expect(ribbonKey(makeRun({ status: 'running', retrying: true }))).toBe(
      'modelDegradation.history.ribbon.retrying'
    )
  })

  it('已中止显示「已中止」，即使判定是 normal', () => {
    expect(ribbonKey(makeRun({ status: 'cancelled', verdict: 'normal' }))).toBe(
      'modelDegradation.history.ribbon.cancelled'
    )
  })

  it('正常 / 降智 / 已生成各有缎带', () => {
    expect(ribbonKey(makeRun({ verdict: 'normal' }))).toBe('modelDegradation.history.ribbon.passed')
    expect(ribbonKey(makeRun({ verdict: 'degraded' }))).toBe('modelDegradation.history.ribbon.degraded')
    expect(ribbonKey(makeRun({ verdict: 'generated' }))).toBe('modelDegradation.history.ribbon.generated')
  })

  it('unknown 且有错误信息 → 失败；否则 → 无法判定', () => {
    expect(ribbonKey(makeRun({ verdict: 'unknown', error_message: '超时' }))).toBe(
      'modelDegradation.history.ribbon.failed'
    )
    expect(ribbonKey(makeRun({ verdict: 'unknown', error_message: '' }))).toBe(
      'modelDegradation.history.ribbon.unknown'
    )
  })
})

describe('ribbonClass', () => {
  it('状态优先于判定', () => {
    expect(ribbonClass(makeRun({ status: 'running' }))).toBe('is-running')
    expect(ribbonClass(makeRun({ status: 'running', retrying: true }))).toBe('is-retrying')
    expect(ribbonClass(makeRun({ status: 'cancelled' }))).toBe('is-cancelled')
  })

  it('unknown 带错误时归到 is-failed，便于统一上色', () => {
    expect(ribbonClass(makeRun({ verdict: 'unknown', error_message: 'x' }))).toBe('is-failed')
    expect(ribbonClass(makeRun({ verdict: 'unknown', error_message: '' }))).toBe('is-unknown')
  })

  it('正常与降智直接映射', () => {
    expect(ribbonClass(makeRun({ verdict: 'normal' }))).toBe('is-normal')
    expect(ribbonClass(makeRun({ verdict: 'degraded' }))).toBe('is-degraded')
  })
})

describe('errorKindKey', () => {
  it('12 类错误都映射到 upstream.*', () => {
    expect(errorKindKey('auth_failed')).toBe('modelDegradation.upstream.authFailed')
    expect(errorKindKey('forbidden')).toBe('modelDegradation.upstream.forbidden')
    expect(errorKindKey('not_found')).toBe('modelDegradation.upstream.notFound')
    expect(errorKindKey('timeout')).toBe('modelDegradation.upstream.timeout')
    expect(errorKindKey('rate_limited')).toBe('modelDegradation.upstream.rateLimited')
    expect(errorKindKey('quota_exceeded')).toBe('modelDegradation.upstream.quotaExceeded')
    expect(errorKindKey('server_error')).toBe('modelDegradation.upstream.serverError')
    expect(errorKindKey('gateway_timeout')).toBe('modelDegradation.upstream.gatewayTimeout')
    expect(errorKindKey('stream_broken')).toBe('modelDegradation.upstream.streamBroken')
    expect(errorKindKey('response_incomplete')).toBe('modelDegradation.upstream.responseIncomplete')
    expect(errorKindKey('empty_response')).toBe('modelDegradation.upstream.emptyResponse')
    expect(errorKindKey('generic')).toBe('modelDegradation.upstream.generic')
  })

  it('未知值兜底到 generic，不把原始英文码漏给用户', () => {
    expect(errorKindKey('something_new')).toBe('modelDegradation.upstream.generic')
    expect(errorKindKey('')).toBe('modelDegradation.upstream.generic')
  })
})

describe('itemVerdictKey / renderVerdictKey', () => {
  it('单题四态', () => {
    expect(itemVerdictKey('ok')).toBe('modelDegradation.item.passed')
    expect(itemVerdictKey('degraded')).toBe('modelDegradation.item.failed')
    expect(itemVerdictKey('generated')).toBe('modelDegradation.item.generated')
    expect(itemVerdictKey('unknown')).toBe('modelDegradation.item.unknown')
  })

  it('生成题判定含未检测态', () => {
    expect(renderVerdictKey('ok')).toBe('modelDegradation.metrics.renderOk')
    expect(renderVerdictKey('generated')).toBe('modelDegradation.metrics.renderGenerated')
    expect(renderVerdictKey('degraded')).toBe('modelDegradation.metrics.renderDegraded')
    expect(renderVerdictKey('unknown')).toBe('modelDegradation.metrics.renderUnknown')
    expect(renderVerdictKey('none')).toBe('modelDegradation.metrics.renderSkipped')
  })
})

describe('protocolShort', () => {
  it('三种协议各有短标签', () => {
    expect(protocolShort('chat_completions')).toBe('Chat')
    expect(protocolShort('responses')).toBe('Responses')
    expect(protocolShort('messages')).toBe('Messages')
  })
})

describe('formatDuration', () => {
  it('0 与负数显示占位符', () => {
    expect(formatDuration(0)).toBe('—')
    expect(formatDuration(-1)).toBe('—')
  })

  it('小于 1 秒用毫秒', () => {
    expect(formatDuration(500)).toBe('500ms')
  })

  it('一秒到一分钟用秒，保留一位小数', () => {
    expect(formatDuration(1000)).toBe('1.0s')
    expect(formatDuration(12300)).toBe('12.3s')
  })

  it('超过一分钟用 m s', () => {
    expect(formatDuration(60000)).toBe('1m 0s')
    expect(formatDuration(125000)).toBe('2m 5s')
  })

  it('秒数四舍五入到 60 时顺进一分钟', () => {
    expect(formatDuration(119600)).toBe('2m 0s')
  })
})

describe('formatTimeoutMinutes', () => {
  it('秒换算成分钟', () => {
    expect(formatTimeoutMinutes(300)).toBe('5')
    expect(formatTimeoutMinutes(900)).toBe('15')
  })
})

describe('formatPassRate', () => {
  it('比例换成整数百分比', () => {
    expect(formatPassRate(0)).toBe('0%')
    expect(formatPassRate(0.5)).toBe('50%')
    expect(formatPassRate(1)).toBe('100%')
  })

  it('小数四舍五入', () => {
    expect(formatPassRate(0.666)).toBe('67%')
  })

  it('非法值显示占位符而不是 NaN%', () => {
    expect(formatPassRate(Number.NaN)).toBe('—')
    expect(formatPassRate(-1)).toBe('—')
  })
})
