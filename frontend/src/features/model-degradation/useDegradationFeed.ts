/**
 * 页面数据编排：题库 / 统计 / 历史列表 / 当前检测 + 轮询。
 *
 * 轮询参数对齐参考实现：每 3 秒一次，最多 240 次（12 分钟）。
 * 上限的意义不是「省请求」，而是防止一次卡死的上游让页面永远转圈 ——
 * 到点就停，并明确告诉用户「已停止等待」。
 *
 * 生成题超时最长可到 60 分钟 > 12 分钟，因此到上限时**不中止**后端任务，
 * 只是前端不再等：用户仍可在历史里点回来继续看。中止是用户的动作，
 * 不该由前端计时器代劳。
 */
import { computed, onBeforeUnmount, ref } from 'vue'
import {
  cancelRun as apiCancelRun,
  fetchRunDetail,
  fetchRuns,
  fetchStats,
  fetchTaskBank,
  startRun
} from './api'
import type { MDRun, MDRunDetail, MDStats, MDTaskBank } from './types'
import type { MDStartInput } from './types'

/** 轮询间隔（毫秒），对齐参考实现。 */
const POLL_INTERVAL_MS = 3000

/** 最大轮询次数，对齐参考实现。 */
const MAX_POLLS = 240

/** 每页条数。 */
export const PAGE_SIZE = 12

