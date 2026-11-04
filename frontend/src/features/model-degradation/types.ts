/**
 * 「模型降智检测」的前端类型。
 *
 * 与后端 backend/internal/modeldegradation/handler/dto.go 一一对应。
 * 不含任何凭据字段 —— 后端 DTO 里就没有，这里自然也没有。
 */

/** 协议：三种上游形态。 */
export type MDProtocol = 'chat_completions' | 'responses' | 'messages'

/** 思考等级。空串表示不指定。 */
export type MDReasoningEffort = '' | 'low' | 'medium' | 'high' | 'xhigh' | 'max'

/** 运行状态。 */
export type MDRunStatus = 'running' | 'completed' | 'cancelled'

/** 运行级判定四态。 */
export type MDVerdict = 'normal' | 'degraded' | 'unknown' | 'generated'

/** 生成题判定。空串表示本次没跑生成题。 */
export type MDRenderVerdict = '' | 'ok' | 'degraded' | 'unknown' | 'generated'

/** 单题判定。 */
export type MDItemVerdict = 'ok' | 'degraded' | 'generated' | 'unknown'

/** 题型。 */
export type MDTaskType = 'logic' | 'render'

/** 题库里的一道题。 */
export interface MDTask {
  task_type: MDTaskType
  key: string
  title: string
  prompt: string
  expected: string
}

/** GET /model-degradation/tasks 的响应。 */
export interface MDTaskBank {
  logic: MDTask[]
  render: MDTask[]
}

/** 单题结果。 */
export interface MDItem {
  task_type: MDTaskType
  task_key: string
  prompt: string
  expected: string
  response: string
  passed: boolean
  verdict: MDItemVerdict
  detail: string
  /** 生成题抽出的 SVG 源码；逻辑题为空。 */
  artwork?: string
  duration_ms: number
}

/** 运行记录。 */
export interface MDRun {
  id: number
  /** 已脱敏的上游地址，形如 https://api.example.com/*** */
  base_url: string
  model_id: string
  protocol: MDProtocol
  reasoning_effort: MDReasoningEffort
  render_timeout_seconds: number

  status: MDRunStatus
  verdict: MDVerdict
  logic_passed: number
  logic_total: number
  render_verdict: MDRenderVerdict

  error_message: string
  error_kind: string

  duration_ms: number
  created_at: string
  completed_at: string | null
  retrying: boolean

  /** 已脱敏的发起人邮箱，形如 a***@example.com */
  owner_email: string
  group_id: number
  group_name: string
  is_owner: boolean

  /** 首个生成题的作品源码，仅供历史墙缩略图；逻辑题检测时为空。 */
  artwork_preview?: string
}

/** 运行详情。 */
export interface MDRunDetail extends MDRun {
  items: MDItem[]
}

/** 列表响应。 */
export interface MDRunList {
  items: MDRun[]
  total: number
}

/** 顶部统计。 */
export interface MDStats {
  /** 通过率，0..1 */
  pass_rate: number
  last_duration_ms: number
  run_count: number
}

/** 发起检测时提交的自定义题目。 */
export interface MDQuestionInput {
  task_type: MDTaskType
  key: string
  prompt: string
  expected: string
}

/** POST /model-degradation/runs 的请求体。 */
export interface MDStartInput {
  base_url: string
  api_key: string
  model_id: string
  protocol: MDProtocol
  reasoning_effort?: MDReasoningEffort
  render_timeout_seconds?: number
  tasks: MDTaskType[]
  questions: MDQuestionInput[]
}

/** POST /model-degradation/models 的请求体。 */
export interface MDModelsInput {
  base_url: string
  api_key: string
  protocol: MDProtocol
}

/** POST /model-degradation/models 的响应。 */
export interface MDModelsResult {
  models: string[]
  /** 拉取失败时的说明；成功时缺省。 */
  error?: string
}

/** 管理端可见性配置。 */
export interface MDVisibility {
  hidden_group_ids: number[]
  hidden_hosts: string[]
  hosts: MDHostUsage[]
}

/** 未命中分组的上游主机。 */
export interface MDHostUsage {
  host: string
  run_count: number
}

/** 后端的 12 类归一化错误。 */
export type MDErrorKind =
  | 'auth_failed'
  | 'forbidden'
  | 'not_found'
  | 'timeout'
  | 'rate_limited'
  | 'gateway_timeout'
  | 'server_error'
  | 'generic'
  | 'stream_broken'
  | 'response_incomplete'
  | 'quota_exceeded'
  | 'empty_response'
  | ''

/** 生成题超时可选分钟数，与后端 RenderTimeoutChoices 一致。 */
export const RENDER_TIMEOUT_CHOICES = [5, 10, 15, 20, 30, 45, 60] as const

/** 默认生成题超时（分钟）。 */
export const DEFAULT_RENDER_TIMEOUT_MINUTES = 15

/** 可选的思考等级，与后端顺序一致。 */
export const REASONING_EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max'] as const

/** 可选协议，与后端顺序一致。 */
export const PROTOCOLS = ['chat_completions', 'responses', 'messages'] as const

/**
 * 该协议是否支持思考等级。
 *
 * Anthropic messages 协议不接受 reasoning_effort，发了会 400 ——
 * 那个 400 会被归一化成 server_error，看起来像上游挂了。
 * 因此这里直接不给用户选，与后端保持一致。
 */
export function supportsReasoningEffort(protocol: MDProtocol): boolean {
  return protocol === 'chat_completions' || protocol === 'responses'
}

/** 该协议是否支持生成题。 */
export function supportsRender(protocol: MDProtocol): boolean {
  return protocol === 'responses' || protocol === 'messages'
}

/** 是否处于运行中。 */
export function isRunning(run: MDRun): boolean {
  return run.status === 'running'
}

/** 是否是纯生成模式（没跑逻辑题）。 */
export function isPureGeneration(run: MDRun): boolean {
  return run.logic_total === 0
}
