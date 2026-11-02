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

        <div class="card overflow-x-auto">
          <table class="w-full text-xs">
            <thead>
              <tr class="border-b border-gray-200 text-left text-gray-400 dark:border-dark-700">
                <th class="p-2">{{ t('iqBenchmark.overview.model') }}</th>
                <th class="p-2">{{ t('iqBenchmark.overview.total') }}</th>
                <th class="p-2">{{ t('iqBenchmark.overview.successRate') }}</th>
                <th class="p-2">{{ t('iqBenchmark.overview.avgDuration') }}</th>
                <th class="p-2">{{ t('iqBenchmark.overview.lastRunAt') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="bucket in overview?.buckets ?? []"
                :key="bucket.model"
                class="border-b border-gray-100 last:border-0 dark:border-dark-800"
              >
                <td class="p-2 font-medium">{{ bucket.model }}</td>
                <td class="p-2 tabular-nums">{{ bucket.total }}</td>
                <td class="p-2 tabular-nums">
                  {{ formatPercent(successRate(bucket.success, bucket.total)) }}
                </td>
                <td class="p-2 tabular-nums">{{ formatDuration(bucket.avg_duration_ms) }}</td>
                <td class="p-2 tabular-nums">{{ formatTime(bucket.last_run_at) }}</td>
              </tr>
              <tr v-if="(overview?.buckets ?? []).length === 0">
                <td colspan="5" class="p-6 text-center text-gray-400">{{ t('iqBenchmark.overview.empty') }}</td>
              </tr>
            </tbody>
          </table>
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
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import ArtifactGrid from '@/features/iq-benchmark/ArtifactGrid.vue'
import ArtifactDetail from '@/features/iq-benchmark/ArtifactDetail.vue'
import ComparePanel from '@/features/iq-benchmark/ComparePanel.vue'
import FilterBar from '@/features/iq-benchmark/FilterBar.vue'
import UserTestDialog from '@/features/iq-benchmark/UserTestDialog.vue'
import { useArtifactFeed } from '@/features/iq-benchmark/useArtifactFeed'
import {
  fetchArtifacts,
  fetchCompare,
  fetchMyTargets,
  fetchOverview,
  fetchPublicCases,
} from '@/features/iq-benchmark/api'
import type {
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

onMounted(async () => {
  // 题目与概览是轻量请求，失败不影响主列表展示。
  const [caseList, overviewData] = await Promise.allSettled([fetchPublicCases(), fetchOverview()])
  if (caseList.status === 'fulfilled') cases.value = caseList.value
  if (overviewData.status === 'fulfilled') overview.value = overviewData.value

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
