<template>
  <div class="space-y-4">
    <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('modelPlaza.official.hint') }}</p>
    <div class="flex flex-wrap gap-3"><select v-model="platform" class="input w-full sm:w-48" :aria-label="t('modelPlaza.official.platform')"><option v-for="item in platforms" :key="item.id" :value="item.id">{{ item.display_name }}</option></select><input v-model="search" type="search" class="input min-w-48 flex-1" :placeholder="t('modelPlaza.square.search')" :aria-label="t('modelPlaza.square.search')" /><button class="btn btn-primary" :disabled="loading || saving" @click="editNew">{{ t('modelPlaza.official.add') }}</button></div>
    <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}<button class="ml-3 underline" @click="load">{{ t('common.refresh') }}</button></p>
    <p v-if="loading" role="status" class="py-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</p>
    <template v-else><p v-if="total > entries.length" class="text-xs text-amber-600">{{ t('modelPlaza.official.limited', { total, count: entries.length }) }}</p><div class="max-h-72 overflow-auto rounded-lg border border-gray-200 dark:border-dark-700"><table class="w-full text-left text-sm"><thead class="sticky top-0 bg-gray-50 text-xs text-gray-500 dark:bg-dark-800"><tr><th class="p-3">{{ t('modelPlaza.table.model') }}</th><th class="p-3">{{ t('modelPlaza.official.source') }}</th><th class="p-3">{{ t('modelPlaza.square.input_price') }} / 1M</th><th class="p-3"></th></tr></thead><tbody><tr v-for="entry in entries" :key="entry.model" class="border-t border-gray-100 dark:border-dark-700"><td class="break-all p-3 font-mono text-xs text-gray-900 dark:text-gray-100">{{ entry.model }}</td><td class="p-3 text-xs">{{ t(`modelPlaza.official.${entry.source}`) }}</td><td class="p-3 font-mono text-xs">{{ displayPrice(entry.pricing?.input_price) }}</td><td class="p-3 text-right"><button class="text-primary-600 dark:text-primary-300" :disabled="saving" @click="edit(entry)">{{ t('common.edit') }}</button></td></tr></tbody></table><p v-if="!entries.length" class="p-6 text-center text-sm text-gray-500">{{ t('modelPlaza.noSearchResult') }}</p></div></template>
    <form v-if="editing" class="space-y-4 rounded-lg border border-primary-200 p-4 dark:border-primary-900" @submit.prevent="save">
      <div class="flex flex-wrap items-center justify-between gap-3"><h4 class="font-semibold text-gray-900 dark:text-white">{{ t('modelPlaza.official.edit') }}</h4><button type="button" class="text-xs text-gray-500" @click="cancelEdit">{{ t('common.cancel') }}</button></div>
      <label class="block text-xs text-gray-500">{{ t('modelPlaza.table.model') }}<input v-model="model" class="input mt-1 font-mono" :disabled="existing" required maxlength="300" /></label>
      <label class="block text-xs text-gray-500">{{ t('modelPlaza.official.mode') }}<select v-model="mode" class="input mt-1"><option value="">{{ t('modelPlaza.official.automatic') }}</option><option v-for="value in ['token', 'image', 'per_request', 'video']" :key="value" :value="value">{{ value }}</option></select></label>
      <div class="grid gap-3 sm:grid-cols-2"><label v-for="field in fields" :key="field.key" class="text-xs text-gray-500">{{ t(`modelPlaza.official.${field.key}`) }}<input v-model="values[field.key]" type="number" min="0" step="any" class="input mt-1 font-mono" :placeholder="displayPrice(automatic?.[field.key], field.key === 'per_request_price' ? 1 : 1e6)" /></label></div>
      <label class="block text-xs text-gray-500">{{ t('modelPlaza.official.tiers') }}<select v-model="tiersMode" class="input mt-1"><option value="automatic">{{ t('modelPlaza.official.tiersAutomatic') }}</option><option value="custom">{{ t('modelPlaza.official.tiersCustom') }}</option><option value="none">{{ t('modelPlaza.official.tiersNone') }}</option></select></label>
      <template v-if="tiersMode === 'custom'"><p class="text-xs text-gray-500">{{ t('modelPlaza.official.tiersHint') }}</p><IntervalRow v-for="(interval, index) in intervals" :key="index" :interval="interval" :mode="mode || 'token'" @update="intervals[index] = $event" @remove="intervals.splice(index, 1)" /><button type="button" class="btn btn-secondary" @click="addTier">{{ t('modelPlaza.official.addTier') }}</button></template>
      <p v-if="formError" class="text-sm text-red-600" role="alert">{{ formError }}</p>
      <div class="flex flex-wrap justify-between gap-3"><button v-if="existingOverride" type="button" class="btn btn-secondary text-red-600" :disabled="saving" @click="reset">{{ t('modelPlaza.official.reset') }}</button><button type="submit" class="btn btn-primary ml-auto" :disabled="saving">{{ saving ? t('common.loading') : t('common.save') }}</button></div>
    </form>
  </div>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { onBeforeRouteLeave } from 'vue-router'
