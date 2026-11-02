<template>
  <div class="overflow-x-auto">
    <table class="w-full text-xs">
      <thead>
        <tr class="border-b border-gray-200 text-left text-gray-400 dark:border-dark-700">
          <th class="p-2">{{ t('iqBenchmark.admin.runs.trigger') }}</th>
          <th class="p-2">{{ t('iqBenchmark.admin.runs.startedAt') }}</th>
          <th class="p-2">{{ t('iqBenchmark.admin.runs.finishedAt') }}</th>
          <th class="p-2">{{ t('iqBenchmark.admin.runs.counts') }}</th>
          <th class="p-2">{{ t('iqBenchmark.admin.runs.cost') }}</th>
          <th class="p-2">{{ t('iqBenchmark.admin.runs.status') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="run in runs"
          :key="run.id"
          class="border-b border-gray-100 last:border-0 dark:border-dark-800"
        >
          <td class="p-2">{{ t(`iqBenchmark.trigger.${run.trigger}`) }}</td>
          <td class="p-2 tabular-nums">{{ formatTime(run.started_at) }}</td>
          <td class="p-2 tabular-nums">{{ run.finished_at ? formatTime(run.finished_at) : '—' }}</td>
          <td class="p-2 tabular-nums">
            <span class="text-emerald-600 dark:text-emerald-400">{{ run.success_count }}</span> /
            <span class="text-red-500 dark:text-red-400">{{ run.failed_count }}</span> /
            <span class="text-amber-600 dark:text-amber-400">{{ run.timeout_count }}</span> /
            <span class="text-gray-500">{{ run.blocked_count }}</span>
          </td>
          <td class="p-2 tabular-nums">{{ formatCost(run.total_cost) }}</td>
          <td class="p-2">
            <span class="rounded-full px-2 py-0.5 text-[10px] font-semibold" :class="statusClass(run.status)">
              {{ t(`iqBenchmark.status.${run.status}`) }}
            </span>
          </td>
        </tr>
        <tr v-if="runs.length === 0">
          <td colspan="6" class="p-6 text-center text-gray-400">{{ t('iqBenchmark.empty.runs') }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { IQAdminRun } from './types'
import { formatCost } from './filters'

withDefaults(defineProps<{ runs?: IQAdminRun[] }>(), { runs: () => [] })

const { t, locale } = useI18n()

function formatTime(raw: string): string {
  if (!raw) return '—'
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-US')
}

function statusClass(status: IQAdminRun['status']): string {
  switch (status) {
    case 'success':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'partial':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
    case 'running':
      return 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300'
    default:
      return 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
  }
}
</script>
