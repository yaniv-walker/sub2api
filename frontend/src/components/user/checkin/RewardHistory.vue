<template>
  <div class="overflow-x-auto"><table class="w-full text-left text-sm">
    <thead class="border-b border-gray-200 text-gray-500 dark:border-dark-700"><tr><th class="p-3">{{ t('checkin.date') }}</th><th v-if="admin" class="p-3">{{ t('checkin.userId') }}</th><th class="p-3">{{ t('checkin.type') }}</th><th class="p-3">{{ t('checkin.reward') }}</th><th class="p-3">{{ t('checkin.version') }}</th></tr></thead>
    <tbody><tr v-for="item in items" :key="item.id" class="border-b border-gray-100 dark:border-dark-800"><td class="p-3"><p>{{ item.business_date }}</p><p class="text-xs text-gray-500">{{ formatCheckinTime(item.claimed_at, locale) }}</p></td><td v-if="admin" class="p-3">{{ item.user_id }}</td><td class="p-3">{{ t(item.reward_type === 'daily' ? 'checkin.daily' : 'checkin.cycle') }}</td><td class="p-3 font-semibold text-primary-600">+${{ item.reward_amount }}</td><td class="p-3">v{{ item.rule_version }}</td></tr></tbody>
  </table><p v-if="!items.length" class="p-8 text-center text-gray-500">{{ t('checkin.empty') }}</p></div>
</template>
<script setup lang="ts">
import { formatCheckinTime } from './time'
import { useI18n } from 'vue-i18n'
import type { CheckinRecord } from '@/api/checkin'
defineProps<{ items: CheckinRecord[]; admin?: boolean }>()
const { t, locale } = useI18n()
</script>
