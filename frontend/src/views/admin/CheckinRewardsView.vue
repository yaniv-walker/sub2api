<template>
  <AppLayout><div class="space-y-6"><h1 class="text-2xl font-bold">{{ t('checkin.audit') }}</h1><form class="flex flex-wrap gap-3" @submit.prevent="search"><input v-model="filter" type="number" min="1" class="input max-w-xs" :placeholder="t('checkin.userId')" :aria-label="t('checkin.userId')" /><input v-model="dateFrom" data-testid="date-from" type="date" class="input max-w-xs" :aria-label="t('checkin.dateFrom')" /><input v-model="dateTo" data-testid="date-to" type="date" class="input max-w-xs" :aria-label="t('checkin.dateTo')" /><select v-model="rewardType" data-testid="reward-type" class="input max-w-xs" :aria-label="t('checkin.type')"><option value="">{{ t('checkin.allRewards') }}</option><option value="daily">{{ t('checkin.daily') }}</option><option value="cycle">{{ t('checkin.cycle') }}</option></select><button class="btn btn-primary" :disabled="loading || cyclesLoading || budgetsLoading">{{ t('checkin.search') }}</button></form><p v-if="error" role="alert" class="text-amber-600">{{ error }}</p><section class="card p-6"><h2 class="mb-4 font-semibold">{{ t('checkin.history') }}</h2><RewardHistory :items="items" admin /><button v-if="cursor" class="btn btn-secondary mt-4" :disabled="loading" @click="load(true)">{{ t('checkin.more') }}</button></section>
    <section class="card space-y-4 p-6"><h2 class="font-semibold">{{ t('checkin.actualBudget') }}</h2><p class="text-xs text-gray-500">{{ t('checkin.actualBudgetHint') }}</p><p v-if="budgetsError" role="alert" class="text-amber-600">{{ budgetsError }} <button class="underline" @click="loadBudgets">{{ t('checkin.refresh') }}</button></p><div class="overflow-x-auto"><table class="w-full text-left text-sm"><thead><tr><th class="p-3">{{ t('checkin.date') }}</th><th v-for="key in budgetKeys" :key="key" class="p-3">{{ t(`checkin.${key}`) }}</th></tr></thead><tbody><tr v-for="day in budgets" :key="day.business_date" class="border-t dark:border-dark-700"><td class="p-3">{{ day.business_date }}</td><td v-for="key in budgetKeys" :key="key" class="p-3 font-mono">${{ day[key] }}</td></tr></tbody></table><p v-if="!budgetsLoading && !budgets.length" class="p-4 text-center text-gray-500">{{ t('checkin.empty') }}</p></div></section>
    <section class="card space-y-4 p-6"><h2 class="font-semibold">{{ t('checkin.settlementAudit') }}</h2><p class="text-xs text-gray-500">{{ t('checkin.cancelHint') }}</p><p v-if="cyclesError" role="alert" class="text-amber-600">{{ cyclesError }} <button class="underline" @click="loadCycles(false)">{{ t('checkin.refresh') }}</button></p><div class="overflow-x-auto"><table class="w-full text-left text-sm"><thead><tr><th class="p-3">{{ t('checkin.userId') }}</th><th class="p-3">{{ t('checkin.date') }}</th><th class="p-3">{{ t('checkin.reward') }}</th><th class="p-3">{{ t('checkin.status') }}</th><th class="p-3">{{ t('checkin.actions') }}</th></tr></thead><tbody><tr v-for="cycle in cycles" :key="cycle.id" class="border-t dark:border-dark-700"><td class="p-3">{{ cycle.user_id }}</td><td class="p-3">{{ cycle.start_date }} — {{ cycle.end_date }}</td><td class="p-3">${{ cycle.reward_amount }}</td><td class="p-3">{{ t(`checkin.states.${cycle.state}`) }}</td><td class="p-3"><div v-if="!['paid','completed','cancelled','broken','in_progress','active'].includes(cycle.state)" class="flex gap-2"><button class="btn btn-secondary" @click="begin(cycle, 'retry')">{{ t('checkin.retrySettlement') }}</button><button v-if="cycle.state === 'pending_usage'" class="btn btn-secondary text-red-600" @click="begin(cycle, 'cancel')">{{ t('checkin.cancelSettlement') }}</button></div></td></tr></tbody></table><p v-if="!cycles.length" class="p-4 text-center text-gray-500">{{ t('checkin.empty') }}</p></div><button v-if="cycleCursor" class="btn btn-secondary" :disabled="cyclesLoading" @click="loadCycles(true)">{{ t('checkin.more') }}</button></section>
    <section v-if="selected" role="dialog" aria-modal="false" :aria-label="t('checkin.resolveTitle')" class="card space-y-4 border-amber-300 p-6"><h2 class="font-semibold">{{ t('checkin.resolveTitle') }} #{{ selected.id }}</h2><p>{{ t(action === 'cancel' ? 'checkin.cancelConfirm' : 'checkin.retryConfirm') }}</p><label class="block">{{ t('checkin.reason') }}<textarea v-model="reason" class="input mt-2" rows="3" /></label><p v-if="resolveError" role="alert" class="text-red-600">{{ resolveError }}</p><div class="flex gap-3"><button class="btn btn-primary" :disabled="resolving || !reason.trim()" @click="resolve">{{ t('checkin.confirm') }}</button><button class="btn btn-secondary" :disabled="resolving" @click="selected = null">{{ t('checkin.back') }}</button></div></section>
  </div></AppLayout>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import RewardHistory from '@/components/user/checkin/RewardHistory.vue'