export function useDegradationFeed() {
  const bank = ref<MDTaskBank>({ logic: [], render: [] })
  const bankLoading = ref(true)
  const bankFailed = ref(false)

  const stats = ref<MDStats>({ pass_rate: 0, last_duration_ms: 0, run_count: 0 })

  const runs = ref<MDRun[]>([])
  const total = ref(0)
  const page = ref(1)
  const listLoading = ref(false)
  const listError = ref('')

  /** 判定过滤。 */
  const verdictFilter = ref('')
  /** 题型过滤。 */
  const typeFilter = ref('')

  /** 当前查看的检测。 */
  const current = ref<MDRunDetail | null>(null)
  const detailLoading = ref(false)
  const detailError = ref('')

  /** 已用时（运行中每秒刷新）。 */
  const elapsedMs = ref(0)

  /** 开始时间，用于算已用时。 */
  let startedAt = 0
  let pollTimer: number | null = null
  let elapsedTimer: number | null = null
  let pollCount = 0
  /** 轮询到上限后的提示。 */
  const pollExhausted = ref(false)

  /** 是否有正在跑的自有检测。 */
  const busy = computed(() => current.value?.status === 'running')

  /** 加载题库。题库加载失败不阻断：用户仍可写自定义题。 */
  async function loadBank() {
    bankLoading.value = true
    bankFailed.value = false
    try {
      bank.value = await fetchTaskBank()
    } catch {
      bankFailed.value = true
      bank.value = { logic: [], render: [] }
    } finally {
      bankLoading.value = false
    }
  }

  /** 加载统计。失败保持零值，不弹错。 */
  async function loadStats() {
    try {
      stats.value = await fetchStats(false)
    } catch {
      stats.value = { pass_rate: 0, last_duration_ms: 0, run_count: 0 }
    }
  }

  /** 加载历史列表。 */
  async function loadRuns() {
    listLoading.value = true
    listError.value = ''
    try {
      const result = await fetchRuns({
        limit: PAGE_SIZE,
        offset: (page.value - 1) * PAGE_SIZE,
        verdict: verdictFilter.value || undefined,
        task_type: typeFilter.value || undefined
      })
      runs.value = result.items ?? []
      total.value = result.total ?? 0
    } catch {
      listError.value = 'loadFailed'
      runs.value = []
      total.value = 0
    } finally {
      listLoading.value = false
    }
  }

  /** 切页。 */
  async function goPage(next: number) {
    page.value = Math.max(1, next)
    await loadRuns()
  }

  /** 改判定过滤（回到第一页）。 */
  async function setVerdictFilter(value: string) {
    verdictFilter.value = value
    page.value = 1
    await loadRuns()
  }

  /** 改题型过滤（回到第一页）。 */
  async function setTypeFilter(value: string) {
    typeFilter.value = value
    page.value = 1
    await loadRuns()
  }

  /** 选中一条检测并加载详情。 */
  async function select(id: number) {
    detailLoading.value = true
    detailError.value = ''
    stopPolling()
    try {
      const detail = await fetchRunDetail(id)
      current.value = detail
      elapsedMs.value = detail.duration_ms
      if (detail.status === 'running') {
        // 从创建时间起算，避免切回来时进度条从 0 开始。
        startedAt = new Date(detail.created_at).getTime()
        startPolling(id)
      }
    } catch {
      detailError.value = 'loadFailed'
      current.value = null
    } finally {
      detailLoading.value = false
    }
  }

  /** 发起一次检测。 */
  async function submit(payload: MDStartInput): Promise<boolean> {
    detailError.value = ''
    detailLoading.value = true
    stopPolling()
    try {
      const detail = await startRun(payload)
      current.value = detail
      elapsedMs.value = 0
      startedAt = Date.now()
      startPolling(detail.id)
      // 新记录入列，让用户立刻在历史里看到自己这次。
      await Promise.all([loadRuns(), loadStats()])
      return true
    } catch (err) {
      detailError.value = classifySubmitError(err)
      return false
    } finally {
      detailLoading.value = false
    }
  }

  /**
   * 把提交失败归类成 i18n 错误码。
   *
   * 409 是并发闸门（同一用户已有检测在跑），要单独说清楚，
   * 否则用户只会看到一句「失败」，再点一次还是失败。
   */
  function classifySubmitError(err: unknown): string {
    const status = (err as { response?: { status?: number } })?.response?.status
    if (status === 409) return 'startConflict'
    if (status === 400) return 'startInvalid'
    return 'startFailed'
  }

  /** 中止当前检测。 */
  async function cancel() {
    const id = current.value?.id
    if (!id) return
    try {
      await apiCancelRun(id)
      await refreshOnce(id)
      await Promise.all([loadRuns(), loadStats()])
    } catch {
      detailError.value = 'cancelFailed'
    }
  }

  /** 只拉一次详情，不重启轮询。 */
  async function refreshOnce(id: number) {
    try {
      const detail = await fetchRunDetail(id)
      current.value = detail
      elapsedMs.value = detail.duration_ms
    } catch {
      // 单次刷新失败留待下次轮询，不打断用户。
    }
  }

  /** 开始轮询。 */
  function startPolling(id: number) {
    stopPolling()
    pollCount = 0
    pollExhausted.value = false

    pollTimer = window.setInterval(async () => {
      pollCount += 1
      if (pollCount > MAX_POLLS) {
        stopPolling()
        pollExhausted.value = true
        return
      }

      try {
        const detail = await fetchRunDetail(id)
        current.value = detail
        elapsedMs.value = detail.duration_ms
        if (detail.status !== 'running') {
          stopPolling()
          // 结束后刷新统计与列表（判定变了）。
          await Promise.all([loadRuns(), loadStats()])
        }
      } catch {
        // 网络抖动不算致命：下一拍再试，不要中断轮询。
      }
    }, POLL_INTERVAL_MS)

    elapsedTimer = window.setInterval(() => {
      if (current.value?.status === 'running' && startedAt > 0) {
        elapsedMs.value = Date.now() - startedAt
      }
    }, 1000)
  }

  /** 停止轮询与计时。 */
  function stopPolling() {
    if (pollTimer !== null) {
      window.clearInterval(pollTimer)
      pollTimer = null
    }
    if (elapsedTimer !== null) {
      window.clearInterval(elapsedTimer)
      elapsedTimer = null
    }
  }

  /** 首次加载。 */
  async function bootstrap() {
    await Promise.all([loadBank(), loadStats(), loadRuns()])
  }

  // 离开页面必须停掉计时器，否则路由切走后仍在打接口。
  onBeforeUnmount(stopPolling)

  return {
    bank,
    bankLoading,
    bankFailed,
    stats,
    runs,
    total,
    page,
    listLoading,
    listError,
    verdictFilter,
    typeFilter,
    current,
    detailLoading,
    detailError,
    elapsedMs,
    pollExhausted,
    busy,
    bootstrap,
    loadBank,
    loadRuns,
    loadStats,
    goPage,
    setVerdictFilter,
    setTypeFilter,
    select,
    submit,
    cancel
  }
}
