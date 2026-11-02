/**
 * 「模型智商检测作品展」API 客户端。
 *
 * 两类接口分开导出（公开只读 / 管理）：
 *   - 公开接口只消费白名单 DTO，绝不请求任何内部字段；
 *   - 管理接口是本模块自己的命名空间，与上游 admin API 无交集。
 *
 * 限流返回 429 时**不做自动重试**：由 rateLimit.ts 统一处理成友好的
 * 提示并让用户自己决定何时再试，避免客户端自动重试把限流拖成雪崩。
 */
import { apiClient } from '@/api/client'
import type {
  IQAdminArtifact,
  IQAdminCase,
  IQAdminRun,
  IQModerationStatus,
  IQAdminTask,
  IQArtifactFilters,
  IQBenchmarkSettings,
  IQCaseInput,
  IQPage,
  IQPublicArtifact,
  IQPublicCase,
  IQPublicOverview,
  IQTargets,
  IQTaskInput,
  IQUserAPIKey,
  IQUserTest,
  IQUserTestInput,
} from './types'

/** 后端统一响应信封。 */
interface Envelope<T> {
  code: number
  message: string
  reason?: string
  data?: T
}

/** 分页信封（后端 Paginated 的 data 结构）。 */
interface PaginatedEnvelope<T> {
  items: T[]
  total: number
  page: number
  page_size: number
  pages: number
}

const USER_BASE = '/iq-gallery'
const USER_TESTS_BASE = '/iq-gallery/user-tests'
const ADMIN_BASE = '/admin/iq-benchmark'

/** 把筛选条件转成 query 参数，跳过空值。 */
function toParams(filters: IQArtifactFilters): Record<string, string | number> {
  const params: Record<string, string | number> = {}
  if (filters.model) params.model = filters.model
  if (filters.case_id) params.case_id = filters.case_id
  if (filters.status) params.status = filters.status
  if (filters.kind) params.kind = filters.kind
  // source 是唯一「空字符串也有含义」的字段（省略 = 各侧自己的默认口径），
  // 因此只在非空时下发，让后端的默认值生效。
  if (filters.source) params.source = filters.source
  // moderation 只在管理侧生效；公开侧恒为 approved，多传也无害，
  // 因为那条路径由 ModeratedOnly 分支先行接管。
  if (filters.moderation) params.moderation = filters.moderation
  if (filters.since) params.since = filters.since
  if (filters.until) params.until = filters.until
  if (filters.page) params.page = filters.page
  if (filters.page_size) params.page_size = filters.page_size
  return params
}

/** 公开：作品展总览。 */
export async function fetchOverview(): Promise<IQPublicOverview> {
  const { data } = await apiClient.get<Envelope<IQPublicOverview>>(`${USER_BASE}/overview`)
  return data.data ?? emptyOverview()
}

/** 公开：作品列表（分页）。 */
export async function fetchArtifacts(filters: IQArtifactFilters): Promise<IQPage<IQPublicArtifact>> {
  const { data } = await apiClient.get<Envelope<PaginatedEnvelope<IQPublicArtifact>>>(
    `${USER_BASE}/artifacts`,
    { params: toParams(filters) },
  )
  return normalizePage(data.data)
}

/** 公开：单件作品详情。 */
export async function fetchArtifact(id: number): Promise<IQPublicArtifact | null> {
  const { data } = await apiClient.get<Envelope<IQPublicArtifact>>(`${USER_BASE}/artifacts/${id}`)
  return data.data ?? null
}

/**
 * 公开：对比多件作品。
 *
 * 后端一次最多接受 8 条；这里在客户端也截断一次，避免把明显无效的
 * 请求发出去浪费限流额度。
 */
export async function fetchCompare(ids: number[]): Promise<IQPublicArtifact[]> {
  const trimmed = ids.filter((id) => Number.isFinite(id) && id > 0).slice(0, 8)
  if (trimmed.length === 0) return []
  const { data } = await apiClient.get<Envelope<IQPublicArtifact[]>>(`${USER_BASE}/artifacts/compare`, {
    params: { ids: trimmed.join(',') },
  })
  return data.data ?? []
}

/** 公开：题目列表（只读，用于筛选下拉）。 */
export async function fetchPublicCases(): Promise<IQPublicCase[]> {
  const { data } = await apiClient.get<Envelope<PaginatedEnvelope<IQPublicCase>>>(`${USER_BASE}/cases`, {
    params: { page_size: 50 },
  })
  return data.data?.items ?? []
}

// ---------------- 用户自测 ----------------

