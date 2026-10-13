<template>
  <AppLayout><div class="mx-auto max-w-6xl space-y-6">
    <header class="flex flex-wrap items-center justify-between gap-3"><div><h1 class="text-2xl font-bold">{{ t('checkin.title') }}</h1><p class="mt-1 text-sm text-gray-500">{{ t('checkin.subtitle') }}</p></div><button class="btn btn-secondary" :disabled="loading || claiming" @click="load">{{ t('checkin.refresh') }}</button></header>
    <p v-if="error" role="alert" class="rounded-xl bg-amber-50 p-4 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">{{ error }}</p>
    <p v-if="loading && !status" role="status">{{ t('checkin.loading') }}</p>
    <template v-if="status">
      <div class="grid gap-4 sm:grid-cols-3" data-testid="account-summary">
        <section class="card p-5"><p class="text-sm text-gray-500">{{ t('checkin.balance') }}</p><button data-testid="balance" class="mt-2 font-mono text-2xl" :aria-label="t('checkin.reveal')" @click="reveal">{{ visible ? `$${status.balance}` : '••••••' }}</button><p class="mt-2 text-xs text-gray-500">{{ t('checkin.privacy') }}</p></section>
        <section class="card p-5"><p class="text-sm text-gray-500">{{ t('checkin.paidCredit') }}</p><p class="mt-2 font-mono text-2xl">${{ status.eligible_paid_credit_usd }}</p><p class="mt-2 text-xs text-gray-500">{{ t('checkin.minimum', { amount: status.minimum_paid_credit_usd }) }}</p></section>
        <section class="card p-5"><p class="text-sm text-gray-500">{{ t('checkin.todayRequests') }}</p><p data-testid="today-requests" class="mt-2 font-mono text-2xl">{{ status.today_valid_requests }}</p><p class="mt-2 text-xs text-gray-500">{{ t('checkin.requestsHint') }}</p></section>
      </div>
      <div class="grid gap-6 lg:grid-cols-[1.35fr_1fr]">
        <section class="card p-6 text-center"><p class="text-sm text-gray-500">{{ status.business_date }} · {{ status.timezone }} · v{{ status.rule_version }}</p>
          <CheckinWheel :options="status.reward_options" :result="status.record" :checked-in="status.checked_in" />
          <button data-testid="claim" class="btn btn-primary w-full py-3" :disabled="claiming || uncertain || !status.can_claim || !flagEnabled" @click="claim">{{ claiming ? t('checkin.claiming') : t(`checkin.states.${displayState}`) }}</button>
          <p v-if="!status.eligible" class="mt-3 text-sm text-amber-600">{{ t('checkin.minimum', { amount: status.minimum_paid_credit_usd }) }}</p>
          <p class="mt-3 text-xs text-gray-500">{{ t('checkin.reset') }} {{ formatCheckinTime(status.next_reset_at, locale) }}</p>
        </section>
        <div class="space-y-6"><section class="card space-y-3 p-6"><h2 class="font-semibold">{{ t('checkin.rules') }}</h2><p class="text-sm">{{ t('checkin.minimum', { amount: status.minimum_paid_credit_usd }) }}</p><p class="text-sm text-gray-500">{{ t('checkin.rewardCreditHint') }}</p><p v-if="status.cycle_enabled" class="text-sm">{{ t('checkin.cycleRule', { days: status.cycle_days, requests: status.requests_threshold, reward: status.cycle_reward_usd }) }}</p><p v-else class="text-sm text-gray-500">{{ t('checkin.cycleDisabled') }}</p></section>
          <section class="card space-y-3 p-6"><h2 class="font-semibold">{{ t('checkin.bonus') }} <span class="text-xs font-normal text-gray-500">{{ t(status.dynamic_bonus.locked ? 'checkin.locked' : 'checkin.live') }}</span></h2><template v-if="status.dynamic_bonus.enabled"><div v-for="key in bonusKeys" :key="key" class="flex justify-between text-sm"><span>{{ t(`checkin.${key}`) }}</span><span>{{ (status.dynamic_bonus[key] / 100).toFixed(2) }}%</span></div><p class="border-t pt-3 text-primary-600">{{ t('checkin.total') }} {{ (status.dynamic_bonus.total_bps / 100).toFixed(2) }}%</p></template><p v-else class="text-sm text-gray-500">{{ t('checkin.bonusDisabled') }}</p><p class="text-xs text-gray-500">{{ t('checkin.bonusHint') }}</p></section>
        </div>
      </div>
      <CheckinCycleCard v-if="status.cycle_enabled || status.cycle" :cycle="status.cycle" />
    </template>
    <section class="card p-6"><h2 class="mb-4 text-lg font-semibold">{{ t('checkin.history') }}</h2><p v-if="historyError" role="alert" class="mb-3 text-amber-600">{{ historyError }} <button class="underline" @click="loadHistory(false)">{{ t('checkin.refresh') }}</button></p><RewardHistory :items="history" /><button v-if="nextCursor" class="btn btn-secondary mt-4" :disabled="historyLoading" @click="loadHistory(true)">{{ t('checkin.more') }}</button></section>
  </div></AppLayout>
