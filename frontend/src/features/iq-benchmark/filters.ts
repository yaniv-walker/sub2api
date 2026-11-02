/**
 * 筛选与对比的纯函数工具。
 *
 * 全部是纯函数（无 DOM、无 Pinia），因此可以完整单测；
 * 视图层只负责把状态喂进来。
 */
import type {
  IQArtifactFilters,
  IQRenderKind,
  IQArtifactStatus,
} from './types'

/** 筛选状态（视图层的本地状态形状）。 */
export interface IQFilterState {
  model: string
  caseId: number | null
  status: IQArtifactStatus | ''
  kind: IQRenderKind | ''
  /** 时间范围：最近 N 天；0 表示不限。 */
  sinceDays: number
}

/** 默认筛选（不限模型/题目，默认看最近 7 天）。 */
export function defaultFilterState(): IQFilterState {
  return { model: '', caseId: null, status: '', kind: '', sinceDays: 7 }
}

/** 可选的模型列表（从作品里推导，保持稳定排序）。 */
export function deriveModelOptions(models: string[]): string[] {
  const unique = Array.from(new Set(models.filter((m) => Boolean(m && m.trim()))))
  unique.sort((a, b) => a.localeCompare(b))
  return unique
}

/**
 * 把本地筛选状态转成 API 参数。
 *
 * sinceDays 用**相对时间**而不是让用户填日期：
 * 检测结果是持续增长的，用户真正关心的是「最近多久」。
 */
export function toArtifactFilters(
  state: IQFilterState,
  now: Date = new Date(),
  page = 1,
  pageSize = 20,
): IQArtifactFilters {
  const filters: IQArtifactFilters = { page, page_size: pageSize }
  if (state.model.trim()) filters.model = state.model.trim()
  if (state.caseId != null) filters.case_id = state.caseId
  if (state.status) filters.status = state.status
  if (state.kind) filters.kind = state.kind
  if (state.sinceDays > 0) {
    const since = new Date(now.getTime() - state.sinceDays * 24 * 60 * 60 * 1000)
    filters.since = since.toISOString()
  }
  return filters
}

/** 判断筛选是否为默认状态（用于显示「清除筛选」按钮）。 */
export function isDefaultFilterState(state: IQFilterState): boolean {
  const def = defaultFilterState()
  return (
    state.model === def.model &&
    state.caseId === def.caseId &&
    state.status === def.status &&
    state.kind === def.kind &&
    state.sinceDays === def.sinceDays
  )
}

/** 生成稳定的筛选签名，用于「筛选变化才重新拉取」的比较。 */
export function filterSignature(state: IQFilterState): string {
  return [state.model.trim(), state.caseId ?? '', state.status, state.kind, state.sinceDays].join('|')
}

// ---------------- 对比选择 ----------------

/** 对比数量上限（与后端一致）。 */
export const MAX_COMPARE_ITEMS = 8

/**
 * 切换某件作品的选中状态。
 *
 * 超上限时**不静默丢弃**，而是返回 added=false 让调用方提示用户：
 * 静默忽略会让用户以为点击失效。
 */
export function toggleCompareSelection(
  selected: number[],
  id: number,
): { selected: number[]; added: boolean; reason?: 'limit' | 'invalid' } {
  if (!Number.isFinite(id) || id <= 0) {
    return { selected, added: false, reason: 'invalid' }
  }
  const index = selected.indexOf(id)
  if (index >= 0) {
    const next = selected.slice()
    next.splice(index, 1)
    return { selected: next, added: false }
  }
  if (selected.length >= MAX_COMPARE_ITEMS) {
    return { selected, added: false, reason: 'limit' }
  }
  return { selected: [...selected, id], added: true }
}

/** 判断某件作品是否已被选中。 */
export function isSelected(selected: number[], id: number): boolean {
  return selected.includes(id)
}

/**
 * 按题目分组对比结果。
 *
 * 对比的价值在于「同一道题，不同模型/不同时间的表现」，
 * 因此必须先按题目聚在一起，否则横向对比没有意义。
 */
export function groupByCase<T extends { case_id: number; case_name: string }>(
  items: T[],
): Array<{ caseId: number; caseName: string; items: T[] }> {
  const groups = new Map<number, { caseId: number; caseName: string; items: T[] }>()
  for (const item of items) {
    const existing = groups.get(item.case_id)
    if (existing) {
      existing.items.push(item)
      continue
    }
    groups.set(item.case_id, {
      caseId: item.case_id,
      caseName: item.case_name,
      items: [item],
    })
  }
  return Array.from(groups.values())
}

// ---------------- 展示格式化 ----------------

/** 毫秒 → 可读时长。 */
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '—'
  if (ms < 1000) return `${Math.round(ms)} ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`
  const minutes = Math.floor(ms / 60_000)
  const seconds = Math.round((ms % 60_000) / 1000)
  return `${minutes}m ${seconds}s`
}

/** 成功率（0-1），分母为 0 时返回 null 而不是 0，避免误导。 */
export function successRate(success: number, total: number): number | null {
  if (!Number.isFinite(total) || total <= 0) return null
  return success / total
}

/** 成功率 → 百分比文本。 */
export function formatPercent(ratio: number | null): string {
  if (ratio === null) return '—'
  const clamped = Math.min(Math.max(ratio, 0), 1)
  return `${(clamped * 100).toFixed(1)}%`
}

/** 成本展示：保留 6 位小数，极小额不显示为 0.000000。 */
export function formatCost(cost: number | undefined): string {
  if (cost === undefined || cost === null || !Number.isFinite(cost)) return '—'
  if (cost === 0) return '0'
  if (cost < 0.000001) return '<0.000001'
  return cost.toFixed(6).replace(/0+$/, '').replace(/\.$/, '')
}

/** token 数展示：千位分隔。 */
export function formatTokens(tokens: number | undefined): string {
  if (tokens === undefined || tokens === null || !Number.isFinite(tokens)) return '—'
  return tokens.toLocaleString('en-US')
}
