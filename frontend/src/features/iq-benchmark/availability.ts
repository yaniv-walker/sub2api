/**
 * 作品展的可用性探测（用于侧栏与路由级的显示判断）。
 *
 * ## 为什么不用 featureFlags.ts 的注册表
 *
 * 上游的 `utils/featureFlags.ts` 依赖后端 public settings 注入的字段，新增一个
 * 标志要同步改后端 **11 处**（见该文件头部的「Adding a new flag」清单）。
 * 本功能有硬约束：既有文件改动限「注册一行」级别，因此不能走那条路。
 *
 * ## 这里怎么做
 *
 * 直接探一次公开接口：
 *   - 功能启用时 `GET /iq-gallery/overview` 返回 200；
 *   - 功能未启用时后端**根本没注册该路由**，返回 404。
 *
 * 探测结果在会话内缓存（含 in-flight Promise 合并），因此侧栏与页面共享一次请求。
 * 判定是 **fail-closed**：探测失败一律视为「未启用」，与后端 404 行为一致。
 */
import { ref } from 'vue'
import { fetchOverview } from './api'

/** 最近一次探测结论：null = 尚未探测完成。 */
const available = ref<boolean | null>(null)

/** 进行中的探测（避免并发重复请求）。 */
let inflight: Promise<boolean> | null = null

/**
 * 探测用户侧作品展是否可用。
 *
 * @param force 为 true 时忽略缓存重新探测（管理员刚开启功能后可调用）。
 */
export async function probeIqGalleryAvailability(force = false): Promise<boolean> {
  if (!force && available.value !== null) {
    return available.value
  }
  if (!force && inflight) {
    return inflight
  }
  inflight = (async () => {
    try {
      await fetchOverview()
      available.value = true
    } catch {
      // 404（未注册）/ 503（未启用）/ 网络错误 一律视为不可用。
      available.value = false
    } finally {
      inflight = null
    }
    return available.value === true
  })()
  return inflight
}

/**
 * 侧栏使用的响应式可用性标记。
 *
 * 首次调用时触发一次探测；未探测完成前返回 false（不闪出菜单再消失）。
 */
export function useIqGalleryAvailable() {
  if (available.value === null && !inflight) {
    void probeIqGalleryAvailability()
  }
  return available
}

/** 重置缓存（管理员在设置里切换开关后调用）。 */
export function resetIqGalleryAvailability(): void {
  available.value = null
  inflight = null
}
