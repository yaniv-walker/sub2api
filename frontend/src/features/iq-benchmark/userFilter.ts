/**
 * 管理端用户列表的「隐藏内部账号」开关。
 *
 * ## 背景
 *
 * 作品展需要一个内部账号（`notes` 前缀 `iq-benchmark:internal-agent`）来承载
 * 检测流量。它出现在管理端用户列表里会让管理员困惑，也不该被误操作禁用/删除。
 *
 * ## 约束
 *
 * 用户已确认：**不改上游用户查询**，只在管理端列表做前端过滤，且默认开启隐藏。
 * 因此过滤发生在前端已取回的当页数据上——这带来一个必须说清的局限：
 *
 *   - 隐藏只在当前页内生效，分页总数与页码仍按未过滤的集合计算；
 *   - 若某页所有行都被隐藏，该页会显示为空（不会自动跳页）。
 *
 * 这与「不改上游查询」的取舍一致：宁可接受这一处观感缺陷，也不改上游的
 * `ListWithFilters` 语义。
 */
import { ref, watch } from 'vue'

/** 与后端 `InternalAgentNotesPrefix` 保持一致（唯一的识别依据）。 */
export const INTERNAL_AGENT_NOTES_PREFIX = 'iq-benchmark:internal-agent'

/** localStorage 键，沿用管理端偏好的一般命名风格。 */
const STORAGE_KEY = 'sub2api.admin.users.hideInternalAccounts'

/**
 * 判断一行是否为作品展内部账号。
 *
 * 只认 `notes` 前缀，与后端 `IsInternalAgentNotes` 同一判定依据；
 * 不依赖邮箱、用户名或角色，避免与真实用户混淆。
 */
export function isInternalAccount(user: { notes?: string | null }): boolean {
  const notes = (user.notes ?? '').trim()
  return notes.startsWith(INTERNAL_AGENT_NOTES_PREFIX)
}

/**
 * 从列表中移除内部账号（默认行为）。
 *
 * @param rows 已取回的用户行
 * @param hide 为 false 时原样返回（管理员显式要求查看内部账号）
 */
export function filterInternalAccounts<T extends { notes?: string | null }>(
  rows: T[],
  hide: boolean,
): T[] {
  if (!hide) return rows
  return rows.filter((row) => !isInternalAccount(row))
}

/** 读取持久化偏好；读取失败（隐私模式/无 localStorage）时回退到默认值 true。 */
function readStored(): boolean {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    // 只有显式存过 'false' 才认为管理员关闭了隐藏。
    if (raw === null) return true
    return raw !== 'false'
  } catch {
    return true
  }
}

function writeStored(value: boolean): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, String(value))
  } catch {
    // 写失败不影响当前会话内的开关状态。
  }
}

/** 模块级共享状态：多个组件实例看到同一个开关。 */
const hideInternal = ref<boolean>(readStored())

watch(hideInternal, (value) => writeStored(value))

/**
 * 管理端用户列表使用的隐藏开关。
 *
 * 默认 `true`（隐藏），符合「内部账号不该干扰日常管理」的预期。
 */
export function useHideInternalAccounts() {
  return hideInternal
}
