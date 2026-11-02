<template>
  <div>
    <!-- 加载骨架：固定高度避免布局跳动 -->
    <div
      v-if="loading && items.length === 0"
      class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4"
    >
      <div
        v-for="n in skeletonCount"
        :key="`skeleton-${n}`"
        class="card animate-pulse overflow-hidden"
      >
        <div class="h-48 bg-gray-100 dark:bg-dark-800"></div>
        <div class="space-y-2 p-3">
          <div class="h-3 w-2/3 rounded bg-gray-100 dark:bg-dark-800"></div>
          <div class="h-3 w-1/2 rounded bg-gray-100 dark:bg-dark-800"></div>
        </div>
      </div>
    </div>

    <!-- 空态 -->
    <div v-else-if="items.length === 0" class="card flex flex-col items-center gap-2 p-10 text-center">
      <Icon name="inbox" size="lg" class="text-gray-300 dark:text-dark-600" />
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ emptyLabel }}</p>
      <button v-if="showReset" class="btn btn-secondary btn-sm mt-2" @click="$emit('reset')">
        {{ t('iqBenchmark.filter.reset') }}
      </button>
    </div>

    <!-- 网格 -->
    <div v-else class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
      <ArtifactCard
        v-for="item in items"
        :key="item.id"
        :artifact="item"
        :selectable="selectable"
        :selected="selectedIds.includes(item.id)"
        @detail="$emit('detail', $event)"
        @preview="$emit('preview', $event)"
        @toggle-compare="$emit('toggle-compare', $event)"
      />
    </div>

    <!-- 分页 -->
    <div v-if="total > pageSize" class="mt-4 flex items-center justify-between gap-2">
      <span class="text-xs text-gray-400">
        {{ (page - 1) * pageSize + 1 }} - {{ Math.min(page * pageSize, total) }} / {{ total }}
      </span>
      <div class="flex items-center gap-2">
        <button class="btn btn-secondary btn-sm" :disabled="page <= 1 || loading" @click="$emit('page', page - 1)">
          <Icon name="chevronLeft" size="sm" />
        </button>
        <span class="text-xs tabular-nums text-gray-500">{{ page }} / {{ totalPages }}</span>
        <button
          class="btn btn-secondary btn-sm"
          :disabled="page >= totalPages || loading"
          @click="$emit('page', page + 1)"
        >
          <Icon name="chevronRight" size="sm" />
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ArtifactCard from './ArtifactCard.vue'
import type { IQPublicArtifact } from './types'

const props = withDefaults(
  defineProps<{
    items: IQPublicArtifact[]
    loading?: boolean
    total?: number
    page?: number
    pageSize?: number
    /** 是否允许选择对比。 */
    selectable?: boolean
    selectedIds?: number[]
    /** 是否处于「有筛选但仍为空」的状态，用于显示清除筛选按钮。 */
    showReset?: boolean
  }>(),
  {
    loading: false,
    total: 0,
    page: 1,
    pageSize: 20,
    selectable: true,
    selectedIds: () => [],
    showReset: false,
  },
)

defineEmits<{
  (e: 'detail', artifact: IQPublicArtifact): void
  (e: 'preview', artifact: IQPublicArtifact): void
  (e: 'toggle-compare', artifact: IQPublicArtifact): void
  (e: 'page', page: number): void
  (e: 'reset'): void
}>()

const { t } = useI18n()

const skeletonCount = 8

/** 总页数（至少 1，避免显示 0/0）。 */
const totalPages = computed(() => Math.max(1, Math.ceil(props.total / Math.max(props.pageSize, 1))))

/** 空态文案按「是否筛选过」区分。 */
const emptyLabel = computed(() =>
  props.showReset ? t('iqBenchmark.empty.filtered') : t('iqBenchmark.empty.artifacts'),
)
</script>
