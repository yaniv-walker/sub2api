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

/**
 * 作品来源。
 *
 * - scheduled：管理员配置的定时检测（官方口径，排行榜默认只统计这一部分）；
 * - user_test：用户用自己的本站 Key 自测（公开可查，但打「用户自测」角标）。
 */
export type IQArtifactSource = 'scheduled' | 'user_test'

/**
 * 来源筛选口径。
 *
 * 'all' 是**查询专用**值（表示不加过滤），不会作为落库状态出现。
 * 公开接口的默认口径是 scheduled：一次自测刷量不应拉偏模型排行。
 */
export type IQArtifactSourceFilter = IQArtifactSource | 'all'

/** 产出审核状态（仅管理侧可见；公开侧恒定 approved）。 */
export type IQModerationStatus = 'approved' | 'pending' | 'rejected'

/** 自测记录状态。 */
export type IQUserTestStatus =
  | 'pending'
  | 'running'
  | 'success'
  | 'failed'
  | 'blocked'
  | 'rejected'

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
  /** 是否开放自测（自测对话框据此过滤候选）。 */
  allow_user_test: boolean
}

/**
 * 单题判定四态。
 *
 * 与 status 是两件事，不能互相推出：
 *   status   = 这次调用跑通了没有（网络/上游/超时）
 *   verdict  = 模型答对了没有
 *
 * 一条 status='success' 的产出完全可能是 degraded ——
 * 那正是「模型悄悄变笨」时唯一能看出来的信号。
 */
export type IQVerdict = 'ok' | 'degraded' | 'generated' | 'unknown'

/** 运行级判定四态（与单题的区别是多了 normal 这个汇总态）。 */
export type IQRunVerdict = 'normal' | 'degraded' | 'unknown' | 'generated'

/**
 * 12 类归一化后的上游错误。
 *
 * 这些是**展示用**的分类，不是内部错误码。内部 error_code 含供应商与
 * 路由信息，后端不下发；前端只可能见到这 12 个值。
 */
export type IQErrorKind =
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
  /** 单题判定四态。缺失时后端已兜底为 unknown，前端仍按 unknown 处理。 */
  verdict: IQVerdict
  /** 渲染形态判定，与 verdict 独立：可以答对但渲染坏掉。 */
  render_verdict: IQVerdict
  /** 归一化错误类别；成功作品不下发。 */
  error_kind?: IQErrorKind
  /** 脱敏后的失败说明；成功作品为空。 */
  error_summary?: string
  /** 仅在管理员开启评分配置时下发。 */
  score?: number
  duration_ms: number
  /** 是否被安全扫描拦截（blocked）。 */
  is_unsafe: boolean
  /**
   * 这条产出是不是**当前登录用户**自己发起的自测。
   *
   * 服务端算好的布尔值：前端拿不到 user_id / api_key_id，
   * 因此无法（也不需要）自己判断归属。
   */
  is_mine: boolean
  /**
   * 产出属主：定时检测（scheduled）或用户自测（user_test）。
   *
   * 用于渲染「用户自测」角标 —— 用户有权知道这条是官方定期检测的结果，
   * 还是另一个用户自己花钱测的。后端只下发 source，不下发触发者身份。
   */
  source: IQArtifactSource
  created_at: string
  /** 以下三项仅在管理员显式开启时才下发。 */
  cost?: number
  input_tokens?: number
  output_tokens?: number
}

/**
 * 公开侧概览聚合行。
 *
 * group_name 是**管理员自填的展示名**（expose_name），不是上游分组名。
 * 为空表示管理员没给这个分组起名 —— 前端应跳过这堵墙，不要自己造名字。
 */