</template>
<script setup lang="ts">
import { formatCheckinTime } from '@/components/user/checkin/time'
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import RewardHistory from '@/components/user/checkin/RewardHistory.vue'
import CheckinCycleCard from '@/components/user/checkin/CheckinCycleCard.vue'
import CheckinWheel from '@/components/user/checkin/CheckinWheel.vue'
import { checkinAPI, type CheckinRecord, type CheckinStatus } from '@/api/checkin'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
const { t, locale } = useI18n()
const auth = useAuthStore()
const app = useAppStore()
const status = ref<CheckinStatus | null>(null)
const loading = ref(false), claiming = ref(false), visible = ref(false), error = ref(''), uncertain = ref(false)
const history = ref<CheckinRecord[]>([]), nextCursor = ref(0), historyLoading = ref(false), historyError = ref('')
const bonusKeys = ['recharge_bps', 'balance_bps', 'requests_bps'] as const
const flagEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.checkin))
const displayState = computed(() => !flagEnabled.value || !status.value?.enabled ? 'disabled' : status.value.checked_in ? 'checked_in' : status.value.state)
let resetTimer: ReturnType<typeof setTimeout> | undefined
let disposed = false
function scheduleReset() {
  clearTimeout(resetTimer)
  if (!status.value || disposed) return
  const delay = new Date(status.value.next_reset_at).getTime() - new Date(status.value.server_time).getTime()
  if (Number.isFinite(delay) && delay > 0) resetTimer = setTimeout(() => { void load() }, Math.min(delay + 1000, 2147483647))
}
let privacyTimer: ReturnType<typeof setTimeout> | undefined
function reveal() { visible.value = true; clearTimeout(privacyTimer); privacyTimer = setTimeout(() => { visible.value = false }, 2000) }
onUnmounted(() => { disposed = true; clearTimeout(privacyTimer); clearTimeout(resetTimer) })
async function loadHistory(append = false) {
  if (historyLoading.value) return
  historyLoading.value = true; historyError.value = ''
  try { const result = await checkinAPI.history(append ? nextCursor.value : 0); history.value = append ? [...history.value, ...result.items] : result.items; nextCursor.value = result.next_cursor }
  catch { historyError.value = t('checkin.historyFailed') }
  finally { historyLoading.value = false }
}
async function load() {
  if (loading.value) return
  loading.value = true; error.value = ''
  try { status.value = await checkinAPI.status(); uncertain.value = false; scheduleReset() } catch { error.value = t('checkin.loadFailed') }
  finally { loading.value = false }
  await loadHistory()
}
async function reconcile() { status.value = await checkinAPI.status(); scheduleReset(); return status.value.checked_in }
async function claim() {
  if (claiming.value || uncertain.value || !status.value?.can_claim || !flagEnabled.value) return
  claiming.value = true; error.value = ''
  let credited = false
  try {
    const result = await checkinAPI.claim()
    credited = true
    if (status.value) status.value = { ...status.value, record: result.record ?? status.value.record, checked_in: true, can_claim: false }
    if (result.status) { status.value = result.status; scheduleReset() }
    else { try { const latest = await checkinAPI.status(); if (latest.checked_in || !credited) { status.value = latest; scheduleReset() } } catch { /* A confirmed record remains successful even if refresh fails. */ } }
    app.showSuccess(t(result.created ? 'checkin.success' : 'checkin.already'))
  } catch {
    try { credited = await reconcile(); if (credited) app.showSuccess(t('checkin.already')); else error.value = t(status.value?.state === 'budget_exhausted' ? 'checkin.states.budget_exhausted' : 'checkin.failed') }
    catch { uncertain.value = true; error.value = t('checkin.uncertain') }
  } finally {
    if (credited) { try { await auth.refreshUser() } catch { app.showWarning(t('checkin.balanceRefreshFailed')) } await loadHistory() }
    claiming.value = false
  }
}
onMounted(load)
</script>
