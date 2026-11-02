/**
 * 作品流组合式函数：加载、筛选、分页、防抖、单飞、限流冷却。
 *
 * 为什么把逻辑集中在这里而不是散在视图里：
 *   - 防抖与单飞是限流的**前端配套措施**，必须保证每个入口都生效；
 *   - 用户侧与管理侧共用同一套加载逻辑（只有 fetcher 不同），
 *     集中后两边行为天然一致。
 */
import { computed, reactive, ref, watch, onScopeDispose } from 'vue'
import type { Ref } from 'vue'
import {
  defaultFilterState,
  deriveModelOptions,
  filterSignature,
  isDefaultFilterState,
  toArtifactFilters,
} from './filters'
import type { IQFilterState } from './filters'
import { isInCooldown, parseRateLimitError, remainingCooldownSeconds, SingleFlight } from './rateLimit'
import type { IQPage, IQArtifactFilters } from './types'

/** 防抖延迟：不低于 300ms，否则用户连续输入会产生大量请求。 */
export const FILTER_DEBOUNCE_MS = 300

/** 作品流所需的通用字段。 */
interface FeedItem {
  id: number
  model: string
  case_id: number
}

export interface UseArtifactFeedOptions<T extends FeedItem> {
  /** 拉取函数（注入后本组合式函数与具体 API 解耦，便于测试）。 */
  fetcher: (filters: IQArtifactFilters) => Promise<IQPage<T>>
  /** 每页条数。 */
  pageSize?: number
  /** 防抖毫秒数。 */
  debounceMs?: number
  /** 是否在创建时立即加载。 */
  immediate?: boolean
}

export function useArtifactFeed<T extends FeedItem>(options: UseArtifactFeedOptions<T>) {
  const pageSize = options.pageSize ?? 20
  const debounceMs = options.debounceMs ?? FILTER_DEBOUNCE_MS

  const filters = reactive<IQFilterState>(defaultFilterState())
  const items = ref([]) as Ref<T[]>
  const total = ref(0)
  const page = ref(1)
  const loading = ref(false)
  const error = ref<string | null>(null)
  /** 被限流时记录时间戳，用于冷却计时。 */
  const limitedAt = ref<number | null>(null)
  const retryAfterSeconds = ref(0)

  const flight = new SingleFlight()
  let debounceTimer: ReturnType<typeof setTimeout> | null = null

  const modelOptions = computed(() => deriveModelOptions(items.value.map((item) => item.model)))
  const hasMore = computed(() => items.value.length < total.value)
  const showingDefaultFilters = computed(() => isDefaultFilterState(filters))

  /** 冷却中则无法加载。 */
  const coolingDown = computed(() =>
    isInCooldown(limitedAt.value, retryAfterSeconds.value, Date.now()),
  )

  /** 剩余冷却秒数（视图层用于展示倒计时）。 */
  const cooldownSeconds = computed(() =>
    remainingCooldownSeconds(limitedAt.value, retryAfterSeconds.value, Date.now()),
  )

  /** 实际发起一次加载。 */
  async function load(targetPage = page.value): Promise<void> {
    if (isInCooldown(limitedAt.value, retryAfterSeconds.value)) {
      return
    }
    const params = toArtifactFilters(filters, new Date(), targetPage, pageSize)
    // 相同参数复用进行中的请求；不同参数各自独立。
    const key = JSON.stringify(params)
    loading.value = true
    error.value = null
    try {
      const result = await flight.run(key, () => options.fetcher(params))
      items.value = result.items as T[]
      total.value = result.total
      page.value = result.page || targetPage
    } catch (err) {
      const info = parseRateLimitError(err)
      if (info.limited) {
        limitedAt.value = Date.now()
        retryAfterSeconds.value = info.retryAfterSeconds
        error.value = 'rate-limited'
      } else if (info.featureDisabled) {
        error.value = 'disabled'
      } else {
        error.value = 'failed'
      }
    } finally {
      loading.value = false
    }
  }

  /**
   * 防抖加载：筛选变化时调用。
   *
   * 连续变化只在最后一次触发后 debounceMs 执行一次，
   * 从而避免「每敲一个字符发一次请求」。
   */
  function reload(): void {
    if (debounceTimer !== null) {
      clearTimeout(debounceTimer)
    }
    debounceTimer = setTimeout(() => {
      debounceTimer = null
      page.value = 1
      void load(1)
    }, debounceMs)
  }

  /** 立即加载（翻页、手动刷新用；不防抖）。 */
  function reloadNow(targetPage = page.value): void {
    if (debounceTimer !== null) {
      clearTimeout(debounceTimer)
      debounceTimer = null
    }
    void load(targetPage)
  }

  /** 清除全部筛选并重新加载。 */
  function resetFilters(): void {
    Object.assign(filters, defaultFilterState())
    reload()
  }

  /** 上一页 / 下一页。 */
  function goToPage(target: number): void {
    if (target < 1) return
    reloadNow(target)
  }

  // 筛选签名变化时触发防抖重载；用签名避免「引用变但值没变」的抖动。
  watch(
    () => filterSignature(filters),
    (next, prev) => {
      if (next === prev) return
      reload()
    },
  )

  onScopeDispose(() => {
    if (debounceTimer !== null) {
      clearTimeout(debounceTimer)
      debounceTimer = null
    }
    flight.clear()
  })

  if (options.immediate !== false) {
    void load(1)
  }

  return {
    filters,
    items,
    total,
    page,
    pageSize,
    loading,
    error,
    modelOptions,
    hasMore,
    showingDefaultFilters,
    coolingDown,
    cooldownSeconds,
    load,
    reload,
    reloadNow,
    resetFilters,
    goToPage,
  }
}
