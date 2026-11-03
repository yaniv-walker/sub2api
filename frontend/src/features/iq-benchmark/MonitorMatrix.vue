<template>
  <div class="card overflow-hidden">
    <!-- 表头：标题 + 截断提示 -->
    <div class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 px-4 py-3 dark:border-dark-700">
      <div class="flex items-center gap-2">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
          {{ t('iqBenchmark.matrix.title') }}
        </h3>
        <span class="text-xs text-gray-400">{{ t('iqBenchmark.matrix.subtitle') }}</span>
      </div>
      <div class="flex items-center gap-3">
        <!-- 图例：颜色只在图里有意义，必须有文字对照 -->
        <div class="hidden items-center gap-2 sm:flex">
          <span
            v-for="legend in legends"
            :key="legend.state"
            class="flex items-center gap-1 text-[11px] text-gray-500 dark:text-gray-400"
          >
            <span class="h-2.5 w-2.5 rounded-sm" :class="cellClass(legend.state)"></span>
            {{ legend.label }}
          </span>
        </div>
        <span v-if="matrix.truncated" class="text-[11px] text-amber-600 dark:text-amber-400">
          {{ t('iqBenchmark.matrix.truncated', { shown: matrix.total }) }}
        </span>
      </div>
    </div>

    <!-- 空态：没有数据时不要渲染一个空表格 -->
    <div v-if="matrix.cases.length === 0 || matrix.models.length === 0" class="flex flex-col items-center gap-2 px-4 py-10 text-center">
      <Icon name="inbox" size="lg" class="text-gray-300 dark:text-dark-600" />
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('iqBenchmark.matrix.empty') }}</p>
    </div>

    <div v-else class="overflow-x-auto">
      <table class="w-full border-collapse text-xs">
        <thead>
          <tr>
            <!-- 左上角单元格同时标注两个轴，避免读者猜哪边是模型 -->
            <th class="sticky left-0 z-10 bg-white px-3 py-2 text-left font-medium text-gray-400 dark:bg-dark-900">
              {{ t('iqBenchmark.matrix.caseAxis') }} \ {{ t('iqBenchmark.matrix.modelAxis') }}
            </th>
            <th
              v-for="model in matrix.models"
              :key="model"
              class="max-w-[9rem] truncate px-2 py-2 text-center font-medium text-gray-600 dark:text-gray-300"
              :title="model"
            >
              {{ model }}
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="axis in matrix.cases" :key="axis.id" class="border-t border-gray-50 dark:border-dark-800">
            <th
              class="sticky left-0 z-10 max-w-[12rem] truncate bg-white px-3 py-1.5 text-left font-normal text-gray-700 dark:bg-dark-900 dark:text-gray-300"
              :title="axis.name"
            >
              {{ axis.name }}
            </th>
            <td v-for="model in matrix.models" :key="model" class="px-1 py-1 text-center">
              <button
                type="button"
                class="h-7 w-full min-w-[3rem] rounded-sm text-[11px] transition hover:ring-2 hover:ring-primary-400 focus:outline-none focus:ring-2 focus:ring-primary-500"
                :class="cellClass(cellAt(axis.id, model).state)"
                :title="cellTitle(axis, model)"
                :aria-label="cellTitle(axis, model)"
                :disabled="cellAt(axis.id, model).artifact_id === 0"
                @click="onCellClick(axis.id, model)"
              >
                {{ cellLabel(cellAt(axis.id, model).state) }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { IQMatrix, IQMatrixAxis, IQMatrixCell, IQMatrixCellState } from './types'

const props = defineProps<{
  matrix: IQMatrix
  loading?: boolean
}>()

const emit = defineEmits<{
  (e: 'select', artifactId: number): void
}>()

const { t } = useI18n()

/**
 * 按 (题目, 模型) 建索引，避免每个单元格都线性扫一遍 cells。
 *
 * 60 格 × 线性扫描在规模上完全够用，但矩阵会随筛选重算，
 * 每次重算都做 60 次全表扫描属于白白浪费；代价只是一个 Map。
 */
const cellIndex = computed(() => {
  const index = new Map<string, IQMatrixCell>()
  for (const cell of props.matrix.cells) {
    index.set(cellKey(cell.case_id, cell.model), cell)
  }
  return index
})

function cellKey(caseId: number, model: string): string {
  return `${caseId}\u0000${model}`
}

/**
 * 取单元格；缺失时返回一个占位对象。
 *
 * 不返回 undefined 而是返回空状态对象：模板里到处写 `?.` 会让
 * 「本来就该是空格子」和「数据有问题」混在一起看不清。
 */
function cellAt(caseId: number, model: string): IQMatrixCell {
  return (
    cellIndex.value.get(cellKey(caseId, model)) ?? {
      case_id: caseId,
      case_name: '',
      model,
      state: 'empty',
      artifact_id: 0,
    }
  )
}

/** 四种状态的颜色。刻意只用背景色区分，不加边框/纹理。 */
function cellClass(state: IQMatrixCellState): string {
  switch (state) {
    case 'passed':
      return 'bg-emerald-500/85 text-white'
    case 'incorrect':
      return 'bg-amber-400/85 text-amber-950'
    case 'error':
      return 'bg-rose-500/85 text-white'
    default:
      return 'bg-gray-100 text-gray-400 dark:bg-dark-800 dark:text-dark-500'
  }
}

/** 格子里的短文案：颜色之外的第二重编码（色弱用户必须能读）。 */
function cellLabel(state: IQMatrixCellState): string {
  switch (state) {
    case 'passed':
      return t('iqBenchmark.matrix.state.passed')
    case 'incorrect':
      return t('iqBenchmark.matrix.state.incorrect')
    case 'error':
      return t('iqBenchmark.matrix.state.error')
    default:
      return '—'
  }
}

const legends = computed(() => [
  { state: 'passed' as const, label: t('iqBenchmark.matrix.legend.passed') },
  { state: 'incorrect' as const, label: t('iqBenchmark.matrix.legend.incorrect') },
  { state: 'error' as const, label: t('iqBenchmark.matrix.legend.error') },
  { state: 'empty' as const, label: t('iqBenchmark.matrix.legend.empty') },
])

/**
 * 悬停说明：把 state 之外的原始语义（verdict / error_kind）也带上。
 *
 * 矩阵格子太小放不下细节，但「为什么红」恰恰是用户最想知道的 ——
 * 超时和答错都显示红色，处理方式完全不同。
 */
function cellTitle(axis: IQMatrixAxis, model: string): string {
  const cell = cellAt(axis.id, model)
  if (cell.state === 'empty') {
    return t('iqBenchmark.matrix.tooltip.empty', { case: axis.name, model })
  }
  const base = t('iqBenchmark.matrix.tooltip.base', {
    case: axis.name,
    model,
    state: cellLabel(cell.state),
  })
  if (cell.error_kind) {
    return `${base} · ${t(`iqBenchmark.verdict.errorKind.${cell.error_kind}`)}`
  }
  if (cell.verdict) {
    return `${base} · ${t(`iqBenchmark.verdict.state.${cell.verdict}`)}`
  }
  return base
}

function onCellClick(caseId: number, model: string): void {
  const cell = cellAt(caseId, model)
  if (cell.artifact_id > 0) emit('select', cell.artifact_id)
}
</script>
