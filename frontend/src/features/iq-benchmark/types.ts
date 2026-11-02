/**
 * 「模型智商检测作品展」前端类型定义。
 *
 * 这些类型与后端 DTO 一一对应，但**刻意只声明公开侧真实会下发的字段**：
 * 公开接口是白名单，前端也不该假设存在内部字段（例如 internal_user_id、
 * raw_response_ref、group_id）。后端一旦多给了字段，前端也不会消费它。
 */

/** 题目类型：图片生成 / 文本生成 / 代码生成。 */
export type IQCaseKind = 'image' | 'text' | 'code'

/** 期望产出形态。 */
export type IQExpectKind = 'image_url' | 'svg' | 'markdown' | 'text'

/** 作品渲染方式（决定前端用哪种组件展示）。 */
export type IQRenderKind = 'image_url' | 'svg' | 'markdown' | 'text'

/** 作品状态。blocked 表示被安全扫描拦截。 */
export type IQArtifactStatus = 'success' | 'failed' | 'timeout' | 'blocked'

/** 任务状态。非 idle/running 均为熔断态。 */
export type IQTaskStatus =
  | 'idle'
  | 'running'
  | 'quota_exhausted'
  | 'budget_exhausted'
  | 'account_disabled'
  | 'paused_by_admin'
  /** 目标（分组或模型）已失效，需人工改配置后重新启用。 */
  | 'invalid_target'

/** 任务目标解析结果的模型来源。 */
export type IQTargetModelSource = 'runtime' | 'platform_default' | 'none'

/** 分组不可用的原因码（后端返回，前端负责翻译）。 */
export type IQTargetUnavailableReason =
  | 'NO_SCHEDULABLE_ACCOUNT'
  | 'IMAGE_GENERATION_DISABLED'
  | 'ALLOWLIST_FILTERED_EMPTY'
  | 'NO_MODEL_MAPPING'

/** 单个可选的模型。 */
export interface IQTargetModel {
  id: string
  /** 该模型在当前分组下支持的能力（text / image）。 */
  capabilities: string[]
}

/** 一个候选分组及其可选模型（级联下拉的第一级）。 */
export interface IQTargetGroup {
  id: number
  name: string
  platform: string
  allow_image_generation: boolean
  capabilities: string[]
  models: IQTargetModel[]
  model_source: IQTargetModelSource
  /**
   * 空模型集合的成因，或「模型非空但当前没有可调度账号」的警告。
   * 为空表示分组完全健康。
   */
  unavailable_reason?: IQTargetUnavailableReason
}

/** 目标候选集合。 */
export interface IQTargets {
  groups: IQTargetGroup[]
  /** 分组数超上限被截断（只影响展示，不影响已保存的任务）。 */
  truncated: boolean
}

/** 执行触发来源。 */
export type IQRunTrigger = 'schedule' | 'manual' | 'retry'

/** 公开侧题目（后端只下发这 5 个字段）。 */
export interface IQPublicCase {
  id: number
  name: string
  kind: IQCaseKind
  expect_kind: IQExpectKind
  prompt: string
}

/** 公开侧作品。 */
export interface IQPublicArtifact {
  id: number
  case_id: number
  case_name: string
  case_kind: IQCaseKind
  model: string
  render_kind: IQRenderKind
  /** 仅 render_kind=image_url 且未被拦截时存在。 */
  image_url?: string
  /** 仅非图片类且未被拦截时存在。 */
  content?: string
  /** 内容是否被截断（前端据此提示）。 */
  truncated: boolean
  status: IQArtifactStatus
  /** 脱敏后的失败说明；成功作品为空。 */
  error_summary?: string
  /** 仅在管理员开启评分配置时下发。 */
  score?: number
  duration_ms: number
  /** 是否被安全扫描拦截（blocked）。 */
  is_unsafe: boolean
  created_at: string
  /** 以下三项仅在管理员显式开启时才下发。 */
  cost?: number
  input_tokens?: number
  output_tokens?: number
}

/** 公开侧概览聚合行。 */
export interface IQPublicOverviewBucket {
  model: string
  total: number
  success: number
  failed: number
  avg_duration_ms: number
  last_run_at?: string
}