/**
 * 自测：提交一次（用当前用户自己的 Key）。
 *
 * 后端会做开关/题目/Key 归属/配额/去重等全套预检，任一不通过都会
 * 返回明确的 reason（见 handleUserTestError）。成功返回 _pending_ 记录，
 * 前端轮询或直接用返回的 id 查结果。
 */
export async function submitUserTest(input: IQUserTestInput): Promise<IQUserTest> {
  const { data } = await apiClient.post<Envelope<IQUserTest>>(`${USER_TESTS_BASE}`, input)
  if (!data.data) {
    throw new Error('submitUserTest: empty response')
  }
  return data.data
}

/** 自测：我的历史。 */
export async function fetchUserTests(page = 1, pageSize = 20): Promise<IQPage<IQUserTest>> {
  const { data } = await apiClient.get<Envelope<PaginatedEnvelope<IQUserTest>>>(`${USER_TESTS_BASE}`, {
    params: { page, page_size: pageSize },
  })
  return normalizePage(data.data)
}

/** 自测：查单条（用于轮询 pending/running 的终态）。 */
export async function fetchUserTest(id: number): Promise<IQUserTest | null> {
  const { data } = await apiClient.get<Envelope<IQUserTest>>(`${USER_TESTS_BASE}/${id}`)
  return data.data ?? null
}

/**
 * 自测：可选的 API Key 列表。
 *
 * 读的是上游既有的 /keys 接口（本模块不新增 Key 管理能力），
 * 只取自测下拉需要的字段。
 */
export async function fetchMyAPIKeys(): Promise<IQUserAPIKey[]> {
  const { data } = await apiClient.get<Envelope<{ items?: IQUserAPIKey[] } | IQUserAPIKey[]>>('/keys')
  const payload = data.data
  if (!payload) return []
  if (Array.isArray(payload)) return payload
  return payload.items ?? []
}

/**
 * 自测：我可以选的分组与模型。
 *
 * 传 apiKeyID 时后端会按该 Key 绑定的分组收窄候选 —— 与提交时的判定一致，
 * 避免前端展示一个提交时会被忽略的分组。
 */
export async function fetchMyTargets(apiKeyID?: number): Promise<IQTargets> {
  const params: Record<string, number> = {}
  if (apiKeyID && apiKeyID > 0) params.api_key_id = apiKeyID
  const { data } = await apiClient.get<Envelope<IQTargets>>(`${USER_TESTS_BASE}/targets`, { params })
  return data.data ?? { groups: [], truncated: false }
}

// ---------------- 管理侧 ----------------

/** 管理：读取配置（关闭状态下也可读写）。 */
export async function fetchSettings(): Promise<IQBenchmarkSettings | null> {
  const { data } = await apiClient.get<Envelope<IQBenchmarkSettings>>(`${ADMIN_BASE}/settings`)
  return data.data ?? null
}

/**
 * 管理：目标候选（分组 + 模型级联下拉）。
 *
 * refresh=true 会让后端失效模型缓存后重取：管理员刚配好账号/model_mapping 时用。
 * 前端另有 30s 节流（见 TaskForm），避免连点刷爆上游缓存。
 */
export async function fetchTargets(refresh = false): Promise<IQTargets> {
  const { data } = await apiClient.get<Envelope<IQTargets>>(`${ADMIN_BASE}/targets`, {
    params: refresh ? { refresh: 1 } : undefined,
  })
  return data.data ?? { groups: [], truncated: false }
}

/** 管理：保存配置。 */
export async function saveSettings(settings: IQBenchmarkSettings): Promise<IQBenchmarkSettings | null> {
  const { data } = await apiClient.put<Envelope<IQBenchmarkSettings>>(`${ADMIN_BASE}/settings`, settings)
  return data.data ?? null
}

/** 管理：题目列表。 */
export async function fetchAdminCases(page = 1, pageSize = 50): Promise<IQPage<IQAdminCase>> {
  const { data } = await apiClient.get<Envelope<PaginatedEnvelope<IQAdminCase>>>(`${ADMIN_BASE}/cases`, {
    params: { page, page_size: pageSize },
  })
  return normalizePage(data.data)
}

/** 管理：新建题目。 */
export async function createCase(input: IQCaseInput): Promise<IQAdminCase | null> {
  const { data } = await apiClient.post<Envelope<IQAdminCase>>(`${ADMIN_BASE}/cases`, input)
  return data.data ?? null
}

/** 管理：更新题目。 */
export async function updateCase(id: number, input: IQCaseInput): Promise<IQAdminCase | null> {
  const { data } = await apiClient.put<Envelope<IQAdminCase>>(`${ADMIN_BASE}/cases/${id}`, input)
  return data.data ?? null
}

