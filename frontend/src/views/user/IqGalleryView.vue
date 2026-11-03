<template>
  <AppLayout>
    <div class="mx-auto flex w-full max-w-7xl flex-col gap-4 p-4">
      <!-- 标题 -->
      <header class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-lg font-semibold text-gray-900 dark:text-gray-100">{{ t('iqBenchmark.title') }}</h1>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('iqBenchmark.subtitle') }}</p>
        </div>
        <!--
          自测入口默认隐藏：它依赖后端 user_test_enabled，而那个开关不在
          公开接口里。先探测 /user-tests/targets，能拿到数据才显示按钮 ——
          否则用户在关闭自测的站点上点开对话框只会看到一片空。
        -->
        <button v-if="selfTestAvailable" class="btn btn-primary btn-sm" @click="showUserTest = true">
          {{ t('iqBenchmark.userTest.action') }}
        </button>
      </header>

      <!-- 限流提示：不自动重试，让用户自己决定何时再试 -->
      <div
        v-if="feed.error.value === 'rate-limited'"
        class="rounded-lg bg-amber-50 p-3 text-xs text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
      >
        {{
          feed.coolingDown.value
            ? t('iqBenchmark.error.rateLimited', { n: feed.cooldownSeconds.value })
            : t('iqBenchmark.error.rateLimitedShort')
        }}
      </div>
      <div
        v-else-if="feed.error.value === 'disabled'"
        class="rounded-lg bg-amber-50 p-3 text-xs text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
      >
        {{ t('iqBenchmark.error.disabled') }}
      </div>
      <div
        v-else-if="feed.error.value === 'failed'"
        class="flex items-center justify-between rounded-lg bg-red-50 p-3 text-xs text-red-600 dark:bg-red-900/30 dark:text-red-300"
      >
        <span>{{ t('iqBenchmark.error.loadFailed') }}</span>
        <button class="btn btn-ghost btn-sm" @click="feed.reloadNow(1)">{{ t('iqBenchmark.error.retry') }}</button>
      </div>

      <!-- 页签 -->
      <nav class="flex gap-1 border-b border-gray-200 dark:border-dark-700">
        <button
          v-for="tab in tabs"
          :key="tab"
          class="border-b-2 px-3 py-2 text-sm font-medium transition"
          :class="
            activeTab === tab
              ? 'border-primary-500 text-primary-600 dark:text-primary-400'
              : 'border-transparent text-gray-500 hover:text-gray-700 dark:hover:text-gray-300'
          "
          @click="activeTab = tab"
        >
          {{ t(`iqBenchmark.tab.${tab}`) }}
        </button>
      </nav>

      <!-- 作品展 -->
      <section v-show="activeTab === 'gallery'" class="flex flex-col gap-4">
        <FilterBar
          v-model="feed.filters"
          :model-options="feed.modelOptions.value"
          :case-options="cases"
          :loading="feed.loading.value"
          :cooldown-seconds="feed.cooldownSeconds.value"
          @refresh="feed.reloadNow(feed.page.value)"
          @reset="feed.resetFilters"
        />

        <ComparePanel
          v-if="selectedIds.length > 0"
          :selected-ids="selectedIds"
          :results="compareResults"
          :loading="comparing"
          @compare="runCompare"
          @clear="clearSelection"
          @remove="removeFromSelection"
        />

        <!--
          监控矩阵与作品列表**共用同一份 feed.filters**。
          这是刻意的：两处若各自维护筛选状态，用户切页签时看到的
          条件就会不一致，而「矩阵说 3 个模型挂了、列表却只有 1 个模型」
          这种矛盾比不提供矩阵更糟。
        -->
        <MonitorMatrix :matrix="matrix" :loading="feed.loading.value" @select="openArtifactByID" />

        <ArtifactGrid
          :items="feed.items.value"
          :loading="feed.loading.value"
          :total="feed.total.value"
          :page="feed.page.value"
          :page-size="feed.pageSize"
          :selected-ids="selectedIds"
          :show-reset="!feed.showingDefaultFilters.value"
          @detail="openDetail"
          @preview="openDetail"
          @toggle-compare="toggleCompare"
          @page="feed.goToPage"
          @reset="feed.resetFilters"
        />
      </section>

      <!-- 模型对比（聚合） -->
      <section v-show="activeTab === 'overview'" class="flex flex-col gap-4">
        <div class="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <div v-for="stat in overviewStats" :key="stat.label" class="stat-card">
            <span class="stat-label">{{ stat.label }}</span>
            <strong class="stat-value tabular-nums">{{ stat.value }}</strong>
          </div>
        </div>

        <!-- 分组作品墙：一堵墙一张表，按 group_id 分块 -->
        <div v-if="walls.length > 0" class="flex flex-col gap-4">
          <h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
            {{ t('iqBenchmark.wall.title') }}
          </h2>
          <div
            v-for="wall in walls"
            :key="wall.groupId"
            class="card overflow-x-auto"
          >
            <div class="flex items-center justify-between border-b border-gray-100 px-4 py-2 dark:border-dark-700">
              <h3 class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ wall.name }}</h3>
              <span class="text-xs text-gray-400">{{ wall.rows.length }}</span>
            </div>
            <table class="w-full text-xs">
              <thead>
                <tr class="border-b border-gray-200 text-left text-gray-400 dark:border-dark-700">
                  <th class="p-2">{{ t('iqBenchmark.overview.model') }}</th>
                  <th class="p-2">{{ t('iqBenchmark.overview.total') }}</th>
                  <th class="p-2">{{ t('iqBenchmark.overview.successRate') }}</th>
                  <!--
                    通过率与判定通过率并列显示，而不是只留一个。
                    两者的差值就是「调用成功但答错」的比例 ——
                    模型变笨时正是这个差值先变大。
                  -->
                  <th class="p-2">{{ t('iqBenchmark.verdict.label') }}</th>
                  <th class="p-2">{{ t('iqBenchmark.overview.avgDuration') }}</th>
                  <th class="p-2">{{ t('iqBenchmark.overview.lastRunAt') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="bucket in wall.rows"
                  :key="bucket.model"
                  class="border-b border-gray-100 last:border-0 dark:border-dark-800"
                >
                  <td class="p-2 font-medium">{{ bucket.model }}</td>
                  <td class="p-2 tabular-nums">{{ bucket.total }}</td>
                  <td class="p-2 tabular-nums">
                    {{ formatPercent(successRate(bucket.success, bucket.total)) }}
                  </td>
                  <td class="p-2 tabular-nums">
                    <span
                      class="rounded px-1.5 py-0.5 text-[11px]"
                      :class="verdictClass(bucket.verdict)"
                    >
                      {{ t(`iqBenchmark.verdict.state.${bucket.verdict}`) }}
                    </span>
                    <span v-if="bucket.logic_total > 0" class="ml-1 text-gray-400">
                      {{ formatPercent(bucket.passed / bucket.logic_total) }}
                    </span>
                  </td>
                  <td class="p-2 tabular-nums">{{ formatDuration(bucket.avg_duration_ms) }}</td>
                  <td class="p-2 tabular-nums">{{ formatTime(bucket.last_run_at) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
        <div v-else class="card p-6 text-center text-xs text-gray-400">
          {{ t('iqBenchmark.overview.empty') }}
        </div>
      </section>
    </div>

    <!-- 详情弹层 -->
    <ArtifactDetail :artifact="detailTarget" @close="detailTarget = null" />

    <!-- 自测弹层 -->
    <UserTestDialog
      v-if="showUserTest"
      :require-review="true"
      @close="showUserTest = false"
      @submitted="onUserTestSubmitted"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import ArtifactGrid from '@/features/iq-benchmark/ArtifactGrid.vue'
import ArtifactDetail from '@/features/iq-benchmark/ArtifactDetail.vue'
import ComparePanel from '@/features/iq-benchmark/ComparePanel.vue'
import FilterBar from '@/features/iq-benchmark/FilterBar.vue'
import MonitorMatrix from '@/features/iq-benchmark/MonitorMatrix.vue'
import UserTestDialog from '@/features/iq-benchmark/UserTestDialog.vue'
import { useArtifactFeed } from '@/features/iq-benchmark/useArtifactFeed'
import {
  fetchArtifact,
  fetchArtifacts,
  fetchCompare,
  fetchMatrix,
  fetchMyTargets,
  fetchOverview,
  fetchPublicCases,
} from '@/features/iq-benchmark/api'
import type {
  IQMatrix,
  IQPublicArtifact,
  IQPublicCase,
  IQPublicOverview,
} from '@/features/iq-benchmark/types'
import {
  formatDuration,
  formatPercent,
  successRate,
  toggleCompareSelection,
} from '@/features/iq-benchmark/filters'

const { t, locale } = useI18n()

const tabs = ['gallery', 'overview'] as const
type Tab = (typeof tabs)[number]
const activeTab = ref<Tab>('gallery')

/** 作品流（防抖 + 单飞 + 限流冷却都在这里）。 */
const feed = useArtifactFeed<IQPublicArtifact>({
  fetcher: (filters) => fetchArtifacts(filters),
  pageSize: 20,
})

const cases = ref<IQPublicCase[]>([])
const overview = ref<IQPublicOverview | null>(null)
const detailTarget = ref<IQPublicArtifact | null>(null)

/**
 * 监控矩阵数据。
 *
 * 用空矩阵而不是 null 作为初值：模板里就不必到处写 `matrix?.`，
 * 而「还没加载」与「加载了但没有数据」在渲染上是同一件事（一片灰）。
 */
const matrix = ref<IQMatrix>({ cases: [], models: [], cells: [], total: 0, truncated: false })

/**
 * 矩阵与列表共用 feed.filters，因此在筛选变化后必须重新拉矩阵。
 *
 * 单独 watch 而不是塞进 useArtifactFeed：那个 composable 的职责是
 * 「分页列表的单飞 + 防抖 + 限流冷却」，矩阵既不分页、也没有自己的
 * 加载态，硬塞进去会让它的状态机变复杂。代价是矩阵每次筛选都发一次
 * 请求 —— 这正是期望行为（矩阵与列表必须同步）。
 */
watch(
  () => ({ ...feed.filters }),
  async () => {
    await reloadMatrix()
  },
  { deep: true },
)

/** 拉取矩阵；失败时保留上一次的数据而不是清空（避免闪成空白）。 */
async function reloadMatrix(): Promise<void> {
  try {
    matrix.value = await fetchMatrix(feed.filters)
  } catch {
    // 静默失败：矩阵是辅助看板，它挂了不该盖住主列表的错误提示。
    // 保留旧数据总比闪成「没有数据」诚实。
  }
}

/**
 * 从矩阵格子点开作品详情。
 *
 * 矩阵只带回 artifact_id，所以要单独取一次完整对象 ——
 * 让它复用 openDetail 的同一份弹层，而不是再写一个详情视图。
 */
async function openArtifactByID(artifactId: number): Promise<void> {
  try {
    const artifact = await fetchArtifact(artifactId)
    if (artifact) openDetail(artifact)
  } catch {
    // 作品可能刚被删除或转为待审：此时不弹详情即可，不值得报错。
  }
}

/** 自测入口是否可用（探测 /user-tests/targets 的结果）。 */
const selfTestAvailable = ref(false)
const showUserTest = ref(false)

/** 已选对比的作品 ID。 */
const selectedIds = ref<number[]>([])
const compareResults = ref<IQPublicArtifact[]>([])
const comparing = ref(false)

const overviewStats = computed(() => [
  { label: t('iqBenchmark.overview.totalArtifacts'), value: String(overview.value?.total_artifacts ?? 0) },
  { label: t('iqBenchmark.overview.imageArtifacts'), value: String(overview.value?.image_artifacts ?? 0) },
  { label: t('iqBenchmark.overview.textArtifacts'), value: String(overview.value?.text_artifacts ?? 0) },
  { label: t('iqBenchmark.overview.caseCount'), value: String(overview.value?.case_count ?? 0) },
])

/**
 * 把扁平的分桶按分组拆成「墙」。
 *
 * 后端已经保证只在白名单内的分组才会出现，且 group_name 是管理员
 * 自填的展示名（可能为空）。这里**不**为空的展示名编造一个 ——
 * 空名字意味着管理员还没给它起名，前端随便补一个反而会掩盖配置缺失。
 * 这种情况下用分组 ID 兜底，至少能让人去后台找到那一行。
 */
const walls = computed(() => {
  const buckets = overview.value?.buckets ?? []
  const grouped = new Map<number, { groupId: number; name: string; rows: typeof buckets }>()
  for (const bucket of buckets) {
    const groupId = bucket.group_id ?? 0
    const existing = grouped.get(groupId)
    if (existing) {
      existing.rows.push(bucket)
      continue
    }
    grouped.set(groupId, {
      groupId,
      name: bucket.group_name?.trim() || `#${groupId}`,
      rows: [bucket],
    })
  }
  return [...grouped.values()]
})

/** 判定四态的徽标配色。 */
function verdictClass(verdict: string): string {
  switch (verdict) {
    case 'ok':
    case 'normal':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'degraded':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
    case 'generated':
      return 'bg-sky-100 text-sky-700 dark:bg-sky-900/40 dark:text-sky-300'
    default:
      return 'bg-gray-100 text-gray-500 dark:bg-dark-800 dark:text-gray-400'
  }
}

onMounted(async () => {
  // 题目与概览是轻量请求，失败不影响主列表展示。
  const [caseList, overviewData] = await Promise.allSettled([fetchPublicCases(), fetchOverview()])
  if (caseList.status === 'fulfilled') cases.value = caseList.value
  if (overviewData.status === 'fulfilled') overview.value = overviewData.value

  // 矩阵与列表同源，首屏必须一起拉：等到 watch 首次触发是不行的
  // （filters 在初值上不会变化，watch 不会跑），那样首屏矩阵永远是空的。
  await reloadMatrix()

  // 探测自测可用性。失败（含 404 = 未开启）一律当作不可用：
  // 这里刻意**吞掉错误**，因为它只是决定要不要显示一个按钮，
  // 不值得在作品展顶部弹一条红色错误。
  try {
    const targets = await fetchMyTargets()
    selfTestAvailable.value = targets.groups.length > 0
  } catch {
    selfTestAvailable.value = false
  }
})

/** 自测提交成功：关弹层并刷新列表（source 切到「全部」才看得到自测产出）。 */
async function onUserTestSubmitted(): Promise<void> {
  showUserTest.value = false
  // 自测产出默认被 source=scheduled 过滤掉，所以主动切到 all，
  // 否则用户提交完看不到自己刚测的东西，会以为失败了。
  // filters 是 reactive 对象，必须就地赋值（重新绑定会让 v-model 断链）。
  feed.filters.source = 'all'
  await feed.reloadNow(1)
}

function openDetail(artifact: IQPublicArtifact): void {
  detailTarget.value = artifact
}

function toggleCompare(artifact: IQPublicArtifact): void {
  const result = toggleCompareSelection(selectedIds.value, artifact.id)
  if (result.reason === 'limit') {
    // 超上限不静默丢弃，给出明确提示。
    alert(t('iqBenchmark.compare.limit', { n: 8 }))
    return
  }
  selectedIds.value = result.selected
  // 取消选择时同步移出已有结果，避免对比区显示已取消的作品。
  if (!result.added) {
    compareResults.value = compareResults.value.filter((item) => item.id !== artifact.id)
  }
}

function removeFromSelection(id: number): void {
  selectedIds.value = selectedIds.value.filter((item) => item !== id)
  compareResults.value = compareResults.value.filter((item) => item.id !== id)
}

function clearSelection(): void {
  selectedIds.value = []
  compareResults.value = []
}

async function runCompare(): Promise<void> {
  if (selectedIds.value.length < 2) return
  comparing.value = true
  try {
    compareResults.value = await fetchCompare(selectedIds.value)
  } catch {
    compareResults.value = []
  } finally {
    comparing.value = false
  }
}

function formatTime(raw: string | undefined): string {
  if (!raw) return '—'
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}
</script>
