<template>
  <section class="card space-y-4 p-6">
    <h2 class="text-lg font-semibold">{{ t('checkin.cycle') }}</h2>
    <template v-if="cycle">
      <p class="text-sm text-gray-500">{{ cycle.start_date }} — {{ cycle.end_date }} · v{{ cycle.rule_version }}</p>
      <p class="font-medium">{{ t('checkin.cycleProgress', { count: cycle.qualified_days, total: cycle.days.length }) }} · ${{ cycle.reward_amount }}</p>
      <div class="grid grid-cols-2 gap-2 sm:grid-cols-4 lg:grid-cols-7"><div v-for="day in cycle.days" :key="day.date" class="rounded-xl border p-3 text-xs" :class="day.met ? 'border-primary-300 bg-primary-50 dark:bg-primary-900/20' : 'border-gray-200 dark:border-dark-700'">
        <p class="font-medium">{{ day.date.slice(5) }}</p><p class="mt-2">{{ t('checkin.daily') }}: {{ day.checked_in ? '✓' : '—' }}</p><p>{{ day.requests }} / {{ cycle.requests_threshold }}</p><p class="mt-1 text-gray-500">{{ t(day.future ? 'checkin.future' : day.finalized ? (day.met ? 'checkin.met' : 'checkin.broken') : 'checkin.inProgress') }}</p>
      </div></div>
      <p class="text-sm">{{ t(`checkin.states.${cycle.state}`) }} · {{ t('checkin.settleAt') }} {{ formatCheckinTime(cycle.settle_at, locale) }}</p>
    </template>
    <p v-else class="text-sm text-gray-500">{{ t('checkin.noCycle') }}</p><p class="text-xs text-gray-500">{{ t('checkin.cycleHint') }}</p>
  </section>
</template>
<script setup lang="ts">
import { formatCheckinTime } from './time'
import { useI18n } from 'vue-i18n'
import type { CheckinCycle } from '@/api/checkin'
defineProps<{ cycle: CheckinCycle | null }>()
const { t, locale } = useI18n()
</script>