export interface IQPublicOverviewBucket {
  group_id: number
  group_name?: string
  model: string
  total: number
  success: number
  failed: number
  /** 该行的四态汇总。 */
  verdict: IQVerdict
  /** 判定口径的分子分母：passed / logic_total。 */
  passed: number
  logic_total: number
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

/**
 * 监控矩阵单元格的四种状态。
 *
 * 只有四种，刻意不再细分：矩阵的价值是「一眼扫过去看出哪里红了」，
 * 每多一种颜色，辨认成本就涨一截，而信息增量很小。
 */
export type IQMatrixCellState = 'passed' | 'error' | 'incorrect' | 'empty'

/** 矩阵单元格。 */
export interface IQMatrixCell {
  case_id: number
  case_name: string
  model: string
  state: IQMatrixCellState
  /** 可点开的那条产出；0 表示空格子。 */
  artifact_id: number
  verdict?: IQVerdict
  error_kind?: IQErrorKind
  created_at?: string
}

/** 矩阵行轴（题目）。 */
export interface IQMatrixAxis {
  id: number
  name: string
  kind: string
}

/** 监控矩阵。 */
export interface IQMatrix {
  cases: IQMatrixAxis[]
  models: string[]
  cells: IQMatrixCell[]
  /** 实际覆盖的格子数（<= 60）。 */
  total: number
  /** 题目 × 模型超过 60 格时为真，前端须提示「当前只展示了一部分」。 */
  truncated: boolean
}

/** 管理侧题目（字段完整）。 */
export interface IQAdminCase extends IQPublicCase {
  system_prompt: string
  params: Record<string, unknown>
  scoring: 'manual' | 'rule' | 'manual_rule' | 'llm_judge'
  llm_judge_enabled: boolean
  builtin: boolean
  enabled: boolean
  /** allow_user_test 已由 IQPublicCase 提供，此处不重复声明。 */
  sort_order: number
  /** 参考答案；空串表示本题不可判分（图片题 / 开放式生成题）。 */
  expected: string
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
  /** 产出属主：管理侧看全量，需要能一眼区分定时检测与用户自测。 */
  source: IQArtifactSource
  /** 自测产出的触发者（仅管理侧下发；公开侧绝不下发）。 */
  triggered_by_user_id?: number | null
  /** 审核状态（仅管理侧下发）。 */
  moderation_status?: IQModerationStatus
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
  /** 自测总开关（关闭时前端不显示自测入口，后端路由也不注册）。 */
  user_test_enabled: boolean
  /** 每用户每日自测次数上限。 */
  user_test_daily_limit: number
  /** 每用户同时在跑的自测数上限。 */
  user_test_max_concurrent: number
  /** 自测产出是否需要人工审核后才公开。 */
  user_test_require_review: boolean
  /** 每用户每日自测成本上限；0 = 不限制。 */
  user_test_daily_cost_budget: number
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
  /**
   * 来源口径。
   *
   * 公开侧：省略 = scheduled（只看官方定时检测），'all' = 全量。
   * 管理侧：省略 = all（管理要看全量），便于排查自测产出。
   */
  source?: IQArtifactSourceFilter | ''
  /**
   * 审核状态（**仅管理侧**）。
   *
   * 公开侧不看这个参数：那里恒定只返回 approved。管理侧传 `pending`
   * 即「审核队列」—— require_review 打开后待审产出的唯一入口。
   */
  moderation?: IQModerationStatus | ''
  since?: string
  until?: string
  page?: number
  page_size?: number
}

/** 自测提交入参（用哪个 Key 测哪一题的哪个模型）。 */
export interface IQUserTestInput {
  api_key_id: number
  case_id: number
  group_id: number
  model: string
}

/** 自测记录（公开 DTO，不含预估成本与去重键）。 */
export interface IQUserTest {
  id: number
  case_id: number
  group_id: number
  model: string
  status: IQUserTestStatus
  error_code?: string
  /** 成功时指向作品展里的那条产出。 */
  artifact_id?: number | null
  created_at: string
  updated_at: string
}

/** 用户自己的 API Key（自测下拉用，只取必要字段）。 */
export interface IQUserAPIKey {
  id: number
  name: string
  /** 后端只下发脱敏后的前缀（如 sk-ab...xyz）。 */
  key_prefix?: string
  status: string
  /** 该 Key 所属分组（自测时用于限定可选模型）。 */
  group_id?: number
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
  /** 是否开放给用户自测。 */
  allow_user_test?: boolean
  sort_order?: number
  /**
   * 参考答案。
   *
   * 空串表示「本题不可判分」（图片题、开放式生成题）。非空时执行侧按
   * 归一化后的等值比较给出 verdict。自定义的文本/代码题必须提供 ——
   * 没有参考答案就没有判分依据，通过率会退化成「调用成功率」。
   */
  expected?: string
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
