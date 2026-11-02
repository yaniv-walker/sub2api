<template>
  <div class="flex flex-col gap-4">
    <header class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
          {{ t('iqBenchmark.compare.title') }}
        </h3>
        <p class="text-xs text-gray-500 dark:text-gray-400">
          {{ t('iqBenchmark.compare.selected', { n: selectedIds.length }) }}
          <span class="text-gray-400"> / {{ MAX_COMPARE_ITEMS }}</span>
        </p>
      </div>
      <div class="flex items-center gap-2">
        <button
          class="btn btn-primary btn-sm"
          :disabled="selectedIds.length < 2 || loading"
          @click="$emit('compare')"
        >
          {{ t('iqBenchmark.compare.start') }}
        </button>
        <button class="btn btn-ghost btn-sm" :disabled="selectedIds.length === 0" @click="$emit('clear')">
          {{ t('iqBenchmark.compare.clear') }}
        </button>
      </div>
    </header>

    <!-- 提示：达到上限 -->
    <p
      v-if="selectedIds.length >= MAX_COMPARE_ITEMS"
      class="rounded-lg bg-amber-50 p-2 text-xs text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
    >
      {{ t('iqBenchmark.compare.limit', { n: MAX_COMPARE_ITEMS }) }}
    </p>

    <!-- 未开始对比 -->
    <div v-if="results.length === 0" class="card p-8 text-center text-sm text-gray-500 dark:text-gray-400">
      {{ t('iqBenchmark.compare.empty') }}
    </div>

    <!-- 对比结果：按题目分组，同题才能横向比较 -->
    <div v-else class="flex flex-col gap-6">
      <section v-for="group in groups" :key="group.caseId">
        <h4 class="mb-2 text-xs font-semibold uppercase tracking-wide text-gray-400">
          {{ group.caseName }}
        </h4>
        <div class="grid grid-cols-1 gap-3" :class="gridClass">
          <div
            v-for="item in group.items"
            :key="item.id"
            class="card flex flex-col overflow-hidden"
          >
            <div class="flex items-center justify-between gap-2 border-b border-gray-200 p-2 dark:border-dark-700">
              <span class="truncate text-xs font-semibold">{{ item.model }}</span>
              <button
                class="btn btn-ghost btn-sm px-1 py-0.5"
                :title="t('iqBenchmark.compare.remove')"
                @click="$emit('remove', item.id)"
              >
                <Icon name="x" size="xs" />
              </button>
            </div>

            <div class="flex min-h-[10rem] items-center justify-center bg-gray-50 p-2 dark:bg-dark-900">
              <p v-if="isBlocked(item)" class="p-4 text-center text-xs text-amber-600 dark:text-amber-400">
                {{ t('iqBenchmark.card.unsafe') }}
              </p>
              <img
                v-else-if="item.image_url"
                :src="item.image_url"
                :alt="item.case_name"
                referrerpolicy="no-referrer"
                class="max-h-56 object-contain"
              />
              <img
                v-else-if="svgURI(item)"
                :src="svgURI(item)"
                :alt="item.case_name"
                class="max-h-56 object-contain"
              />
              <pre
                v-else
                class="max-h-56 w-full overflow-auto whitespace-pre-wrap break-words text-[11px] leading-relaxed"
              >{{ truncateText(item.content, 400) }}</pre>
            </div>

            <dl class="flex flex-wrap gap-x-3 gap-y-1 p-2 text-[11px] text-gray-400">
              <div class="flex gap-1">
                <dt>{{ t('iqBenchmark.card.duration') }}:</dt>
                <dd class="tabular-nums">{{ formatDuration(item.duration_ms) }}</dd>
              </div>
              <div v-if="item.score !== undefined" class="flex gap-1">
                <dt>{{ t('iqBenchmark.card.score') }}:</dt>
                <dd class="tabular-nums">{{ item.score }}</dd>
              </div>
              <div class="flex gap-1">
                <dt>{{ t('iqBenchmark.admin.artifacts.status') }}:</dt>
                <dd>{{ t(`iqBenchmark.status.${item.status}`) }}</dd>
              </div>
            </dl>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { IQPublicArtifact } from './types'
import { formatDuration, groupByCase, MAX_COMPARE_ITEMS } from './filters'
import { svgToDataURI, truncateText } from './sanitize'

const props = defineProps<{
  selectedIds: number[]
  results: IQPublicArtifact[]
  loading?: boolean
}>()

defineEmits<{
  (e: 'compare'): void
  (e: 'clear'): void
  (e: 'remove', id: number): void
}>()

const { t } = useI18n()

const groups = computed(() => groupByCase(props.results))

/** 列数随对比数量自适应，最多 4 列避免卡片过窄。 */
const gridClass = computed(() => {
  const count = Math.min(props.results.length, 4)
  switch (count) {
    case 1:
      return 'sm:grid-cols-1'
    case 2:
      return 'sm:grid-cols-2'
    case 3:
      return 'sm:grid-cols-2 lg:grid-cols-3'
    default:
      return 'sm:grid-cols-2 lg:grid-cols-4'
  }
})

function isBlocked(item: IQPublicArtifact): boolean {
  return item.is_unsafe || item.status === 'blocked'
}

function svgURI(item: IQPublicArtifact): string {
  if (isBlocked(item)) return ''
  return svgToDataURI(item.content)
}
</script>