/** 管理：删除题目。 */
export async function deleteCase(id: number): Promise<void> {
  await apiClient.delete(`${ADMIN_BASE}/cases/${id}`)
}

/** 管理：任务列表。 */
export async function fetchAdminTasks(page = 1, pageSize = 50): Promise<IQPage<IQAdminTask>> {
  const { data } = await apiClient.get<Envelope<PaginatedEnvelope<IQAdminTask>>>(`${ADMIN_BASE}/tasks`, {
    params: { page, page_size: pageSize },
  })
  return normalizePage(data.data)
}

/** 管理：新建任务。 */
export async function createTask(input: IQTaskInput): Promise<IQAdminTask | null> {
  const { data } = await apiClient.post<Envelope<IQAdminTask>>(`${ADMIN_BASE}/tasks`, input)
  return data.data ?? null
}

/** 管理：更新任务。 */
export async function updateTask(id: number, input: IQTaskInput): Promise<IQAdminTask | null> {
  const { data } = await apiClient.put<Envelope<IQAdminTask>>(`${ADMIN_BASE}/tasks/${id}`, input)
  return data.data ?? null
}

/** 管理：删除任务。 */
export async function deleteTask(id: number): Promise<void> {
  await apiClient.delete(`${ADMIN_BASE}/tasks/${id}`)
}

/** 管理：启用/停用任务（启用可解除熔断状态）。 */
export async function setTaskEnabled(id: number, enabled: boolean): Promise<IQAdminTask | null> {
  const { data } = await apiClient.put<Envelope<IQAdminTask>>(`${ADMIN_BASE}/tasks/${id}/enabled`, { enabled })
  return data.data ?? null
}

/** 管理：立即执行一次（不等调度）。 */
export async function runTaskNow(id: number): Promise<void> {
  await apiClient.post(`${ADMIN_BASE}/tasks/${id}/run-now`)
}

/** 管理：某任务的执行记录。 */
export async function fetchRuns(taskId: number, page = 1, pageSize = 20): Promise<IQPage<IQAdminRun>> {
  const { data } = await apiClient.get<Envelope<PaginatedEnvelope<IQAdminRun>>>(
    `${ADMIN_BASE}/tasks/${taskId}/runs`,
    { params: { page, page_size: pageSize } },
  )
  return normalizePage(data.data)
}

/** 管理：作品列表（含诊断字段）。 */
export async function fetchAdminArtifacts(filters: IQArtifactFilters): Promise<IQPage<IQAdminArtifact>> {
  const { data } = await apiClient.get<Envelope<PaginatedEnvelope<IQAdminArtifact>>>(
    `${ADMIN_BASE}/artifacts`,
    { params: toParams(filters) },
  )
  return normalizePage(data.data)
}

/** 管理：删除作品（软删）。 */
export async function deleteArtifact(id: number): Promise<void> {
  await apiClient.delete(`${ADMIN_BASE}/artifacts/${id}`)
}

/** 管理：人工评分（0-100）。 */
export async function scoreArtifact(id: number, score: number): Promise<IQAdminArtifact | null> {
  const { data } = await apiClient.put<Envelope<IQAdminArtifact>>(`${ADMIN_BASE}/artifacts/${id}/score`, {
    score,
  })
  return data.data ?? null
}

/**
 * 审核一件产出。
 *
 * 后端只回 {moderated:true}，不回传整行 —— 与评分的差异是有意的：
 * 审核是幂等的状态迁移，不需要把整条记录再传一遍。
 */
export async function moderateArtifact(id: number, status: IQModerationStatus): Promise<void> {
  await apiClient.put(`${ADMIN_BASE}/artifacts/${id}/moderation`, { status })
}

/** 空概览（请求失败时的安全兜底，避免页面因 undefined 崩掉）。 */
function emptyOverview(): IQPublicOverview {
  return {
    total_artifacts: 0,
    image_artifacts: 0,
    text_artifacts: 0,
    case_count: 0,
    buckets: [],
  }
}

/** 统一分页结构，补齐后端可能省略的字段。 */
function normalizePage<T>(raw: PaginatedEnvelope<T> | undefined): IQPage<T> {
  if (!raw) {
    return { items: [], total: 0, page: 1, page_size: 20 }
  }
  return {
    items: raw.items ?? [],
    total: raw.total ?? 0,
    page: raw.page ?? 1,
    page_size: raw.page_size ?? 20,
  }
}