import { listPlatforms } from '@/constants/platformCatalog'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useAppStore } from '@/stores/app'
import { getOfficialPrice, listOfficialPrices, resetOfficialPrice, saveOfficialPrice, type OfficialPrice, type OfficialPriceEntry } from '@/api/admin/officialPricing'
import { apiIntervalsToForm, formIntervalsToAPI, validateIntervals, type IntervalFormEntry } from './types'
import type { BillingMode } from '@/constants/channel'
import IntervalRow from './IntervalRow.vue'
const { t } = useI18n(), app = useAppStore()
const platforms = computed(listPlatforms), platform = ref('openai'), search = ref('')
const entries = ref<OfficialPriceEntry[]>([]), total = ref(0), loading = ref(false), saving = ref(false), error = ref('')
const editing = ref(false), existing = ref(false), existingOverride = ref(false), model = ref(''), mode = ref<BillingMode | ''>(''), formError = ref('')
const fields = ['input_price', 'output_price', 'cache_write_price', 'cache_write_1h_price', 'cache_read_price', 'image_input_price', 'image_output_price', 'per_request_price'].map(key => ({ key: key as keyof Omit<OfficialPrice, 'billing_mode' | 'intervals'> }))
type PriceKey = typeof fields[number]['key']
const values = ref<Record<PriceKey, string | number>>(Object.fromEntries(fields.map(f => [f.key, ''])) as Record<PriceKey, string | number>)
const automatic = ref<OfficialPrice | null>(null), intervals = ref<IntervalFormEntry[]>([]), tiersMode = ref('automatic')
let initial = '', timer: ReturnType<typeof setTimeout> | undefined, request: AbortController | undefined
const fingerprint = () => JSON.stringify([model.value, mode.value, values.value, tiersMode.value, intervals.value])
const dirty = computed(() => editing.value && fingerprint() !== initial)
defineExpose({ canClose: () => !dirty.value || window.confirm(t('modelPlaza.official.unsaved')) })
function confirmDiscard() { return !dirty.value || window.confirm(t('modelPlaza.official.unsaved')) }
function cancelEdit() { if (confirmDiscard()) editing.value = false }
async function load() {
  request?.abort(); const current = new AbortController(); request = current; loading.value = true; error.value = ''
  try { const result = await listOfficialPrices(platform.value, search.value.trim(), current.signal); if (!current.signal.aborted) { entries.value = result.items; total.value = result.total } }
  catch (e) { if (!current.signal.aborted) error.value = extractApiErrorMessage(e, t('modelPlaza.loadFailed')) }
  finally { if (!current.signal.aborted) loading.value = false }
}
function populate(entry?: OfficialPriceEntry) {
  const price = entry?.override
  model.value = entry?.model ?? search.value.trim(); existing.value = !!entry; existingOverride.value = !!price; automatic.value = entry?.automatic ?? null
  mode.value = price?.billing_mode ?? ''; tiersMode.value = price?.intervals == null ? 'automatic' : price.intervals.length ? 'custom' : 'none'
  intervals.value = apiIntervalsToForm((price?.intervals ?? []).map((tier, index) => ({
    ...tier, tier_label: tier.tier_label ?? '', sort_order: index,
    input_multiplier: tier.input_multiplier ?? null,
    output_multiplier: tier.output_multiplier ?? null,
    cache_write_multiplier: tier.cache_write_multiplier ?? null,
    cache_read_multiplier: tier.cache_read_multiplier ?? null
  })))
  for (const f of fields) values.value[f.key] = price?.[f.key] == null ? '' : String(price[f.key]! * (f.key === 'per_request_price' ? 1 : 1e6))
  formError.value = ''; editing.value = true; initial = fingerprint()
}
function edit(entry: OfficialPriceEntry) { if (confirmDiscard()) populate(entry) }
async function editNew() {
  if (!confirmDiscard()) return
  if (search.value.trim()) { try { const entry = await getOfficialPrice(platform.value, search.value.trim()); populate(entry); return } catch { /* A missing model may be maintained manually. */ } }
  populate()
}
function addTier() { intervals.value.push({ min_tokens: 0, max_tokens: null, tier_label: '', input_price: null, output_price: null, cache_write_price: null, cache_read_price: null, per_request_price: null, input_multiplier: null, output_multiplier: null, cache_write_multiplier: null, cache_read_multiplier: null, sort_order: intervals.value.length }) }
const displayPrice = (n: number | null | undefined, scale = 1e6) => n == null ? t('modelPlaza.official.fallback') : `$${Number((n * scale).toPrecision(10))}`
async function save() {
  formError.value = ''; const price: OfficialPrice = { billing_mode: mode.value || null }
  for (const f of fields) { const raw = String(values.value[f.key]).trim(); const n = Number(raw); if (raw && (!Number.isFinite(n) || n < 0)) { formError.value = t('modelPlaza.official.invalid'); return } price[f.key] = raw ? n / (f.key === 'per_request_price' ? 1 : 1e6) : null }
  if (tiersMode.value === 'custom') { const invalid = validateIntervals(intervals.value, mode.value || 'token', t); if (invalid) { formError.value = invalid; return } price.intervals = formIntervalsToAPI(intervals.value) } else price.intervals = tiersMode.value === 'none' ? [] : null
  saving.value = true
  try { const entry = await saveOfficialPrice(platform.value, model.value.trim(), price); populate(entry); app.showSuccess(t('modelPlaza.official.saved')); await load() }
  catch (e) { formError.value = extractApiErrorMessage(e, t('modelPlaza.official.saveFailed')) } finally { saving.value = false }
}
async function reset() {
  if (!window.confirm(t('modelPlaza.official.resetConfirm'))) return
  saving.value = true
  try { await resetOfficialPrice(platform.value, model.value); editing.value = false; app.showSuccess(t('modelPlaza.official.saved')); await load() } catch (e) { formError.value = extractApiErrorMessage(e, t('modelPlaza.official.saveFailed')) } finally { saving.value = false }
}
let revertingPlatform = false
watch(platform, (_next, previous) => {
  if (revertingPlatform) { revertingPlatform = false; return }
  if (!confirmDiscard()) { revertingPlatform = true; platform.value = previous; return }
  editing.value = false; entries.value = []; void load()
})
watch(search, () => { clearTimeout(timer); timer = setTimeout(load, 300) })
onBeforeRouteLeave(confirmDiscard)
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = '' } }
onMounted(() => { void load(); window.addEventListener('beforeunload', beforeUnload) })
onBeforeUnmount(() => { clearTimeout(timer); request?.abort(); window.removeEventListener('beforeunload', beforeUnload) })
</script>