import { checkinAPI, type CheckinRecord, type CheckinAdminCycle, type CheckinBudget, type CheckinRewardFilters } from '@/api/checkin'
import { useAppStore } from '@/stores/app'
const { t } = useI18n(), app = useAppStore()
const items = ref<CheckinRecord[]>([]), cursor = ref(0), filter = ref(''), loading = ref(false), error = ref('')
const cycles = ref<CheckinAdminCycle[]>([]), cycleCursor = ref(0), cyclesLoading = ref(false), cyclesError = ref('')
const selected = ref<CheckinAdminCycle | null>(null), action = ref<'retry' | 'cancel'>('retry'), reason = ref(''), resolving = ref(false), resolveError = ref('')
const dateFrom = ref(''), dateTo = ref(''), rewardType = ref<'' | 'daily' | 'cycle'>('')
const budgets = ref<CheckinBudget[]>([]), budgetsLoading = ref(false), budgetsError = ref('')
const budgetKeys = ['limit_usd', 'spent_usd', 'reserved_usd', 'remaining_usd'] as const
let loadedFilter: number | undefined
let loadedRewards: CheckinRewardFilters = {}
let loadedDates: { date_from?: string; date_to?: string } = {}
function search() { if (loading.value || cyclesLoading.value || budgetsLoading.value) return; const userId = filter.value ? Number(filter.value) : undefined; if (userId !== undefined && (!Number.isSafeInteger(userId) || userId < 1)) return; if (dateFrom.value && dateTo.value && dateFrom.value > dateTo.value) { error.value = t('checkin.invalidDates'); return }
  loadedFilter = userId; loadedRewards = { user_id: userId, date_from: dateFrom.value || undefined, date_to: dateTo.value || undefined, reward_type: rewardType.value || undefined }; loadedDates = { date_from: dateFrom.value || undefined, date_to: dateTo.value || undefined }; void load(false); void loadCycles(false); void loadBudgets() }
async function load(append: boolean) {
  if (loading.value) return
  loading.value = true; error.value = ''
  try { const data = await checkinAPI.rewards(loadedRewards, append ? cursor.value : 0); items.value = append ? [...items.value, ...data.items] : data.items; cursor.value = data.next_cursor }
  catch { error.value = t('checkin.historyFailed') }
  finally { loading.value = false }
}
async function loadCycles(append: boolean) {
  if (cyclesLoading.value) return
  cyclesLoading.value = true; cyclesError.value = ''
  try { const data = await checkinAPI.cycles(loadedFilter, append ? cycleCursor.value : 0); cycles.value = append ? [...cycles.value, ...data.items] : data.items; cycleCursor.value = data.next_cursor }
  catch { cyclesError.value = t('checkin.historyFailed') }
  finally { cyclesLoading.value = false }
}
async function loadBudgets() {
  if (budgetsLoading.value) return
  budgetsLoading.value = true; budgetsError.value = ''
  try { const data = await checkinAPI.budgets(loadedDates); budgets.value = data.items }
  catch { budgetsError.value = t('checkin.budgetFailed') }
  finally { budgetsLoading.value = false }
}
function begin(cycle: CheckinAdminCycle, nextAction: 'retry' | 'cancel') { selected.value = cycle; action.value = nextAction; reason.value = ''; resolveError.value = '' }
async function resolve() {
  if (!selected.value || !reason.value.trim() || resolving.value) return
  resolving.value = true; resolveError.value = ''
  try { await checkinAPI.resolveCycle(selected.value.id, action.value, reason.value.trim()); app.showSuccess(t('checkin.resolved')); selected.value = null; await loadCycles(false); await load(false); await loadBudgets() }
  catch { resolveError.value = t('checkin.resolveFailed') }
  finally { resolving.value = false }
}
onMounted(search)
</script>
