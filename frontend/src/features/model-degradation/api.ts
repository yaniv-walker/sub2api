/**
 * 「模型降智检测」的 API 层。
 *
 * 路径与后端 internal/server/routes/model_degradation.go 一一对应。
 * 注意 base 是 /model-degradation（apiClient 已带 /api/v1 前缀）。
 */
import { apiClient } from '@/api/client'
import type {
  MDModelsInput,
  MDModelsResult,
  MDRunDetail,
  MDRunList,
  MDStartInput,
  MDStats,
  MDTaskBank,
  MDVisibility
} from './types'

// apiClient 已在响应拦截器中解包 { code, message, data }。
// 此处的 response.data 直接是业务数据，不再读取第二层 data。

const BASE = '/model-degradation'
const ADMIN_BASE = '/admin/model-degradation'

/** 读取题库。 */
export async function fetchTaskBank(): Promise<MDTaskBank> {
  const { data } = await apiClient.get<MDTaskBank>(`${BASE}/tasks`)
  const bank = data
  return {
    // 显式兜成空数组：模板里直接 .length / v-for，undefined 会当场炸。
    logic: Array.isArray(bank?.logic) ? bank.logic : [],
    render: Array.isArray(bank?.render) ? bank.render : []
  }
}

/**
 * 用用户凭据拉取上游模型列表。
 *
 * 后端在拉取失败时仍返回 200 + 空列表 + error 说明（这是便利功能，
 * 拉不到就该让用户手输，不该卡住表单），因此这里也按「可能为空」处理。
 */
export async function fetchModels(input: MDModelsInput): Promise<MDModelsResult> {
  const { data } = await apiClient.post<MDModelsResult>(`${BASE}/models`, input)
  const result = data
  return {
    models: Array.isArray(result?.models) ? result.models : [],
    error: result?.error
  }
}

/** 发起一次检测。返回 running 状态的详情，前端据此开始轮询。 */
export async function startRun(input: MDStartInput): Promise<MDRunDetail> {
  const { data } = await apiClient.post<MDRunDetail>(`${BASE}/runs`, input)
  return data
}

/** 读取检测详情。 */
export async function fetchRunDetail(id: number): Promise<MDRunDetail> {
  const { data } = await apiClient.get<MDRunDetail>(`${BASE}/runs/${id}`)
  return data
}

export interface ListRunsParams {
  limit?: number
  offset?: number
  /** mine=1 只看自己的。 */
  mine?: boolean
  verdict?: string
  task_type?: string
}

/** 读取检测历史（公共墙或我的）。 */
export async function fetchRuns(params: ListRunsParams = {}): Promise<MDRunList> {
  const query: Record<string, string | number> = {}
  if (params.limit) query.limit = params.limit
  if (params.offset) query.offset = params.offset
  if (params.mine) query.mine = 1
  if (params.verdict) query.verdict = params.verdict
  if (params.task_type) query.task_type = params.task_type

  const { data } = await apiClient.get<MDRunList>(`${BASE}/runs`, { params: query })
  const list = data
  return {
    items: Array.isArray(list?.items) ? list.items : [],
    total: typeof list?.total === 'number' ? list.total : 0
  }
}

/** 读取顶部统计。 */
export async function fetchStats(mine = false): Promise<MDStats> {
  const params: Record<string, string | number> = {}
  if (mine) params.mine = 1
  const { data } = await apiClient.get<MDStats>(`${BASE}/stats`, { params })
  const stats = data
  return {
    pass_rate: typeof stats?.pass_rate === 'number' ? stats.pass_rate : 0,
    last_duration_ms: typeof stats?.last_duration_ms === 'number' ? stats.last_duration_ms : 0,
    run_count: typeof stats?.run_count === 'number' ? stats.run_count : 0
  }
}

/**
 * 中止一次检测。
 *
 * 后端在「已经结束」时返回 409。那不算失败 —— 用户想要的结果
 * （它不再占用资源）已经达成了，因此这里吞掉 409，只把其它错误抛出去。
 */
export async function cancelRun(id: number): Promise<void> {
  try {
    await apiClient.post(`${BASE}/runs/${id}/cancel`)
  } catch (err) {
    if (isAlreadyEnded(err)) return
    throw err
  }
}

/** 判断错误是否是我们当作成功的 409。 */
function isAlreadyEnded(err: unknown): boolean {
  const status = (err as { response?: { status?: number } })?.response?.status
  return status === 409
}

/** 读取可见性配置（管理端）。 */
export async function fetchVisibility(): Promise<MDVisibility> {
  const { data } = await apiClient.get<MDVisibility>(`${ADMIN_BASE}/history-visibility`)
  const vis = data
  return {
    hidden_group_ids: Array.isArray(vis?.hidden_group_ids) ? vis.hidden_group_ids : [],
    hidden_hosts: Array.isArray(vis?.hidden_hosts) ? vis.hidden_hosts : [],
    hosts: Array.isArray(vis?.hosts) ? vis.hosts : []
  }
}

/** 切换分组可见性（管理端）。 */
export async function setGroupHidden(groupId: number, hidden: boolean): Promise<void> {
  await apiClient.put(`${ADMIN_BASE}/history-visibility/groups/${groupId}`, { hidden })
}

/** 切换主机可见性（管理端）。 */
export async function setHostHidden(host: string, hidden: boolean): Promise<void> {
  await apiClient.put(`${ADMIN_BASE}/history-visibility/hosts`, { host, hidden })
}
