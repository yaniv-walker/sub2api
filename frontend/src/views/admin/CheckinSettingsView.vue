<template>
  <AppLayout><div class="mx-auto max-w-5xl space-y-6"><header><h1 class="text-2xl font-bold">{{ t('checkin.settings') }}</h1><p class="mt-2 text-sm text-gray-500">{{ t('checkin.adminHint') }}</p></header>
    <p v-if="error" role="alert" class="rounded-xl bg-amber-50 p-4 text-amber-700 dark:bg-amber-900/20">{{ error }} <button class="underline" @click="load">{{ t('checkin.refresh') }}</button></p>
    <form v-if="config" class="space-y-6" @submit.prevent="save">
      <section class="card space-y-4 p-6"><div class="flex items-center justify-between"><h2 class="font-semibold">{{ t('checkin.rules') }} · v{{ config.version }}</h2><label class="flex items-center gap-2"><input v-model="config.enabled" type="checkbox" />{{ t('checkin.enabled') }}</label></div>
        <div class="grid gap-4 sm:grid-cols-3"><label>{{ t('checkin.minimumPaid') }}<input v-model="config.minimum_paid_credit_usd" required class="input mt-2" inputmode="decimal" /></label><label>{{ t('checkin.budget') }}<input v-model="config.daily_budget_usd" required class="input mt-2" inputmode="decimal" /></label><label>{{ t('checkin.effectiveAt') }}<input v-model="effectiveAt" required type="datetime-local" class="input mt-2" /></label></div><p class="text-xs text-gray-500">{{ t('checkin.timezoneHint') }}</p>
      </section>
      <section class="card space-y-4 p-6"><h2 class="font-semibold">{{ t('checkin.pool') }}</h2><div v-for="(option, index) in config.rewards" :key="index" class="flex flex-wrap items-end gap-3"><label class="flex-1">{{ t('checkin.amount') }}<input v-model="option.amount" class="input mt-1" inputmode="decimal" required /></label><label class="flex-1">{{ t('checkin.weight') }}<input v-model.number="option.weight" class="input mt-1" type="number" min="0" max="10000" step="1" required /></label><button type="button" class="btn btn-secondary" :aria-label="t('checkin.remove')" @click="config.rewards.splice(index, 1)">×</button></div><button type="button" class="btn btn-secondary" @click="config.rewards.push({ amount: '', weight: 0 })">{{ t('checkin.add') }}</button><p class="text-sm" :class="weightTotal === 10000 ? 'text-primary-600' : 'text-red-600'">{{ t('checkin.weightTotal') }} {{ weightTotal }} / 10000</p></section>
      <section class="card space-y-4 p-6"><label class="flex items-center gap-2 font-semibold"><input v-model="config.dynamic_bonus_enabled" type="checkbox" />{{ t('checkin.bonus') }}</label><label class="block">{{ t('checkin.maxBonus') }}<input v-model.number="config.maximum_bonus_bps" type="number" min="0" max="2000" class="input mt-2" /></label><p class="text-xs text-gray-500">{{ t('checkin.tierHint') }}</p><div v-for="group in tierGroups" :key="group.key" class="space-y-2"><h3 class="font-medium">{{ t(group.label) }}</h3><div v-for="(tier, index) in config[group.key]" :key="index" class="flex gap-2"><input v-model="tier.threshold" class="input" :aria-label="t('checkin.threshold')" :placeholder="t('checkin.threshold')" /><input v-model.number="tier.bonus_bps" type="number" min="0" max="10000" class="input" :aria-label="t('checkin.bonusBPS')" /><button type="button" class="btn btn-secondary" :aria-label="t('checkin.remove')" @click="config[group.key].splice(index, 1)">×</button></div><button type="button" class="btn btn-secondary" @click="config[group.key].push({ threshold: '', bonus_bps: 0 })">{{ t('checkin.addTier') }}</button></div></section>
      <section class="card space-y-4 p-6"><label class="flex items-center gap-2 font-semibold"><input v-model="config.cycle_enabled" type="checkbox" />{{ t('checkin.cycle') }}</label><div class="grid gap-4 sm:grid-cols-3"><label>{{ t('checkin.days') }}<input v-model.number="config.cycle_days" type="number" min="2" class="input mt-2" /></label><label>{{ t('checkin.requestsThreshold') }}<input v-model.number="config.requests_threshold" type="number" min="1" class="input mt-2" /></label><label>{{ t('checkin.amount') }}<input v-model="config.cycle_reward_usd" class="input mt-2" inputmode="decimal" /></label></div><p class="text-sm text-gray-500">{{ t('checkin.cycleHint') }}</p></section>
      <section class="card space-y-2 p-6"><h2 class="font-semibold">{{ t('checkin.cost') }}</h2><p>{{ t('checkin.baseCost') }}: ${{ expectedCost(config).toFixed(6) }}</p><p>{{ t('checkin.maxCost') }}: ${{ expectedCost(config, config.dynamic_bonus_enabled ? config.maximum_bonus_bps : 0).toFixed(6) }}</p><p class="text-xs text-gray-500">{{ t('checkin.costHint') }}</p></section>
      <p v-if="!valid" class="text-sm text-red-600" role="alert">{{ t('checkin.invalid') }}</p><button type="submit" class="btn btn-primary" :disabled="saving || !valid">{{ saving ? t('checkin.saving') : t('checkin.publish') }}</button>
    </form><p v-else-if="!error">{{ t('checkin.loading') }}</p>
  </div></AppLayout>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { checkinAPI, type CheckinConfig } from '@/api/checkin'
import { expectedCost, nextBeijingDay, validateConfig } from '@/components/user/checkin/rules'
import { useAppStore } from '@/stores/app'
const { t } = useI18n(), app = useAppStore()
const config = ref<CheckinConfig | null>(null), error = ref(''), saving = ref(false), effectiveAt = ref(nextBeijingDay())
const tierGroups = [{ key: 'recharge_bonus_tiers', label: 'checkin.recharge_bps' }, { key: 'balance_bonus_tiers', label: 'checkin.balance_bps' }, { key: 'requests_bonus_tiers', label: 'checkin.requests_bps' }] as const
const weightTotal = computed(() => config.value?.rewards.reduce((sum, r) => sum + r.weight, 0) ?? 0)
const valid = computed(() => !!config.value && validateConfig(config.value) && Number.isFinite(new Date(`${effectiveAt.value}:00+08:00`).getTime()))
async function load() { error.value = ''; try { config.value = await checkinAPI.config(); if (new Date(config.value.effective_at).getTime() > Date.now()) effectiveAt.value = new Date(new Date(config.value.effective_at).getTime() + 8 * 3600000).toISOString().slice(0, 16) } catch { error.value = t('checkin.loadFailed') } }
async function save() {
  if (!valid.value || !config.value || saving.value) return
  saving.value = true; error.value = ''
  try { config.value = await checkinAPI.saveConfig({ ...config.value, effective_at: new Date(`${effectiveAt.value}:00+08:00`).toISOString() }, config.value.version); app.showSuccess(t('checkin.saved')); await app.fetchPublicSettings(true) }
  catch { error.value = t('checkin.saveFailed') }
  finally { saving.value = false }
}
onMounted(load)
</script>
