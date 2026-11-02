<template>
  <AppLayout><div class="mx-auto max-w-6xl space-y-6">
    <header class="flex flex-wrap items-center justify-between gap-3"><div><h1 class="text-2xl font-bold">{{ t('checkin.title') }}</h1><p class="mt-1 text-sm text-gray-500">{{ t('checkin.subtitle') }}</p></div><button class="btn btn-secondary" :disabled="loading || claiming" @click="load">{{ t('checkin.refresh') }}</button></header>
    <p v-if="error" role="alert" class="rounded-xl bg-amber-50 p-4 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">{{ error }}</p>
    <p v-if="loading && !status" role="status">{{ t('checkin.loading') }}</p>
    <template v-if="status">
      <div class="grid gap-4 sm:grid-cols-3" data-testid="account-summary">
        <section class="card p-5"><p class="text-sm text-gray-500">{{ t('checkin.balance') }}</p><p data-testid="balance" class="mt-2 font-mono text-2xl">${{ Number(status.balance).toFixed(2) }}</p></section>
        <section class="card p-5"><p class="text-sm text-gray-500">{{ t('checkin.paidCredit') }}</p><p class="mt-2 font-mono text-2xl">${{ Number(status.eligible_paid_credit_usd).toFixed(2) }}</p><p class="mt-2 text-xs text-gray-500">{{ t('checkin.minimum', { amount: status.minimum_paid_credit_usd }) }}</p></section>
        <section class="card p-5"><p class="text-sm text-gray-500">{{ t('checkin.todayRequests') }}</p><p data-testid="today-requests" class="mt-2 font-mono text-2xl">{{ status.today_valid_requests }}</p><p class="mt-2 text-xs text-gray-500">{{ t('checkin.requestsHint') }}</p></section>
      </div>
      <div class="grid gap-6 lg:grid-cols-[1.35fr_1fr]">
        <section class="card p-6 text-center"><p class="text-sm text-gray-500">{{ status.business_date }} · {{ status.timezone }} · v{{ status.rule_version }}</p>
          <CheckinWheel :key="String(adminTestMode)" :options="status.reward_options" :result="adminTestMode ? testResult : pendingRecord ?? status.record" :checked-in="!adminTestMode && status.checked_in" :spinning="requestPending || testSpinning" :preview="adminTestMode" :animate="wheelAnimation" @settled="onWheelSettled" />
          <label class="mb-4 flex items-center justify-center gap-2 text-sm text-gray-500"><input v-model="wheelAnimation" data-testid="wheel-animation" type="checkbox" :disabled="claiming || testBusy" />{{ t('checkin.wheelAnimation') }}</label>
          <div v-if="auth.isAdmin" class="mb-4 rounded-lg border border-gray-200 p-3 text-left dark:border-dark-700">
            <label class="flex items-center gap-2 text-sm font-medium"><input v-model="testMode" data-testid="wheel-test-mode" type="checkbox" :disabled="claiming || testBusy" />{{ t('checkin.testMode') }}</label>
            <p class="mt-1 text-xs text-gray-500">{{ t('checkin.testHint') }}</p>
            <div v-if="adminTestMode" class="mt-3 flex flex-wrap items-center gap-2">
              <select v-model="testAmount" data-testid="wheel-test-amount" class="input w-auto" :aria-label="t('checkin.testAmount')" :disabled="testBusy"><option v-for="option in status.reward_options" :key="option.amount" :value="option.amount">${{ Number(option.amount) }}</option></select>
              <button data-testid="wheel-test-spin" class="btn btn-secondary" :disabled="testBusy || !status.reward_options.length" @click="testSpin">{{ t(testBusy ? 'checkin.testSpinning' : 'checkin.testSpin') }}</button>
            </div>
          </div>
          <button data-testid="claim" class="btn btn-primary w-full py-3" :disabled="loading || adminTestMode || claiming || uncertain || !status.can_claim || !flagEnabled" @click="claim">{{ claiming ? t('checkin.claiming') : t(`checkin.states.${displayState}`) }}</button>
          <p v-if="!status.eligible" class="mt-3 text-sm text-amber-600">{{ t('checkin.minimum', { amount: status.minimum_paid_credit_usd }) }}</p>
          <p class="mt-3 text-xs text-gray-500">{{ t('checkin.settlementHint') }}</p><p class="mt-3 text-xs text-gray-500">{{ t('checkin.reset') }} {{ formatCheckinTime(status.next_reset_at, locale) }}</p>
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
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
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
const loading = ref(false), claiming = ref(false), error = ref(''), uncertain = ref(false)
const requestPending = ref(false), pendingRecord = ref<CheckinRecord | null>(null)
let finishWheel: (() => void) | undefined
function onWheelSettled() { finishWheel?.(); finishWheel = undefined; if (adminTestMode.value) testBusy.value = false }
async function revealReward(record: CheckinRecord | null) {
  const finished = new Promise<void>(resolve => { finishWheel = resolve })
  pendingRecord.value = record
  requestPending.value = false
  await finished
}
const history = ref<CheckinRecord[]>([]), nextCursor = ref(0), historyLoading = ref(false), historyError = ref('')
const bonusKeys = ['recharge_bps', 'balance_bps', 'requests_bps'] as const
const flagEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.checkin))
const displayState = computed(() => !flagEnabled.value || !status.value?.enabled ? 'disabled' : status.value.checked_in ? 'checked_in' : status.value.state)
const testMode = ref(false), testBusy = ref(false), testSpinning = ref(false), testAmount = ref('')
const wheelAnimation = ref(true)
const testResult = ref<Pick<CheckinRecord, 'id' | 'reward_amount'> | null>(null)
const adminTestMode = computed(() => auth.isAdmin && testMode.value)
let testTimer: ReturnType<typeof setTimeout> | undefined
let testSequence = 0
function resetTest() {
  clearTimeout(testTimer)
  testBusy.value = false; testSpinning.value = false; testResult.value = null
}
watch(adminTestMode, () => { resetTest(); testAmount.value = status.value?.reward_options[0]?.amount ?? '' })
function testSpin() {
  if (!adminTestMode.value || testBusy.value || !status.value?.reward_options.some(option => option.amount === testAmount.value)) return
  testBusy.value = true; testSpinning.value = true; testResult.value = null
  const amount = testAmount.value
  testTimer = setTimeout(() => {
    testResult.value = { id: ++testSequence, reward_amount: amount }
    testSpinning.value = false
  }, 500)
}
let resetTimer: ReturnType<typeof setTimeout> | undefined
let disposed = false
function scheduleReset() {
  clearTimeout(resetTimer)
  if (!status.value || disposed) return
  const delay = new Date(status.value.next_reset_at).getTime() - new Date(status.value.server_time).getTime()
  if (Number.isFinite(delay) && delay > 0) resetTimer = setTimeout(() => { void load() }, Math.min(delay + 1000, 2147483647))
}
onUnmounted(() => { disposed = true; onWheelSettled(); clearTimeout(resetTimer); resetTest() })
async function loadHistory(append = false) {
  if (historyLoading.value) return
  historyLoading.value = true; historyError.value = ''
  try { const result = await checkinAPI.history(append ? nextCursor.value : 0); history.value = append ? [...history.value, ...result.items] : result.items; nextCursor.value = result.next_cursor }
  catch { historyError.value = t('checkin.historyFailed') }
  finally { historyLoading.value = false }
}
async function load() {
  if (loading.value || claiming.value) return
  loading.value = true; error.value = ''
  try { status.value = await checkinAPI.status(); uncertain.value = false; scheduleReset(); await loadHistory() } catch { error.value = t('checkin.loadFailed') }
  finally { loading.value = false }
}
async function claim() {
  if (loading.value || adminTestMode.value || claiming.value || uncertain.value || !status.value?.can_claim || !flagEnabled.value) return
  claiming.value = true; requestPending.value = true; error.value = ''
  const claimDate = status.value.business_date
  let credited = false
  let latest: CheckinStatus | undefined
  let record: CheckinRecord | null = null
  let message = 'checkin.success'
  function applyStatus(snapshot: CheckinStatus) {
    // A stale same-day read must not unlock a claim already confirmed as committed.
    status.value = credited && snapshot.business_date === claimDate && !snapshot.checked_in
      ? { ...snapshot, checked_in: true, can_claim: false, record, balance: status.value?.balance ?? snapshot.balance }
      : snapshot
    scheduleReset()
  }
  try {
    try {
      const result = await checkinAPI.claim()
      credited = true
      record = result.record ?? result.status?.record ?? null
      latest = result.status
      message = result.created ? 'checkin.success' : 'checkin.already'
      if (!latest) {
        try { latest = await checkinAPI.status(); record ??= latest.record } catch { /* Confirmed credit survives a failed status refresh. */ }
      }
    } catch {
      try {
        latest = await checkinAPI.status()
        credited = latest.checked_in
        record = latest.record
        message = 'checkin.already'
        if (!credited) error.value = t(latest.state === 'budget_exhausted' ? 'checkin.states.budget_exhausted' : 'checkin.failed')
      } catch { uncertain.value = true; error.value = t('checkin.uncertain') }
    }
    if (disposed) return
    if (credited) {
      await revealReward(record)
      if (disposed) return
      // The server has committed settlement; only presentation waits for the wheel.
      if (status.value) status.value = { ...status.value, checked_in: true, can_claim: false, record, ...(record ? { balance: record.balance_after ?? status.value.balance } : {}) }
      app.showSuccess(t(message))
      try {
        const refreshed = await checkinAPI.status()
        if (!disposed) applyStatus(refreshed)
      } catch {
        if (latest && !disposed) applyStatus(latest)
        if (!disposed) app.showWarning(t('checkin.balanceRefreshFailed'))
      }
      try { await auth.refreshUser() } catch { if (!disposed) app.showWarning(t('checkin.balanceRefreshFailed')) }
      if (!disposed) await loadHistory()
    } else if (latest) { status.value = latest; scheduleReset() }
  } finally {
    requestPending.value = false
    claiming.value = false
    pendingRecord.value = null
  }
}
onMounted(load)
</script>