/** 公开侧概览。 */
export interface IQPublicOverview {
  total_artifacts: number
  image_artifacts: number
  text_artifacts: number
  case_count: number
  buckets: IQPublicOverviewBucket[]
}

/** 管理侧题目（字段完整）。 */
export interface IQAdminCase extends IQPublicCase {
  system_prompt: string
  params: Record<string, unknown>
  scoring: 'manual' | 'rule' | 'manual_rule' | 'llm_judge'
  llm_judge_enabled: boolean
  builtin: boolean
  enabled: boolean
  sort_order: number
  created_at: string
  updated_at: string
}

/** 管理侧任务。 */
export interface IQAdminTask {
  id: number
  name: string
  group_id: number
  model: string
  case_ids: number[]
  interval_minutes: number
  jitter_seconds: number
  enabled: boolean
  status: IQTaskStatus
  status_reason: string
  next_run_at?: string
  last_run_at?: string
  lease_owner: string
  lease_expires_at?: string
  created_at: string
  updated_at: string
}

/** 管理侧执行记录。 */
export interface IQAdminRun {
  id: number
  task_id: number
  group_id: number
  model: string
  trigger: IQRunTrigger
  started_at: string
  finished_at?: string
  success_count: number
  failed_count: number
  timeout_count: number
  blocked_count: number
  total_duration_ms: number
  total_cost: number
  total_input_tokens: number
  total_output_tokens: number
  internal_user_id: number
  internal_api_key_id: number
  status: 'running' | 'success' | 'partial' | 'failed'
  cost_collected_at?: string
  created_at: string
}

/** 管理侧作品（字段完整，含诊断信息）。 */
export interface IQAdminArtifact {
  id: number
  run_id: number
  task_id: number
  case_id: number
  case_snapshot: Record<string, unknown>
  group_id: number
  model: string
  request_params: Record<string, unknown>
  render_kind: IQRenderKind
  image_url: string
  content: string
  content_truncated: boolean
  content_sha256: string
  unsafe_flags: string[]
  raw_response_ref: string
  duration_ms: number
  input_tokens: number
  output_tokens: number
  cost: number
  status: IQArtifactStatus
  error_code: string
  error_summary: string
  score?: number
  scored_by?: number
  scored_at?: string
  blob_key: string
  created_at: string
}

/** 限流配置。 */
export interface IQGalleryRateLimitSettings {
  read_limit_per_minute: number
  heavy_limit_per_minute: number
  max_concurrent: number
}

/** 功能配置（管理端可读写）。 */
export interface IQBenchmarkSettings {
  enabled: boolean
  default_group_id: number
  internal_gateway_base_url: string
  interval_min_minutes: number
  interval_max_minutes: number
  model_timeout_seconds: number
  max_artifact_bytes: number
  max_artifacts_per_run: number
  max_task_concurrency: number
  daily_cost_budget: number
  min_balance_floor: number
  scoring_enabled: boolean
  llm_judge_enabled: boolean
  expose_cost_to_user: boolean
  expose_tokens_to_user: boolean
  retention_days: number
  max_artifacts: number
  cleanup_interval_minutes: number
  rate_limit: IQGalleryRateLimitSettings
}

/** 分页信封。 */
export interface IQPage<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

/** 作品列表筛选条件。 */
export interface IQArtifactFilters {
  model?: string
  case_id?: number
  status?: IQArtifactStatus | ''
  kind?: IQRenderKind | ''
  since?: string
  until?: string
  page?: number
  page_size?: number
}

/** 创建/更新题目入参。 */
export interface IQCaseInput {
  name: string
  kind: IQCaseKind
  prompt: string
  system_prompt?: string
  params?: Record<string, unknown>
  expect_kind: IQExpectKind
  scoring?: string
  enabled?: boolean
  sort_order?: number
}

/** 创建/更新任务入参。 */
export interface IQTaskInput {
  name: string
  group_id: number
  model: string
  case_ids: number[]
  interval_minutes: number
  jitter_seconds?: number
  enabled?: boolean
}
