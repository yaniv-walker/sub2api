<template>
  <section class="card p-6" data-testid="checkin-cycle">
    <header class="flex items-start justify-between gap-4">
      <div>
        <h2 class="text-lg font-semibold">{{ t('checkin.cycleTitle') }}</h2>
        <p v-if="cycle" class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('checkin.cycleTimeline', { count: elapsedDays, total: cycle.days.length, remaining: remainingDays }) }}</p>
      </div>
      <span v-if="cycle" class="shrink-0 rounded-md border border-gray-200 bg-gray-50 px-2.5 py-1 text-xs font-semibold text-gray-600 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-300">{{ t(`checkin.states.${cycle.state}`) }}</span>
    </header>
    <template v-if="cycle">
      <div class="mt-3 h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-800" role="progressbar" :aria-label="t('checkin.cycleTitle')" :aria-valuenow="elapsedDays" :aria-valuemin="0" :aria-valuemax="cycle.days.length">
        <div class="h-full rounded-full bg-emerald-700 transition-[width] dark:bg-emerald-500" :style="{ width: `${progress}%` }" />
      </div>
      <div class="mt-4 grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-7">
        <div v-for="day in cycle.days" :key="day.date" class="rounded-md border px-3 py-2.5 text-center" :class="day.future ? 'border-gray-100 bg-gray-50/60 text-gray-400 dark:border-dark-800 dark:bg-dark-900' : day.finalized && !day.met ? 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-900/20 dark:text-amber-300' : 'border-gray-200 bg-gray-50 text-gray-800 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-100'" :title="day.future ? t('checkin.future') : t('checkin.cycleDayDetails', { checked: t(day.checked_in ? 'checkin.already' : 'checkin.notCheckedIn'), count: day.requests, threshold: cycle.requests_threshold })">
          <p class="text-sm font-semibold">{{ formatDay(day.date) }}</p>
          <p class="mt-1 text-xs" :class="day.future ? '' : 'text-gray-500 dark:text-gray-400'">{{ day.future ? t('checkin.future') : day.finalized && !day.met ? t('checkin.broken') : t('checkin.requestCount', { count: day.requests }) }}</p>
        </div>
      </div>
      <p class="mt-3 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('checkin.cycleSettlement', { requests: cycle.requests_threshold, days: cycle.days.length, time: formatCheckinTime(cycle.settle_at, locale) }) }}</p>
    </template>
    <template v-else><p class="mt-3 text-sm text-gray-500">{{ t('checkin.noCycle') }}</p><p class="mt-3 text-xs text-gray-500">{{ t('checkin.cycleHint') }}</p></template>
  </section>
</template>
<script setup lang="ts">
import { formatCheckinTime } from './time'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CheckinCycle } from '@/api/checkin'
const props = defineProps<{ cycle: CheckinCycle | null }>()
const { t, locale } = useI18n()
const elapsedDays = computed(() => props.cycle?.days.filter(day => !day.future).length ?? 0)
const remainingDays = computed(() => (props.cycle?.days.length ?? 0) - elapsedDays.value)
const progress = computed(() => props.cycle?.days.length ? elapsedDays.value / props.cycle.days.length * 100 : 0)
function formatDay(date: string) {
  const [, month, day] = date.split('-')
  return `${Number(month)}/${Number(day)}`
}
</script>
