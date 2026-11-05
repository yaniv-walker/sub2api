<template>
  <div class="model-square space-y-5">
    <header v-if="!embedded"><h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('modelPlaza.title') }}</h1><p class="mt-1 text-sm text-gray-500">{{ t('modelPlaza.description') }}</p></header>
    <div v-if="descriptionHtml" class="prose prose-sm max-w-none dark:prose-invert" v-html="descriptionHtml"></div>
    <p v-if="!auth.isAuthenticated" class="text-xs text-gray-500">{{ t('modelPlaza.anonymousHint') }}</p>
    <div class="square-stats"><div v-for="stat in stats" :key="stat.label" class="square-stat"><span>{{ stat.label }}</span><strong>{{ stat.value }}</strong></div><button type="button" class="btn btn-secondary self-center" :disabled="loading" :aria-label="t('common.refresh')" @click="$emit('refresh')"><Icon name="refresh" size="md" :class="{ 'animate-spin': loading }" /></button></div>
    <div class="flex flex-wrap items-center gap-3">
      <input v-model="search" type="search" class="input min-w-48 flex-1 sm:max-w-sm" :aria-label="t('modelPlaza.square.search')" :placeholder="t('modelPlaza.square.search')" />
      <select v-model="groupId" class="input w-full sm:w-48" :aria-label="t('modelPlaza.square.groups')"><option value="">{{ t('modelPlaza.square.allGroups') }}</option><option v-for="group in groupOptions" :key="group.id" :value="String(group.id)">{{ group.name }}</option></select>
      <select v-model="sort" class="input w-full sm:w-40" :aria-label="t('modelPlaza.square.sort')"><option value="name">{{ t('modelPlaza.square.sortName') }}</option><option value="asc">{{ t('modelPlaza.square.sortAsc') }}</option><option value="desc">{{ t('modelPlaza.square.sortDesc') }}</option></select>
      <span class="text-xs text-gray-500">{{ filtered.length }} / {{ models.length }}</span>
      <div class="ml-auto flex gap-1 rounded-lg border border-gray-200 p-1 dark:border-dark-600"><button v-for="mode in (['grid', 'list'] as const)" :key="mode" type="button" class="rounded-md p-2" :class="view === mode ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300' : 'text-gray-500'" :aria-label="t(`modelPlaza.square.${mode}`)" :aria-pressed="view === mode" @click="view = mode"><Icon :name="mode === 'grid' ? 'grid' : 'menu'" size="sm" /></button></div>
    </div>
    <div class="square-shell">
      <aside class="square-catalog" :aria-label="t('modelPlaza.square.catalog')"><h2 class="mb-3 text-xs font-semibold tracking-wider text-gray-500">{{ t('modelPlaza.square.catalog') }}</h2><nav class="square-catalog-items">
        <button type="button" class="catalog-item" :class="{ active: !provider }" :aria-pressed="!provider" @click="provider = ''"><span>{{ t('modelPlaza.square.allProviders') }}</span><b>{{ models.length }}</b></button>
        <button v-for="section in providers" :key="section.platform" type="button" class="catalog-item" :class="{ active: provider === section.platform }" :style="{ '--provider': platformAccentColor(section.platform) }" :aria-pressed="provider === section.platform" @click="provider = provider === section.platform ? '' : section.platform"><span class="min-w-0"><span class="flex items-center gap-2"><span class="provider-dot"></span>{{ platformLabel(section.platform) }}</span><small>{{ section.groupCount }} {{ t('modelPlaza.square.groups') }} · {{ rateText(section.rate) }}</small></span><b>{{ section.models.length }}</b></button>
      </nav></aside>
      <div class="min-w-0">
        <div v-if="loading" class="grid min-h-64 place-items-center text-gray-500" role="status">{{ t('common.loading') }}</div>
        <div v-else-if="error" class="rounded-lg border border-red-200 bg-red-50 p-6 text-red-700 dark:bg-red-950/20 dark:text-red-300" role="alert">{{ t('modelPlaza.loadFailed') }}<button class="btn btn-secondary ml-3" @click="$emit('refresh')">{{ t('common.refresh') }}</button></div>
        <div v-else-if="!filtered.length" class="grid min-h-64 place-content-center gap-3 rounded-xl border border-dashed border-gray-300 px-5 text-center dark:border-dark-600"><p class="text-sm text-gray-500">{{ activeFilters ? t('modelPlaza.noSearchResult') : t('modelPlaza.empty') }}</p><button v-if="activeFilters" class="btn btn-secondary justify-self-center" @click="clearFilters">{{ t('modelPlaza.square.clear') }}</button></div>
        <div v-else-if="view === 'grid'" class="space-y-6"><section v-for="section in sections" :key="section.platform" :style="{ '--provider': platformAccentColor(section.platform) }">
          <h2 class="mb-3 flex items-center gap-2 text-sm font-semibold text-gray-900 dark:text-gray-100"><span class="provider-dot"></span>{{ platformLabel(section.platform) }}<span class="ml-auto text-xs font-normal text-gray-500">{{ section.models.length }} {{ t('modelPlaza.square.models') }}</span></h2>
          <div class="square-model-grid"><article v-for="model in section.models" :key="model.key" class="model-card" role="button" tabindex="0" :aria-label="`${t('modelPlaza.square.copy')} ${model.name}`" @click="copy(model.name)" @keydown.enter.prevent="copy(model.name)" @keydown.space.prevent="copy(model.name)">
            <span class="text-xs text-gray-500">{{ platformLabel(model.platform) }}</span><div class="mb-5 mt-2 flex items-start gap-2"><h3 class="break-all text-base font-semibold text-gray-950 dark:text-white">{{ model.name }}</h3><button type="button" class="ml-auto shrink-0 text-gray-400 hover:text-primary-600" :aria-label="t('modelPlaza.square.copy')" @click.stop="copy(model.name)"><Icon name="copy" size="sm" /></button></div>
            <ModelPriceBoxes :offer="chosen(model)" /><p v-if="complexPricing(model)" class="mt-2 text-[11px] text-gray-500">{{ t('modelPlaza.square.tierHint') }}</p>
            <footer class="mt-auto flex flex-wrap items-center gap-2 pt-4"><span class="rate-chip">{{ rateText(effectiveRate(chosen(model).group)) }}</span><button type="button" class="group-chip" :disabled="!!groupId" @click.stop="groupDialog = model">{{ chosen(model).group.name }}<b v-if="model.offers.length > 1"> +{{ model.offers.length - 1 }}</b></button><button v-if="!groupId" type="button" class="detail-button" @click.stop="detail = model">{{ t('modelPlaza.square.detail') }}</button></footer>
          </article></div>
        </section></div>
        <div v-else class="overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-700"><table class="w-full min-w-[850px] text-left text-sm"><thead class="bg-gray-50 text-xs text-gray-500 dark:bg-dark-800"><tr><th class="p-4">{{ t('modelPlaza.square.provider') }}</th><th class="p-4">{{ t('modelPlaza.table.model') }}</th><th class="p-4">{{ t('modelPlaza.square.prices') }}</th><th class="p-4">{{ t('modelPlaza.square.groups') }}</th><th class="p-4"></th></tr></thead>
          <tbody><tr v-for="model in filtered" :key="model.key" class="border-t border-gray-100 dark:border-dark-700"><td class="p-4 text-gray-500">{{ platformLabel(model.platform) }}</td><td class="p-4 font-semibold text-gray-950 dark:text-white"><button type="button" class="break-all text-left hover:text-primary-600" @click="copy(model.name)">{{ model.name }}</button></td><td class="min-w-72 p-4"><ModelPriceBoxes :offer="chosen(model)" /></td><td class="max-w-64 p-4"><div class="flex flex-wrap gap-1"><button v-for="offer in sortedOffers(model)" :key="offer.group.id" type="button" class="group-chip" :disabled="!!groupId" @click="groupDialog = model">{{ offer.group.name }} {{ rateText(effectiveRate(offer.group)) }}</button></div></td><td class="p-4"><button v-if="!groupId" type="button" class="detail-button" @click="detail = model">{{ t('modelPlaza.square.detail') }}</button></td></tr></tbody>
        </table></div>
      </div>
    </div>
    <p v-if="notice" class="fixed bottom-6 left-1/2 z-[60] -translate-x-1/2 rounded-lg bg-gray-900 px-5 py-3 text-sm text-white shadow-lg" role="status">{{ notice }}</p>
    <BaseDialog :show="!!groupDialog" :title="`${groupDialog?.name ?? ''} · ${t('modelPlaza.square.groups')}`" width="wide" @close="groupDialog = null"><div v-if="groupDialog" class="space-y-2"><div v-for="offer in sortedOffers(groupDialog)" :key="offer.group.id" class="flex justify-between gap-4 rounded-lg bg-gray-50 p-3 dark:bg-dark-700"><span class="text-sm text-gray-900 dark:text-white">{{ offer.group.name }}<code class="ml-2 text-xs text-gray-400">#{{ offer.group.id }}</code></span><b class="rate-chip">{{ rateText(effectiveRate(offer.group)) }}</b></div></div><template #footer><button class="btn btn-secondary" @click="groupDialog = null">{{ t('common.close') }}</button></template></BaseDialog>
    <BaseDialog :show="!!detail" :title="`${detail?.name ?? ''} · ${t('modelPlaza.square.detail')}`" width="extra-wide" @close="detail = null"><div v-if="detail" class="space-y-4"><p class="text-xs text-gray-500">{{ t('modelPlaza.square.detailHint') }}</p><PlazaGroupSection v-for="offer in sortedOffers(detail)" :key="offer.group.id" :group="{ ...offer.group, models: [offer.model] }" /></div><template #footer><button class="btn btn-secondary" @click="detail = null">{{ t('common.close') }}</button></template></BaseDialog>
  </div>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PlazaGroupSection from './PlazaGroupSection.vue'
import ModelPriceBoxes from './ModelPriceBoxes.vue'
import type { ModelPlazaResponse } from '@/api/modelPlaza'
import { useAuthStore } from '@/stores/auth'
import { platformAccentColor, platformLabel } from '@/utils/platformColors'
import { aggregateModels, effectiveRate, modelPrices, selectOffer, type SquareModel } from './modelSquare'
const props = defineProps<{ response: ModelPlazaResponse | null; loading: boolean; error?: boolean; embedded?: boolean }>()
defineEmits<{ refresh: [] }>()
const { t } = useI18n()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const queryString = (key: string) => typeof route.query[key] === 'string' ? route.query[key] as string : ''
const provider = ref(queryString('provider')), groupId = ref(queryString('group')), search = ref(queryString('model'))
const sort = ref('name'), view = ref<'grid' | 'list'>('grid')
const detail = ref<SquareModel | null>(null), groupDialog = ref<SquareModel | null>(null), notice = ref('')
let noticeTimer: ReturnType<typeof setTimeout> | undefined
const descriptionHtml = computed(() => DOMPurify.sanitize(marked.parse(props.response?.description ?? '') as string))
const models = computed(() => aggregateModels(props.response?.groups ?? []))
const providers = computed(() => [...new Set(models.value.map(m => m.platform))].sort().map(platform => {
  const entries = models.value.filter(m => m.platform === platform), offers = entries.flatMap(m => m.offers)
  return { platform, models: entries, groupCount: new Set(offers.map(o => o.group.id)).size, rate: Math.min(...offers.map(o => effectiveRate(o.group))) }
}))
const stats = computed(() => [{ label: t('modelPlaza.square.modelCount'), value: models.value.length }, { label: t('modelPlaza.square.availableCount'), value: models.value.filter(m => m.offers.length).length }, { label: t('modelPlaza.square.groupCount'), value: props.response?.groups.length ?? 0 }])
const groupOptions = computed(() => (props.response?.groups ?? []).filter(g => !provider.value || models.value.some(m => m.platform === provider.value && m.offers.some(o => o.group.id === g.id))))
watch(groupOptions, options => { if (!props.loading && groupId.value && !options.some(g => String(g.id) === groupId.value)) groupId.value = '' })
watch(() => route.query, () => { provider.value = queryString('provider'); groupId.value = queryString('group'); search.value = queryString('model') })
watch([provider, groupId, search], () => {
  const query = { ...route.query }
  for (const [key, value] of [['provider', provider.value], ['group', groupId.value], ['model', search.value]]) { if (value) query[key!] = value; else delete query[key!] }
  if (JSON.stringify(query) !== JSON.stringify(route.query)) void router.replace({ query })
})
const chosen = (model: SquareModel) => selectOffer(model, groupId.value) ?? selectOffer(model)!
const sortedOffers = (model: SquareModel) => [...model.offers].sort((a, b) => effectiveRate(a.group) - effectiveRate(b.group) || a.group.id - b.group.id)
const rateText = (rate: number) => `${Number(rate.toFixed(3))}x`
const activeFilters = computed(() => !!(provider.value || groupId.value || search.value.trim()))
const filtered = computed(() => models.value.filter(m => (!provider.value || m.platform === provider.value) && (!groupId.value || m.offers.some(o => String(o.group.id) === groupId.value)) && `${m.name} ${m.platform} ${platformLabel(m.platform)} ${m.offers[0]?.model.pricing?.billing_mode ?? ''}`.toLowerCase().includes(search.value.trim().toLowerCase())).sort((a, b) => {
  if (sort.value !== 'name') { const pa = modelPrices(chosen(a))[0]?.value, pb = modelPrices(chosen(b))[0]?.value; if (pa == null || pb == null) { if (pa != null) return -1; if (pb != null) return 1 } else if (pa !== pb) return sort.value === 'asc' ? pa - pb : pb - pa }
  return a.name.localeCompare(b.name) || a.platform.localeCompare(b.platform)
}))
const sections = computed(() => providers.value.map(section => ({ ...section, models: filtered.value.filter(m => m.platform === section.platform) })).filter(section => section.models.length).sort((a, b) => a.rate - b.rate || a.platform.localeCompare(b.platform)))
const complexPricing = (m: SquareModel) => !!(chosen(m).model.pricing?.intervals?.length || chosen(m).model.time_pricing?.periods?.length || chosen(m).group.peak_rate_enabled)
function clearFilters() { provider.value = ''; groupId.value = ''; search.value = '' }
async function copy(id: string) {
  try {
    if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(id)
    else { const field = document.createElement('textarea'); field.value = id; field.style.position = 'fixed'; field.style.opacity = '0'; document.body.appendChild(field); try { field.select(); if (!document.execCommand('copy')) throw new Error('copy failed') } finally { field.remove() } }
    notice.value = t('modelPlaza.square.copied')
  } catch { notice.value = t('modelPlaza.square.copyFailed') }
  clearTimeout(noticeTimer); noticeTimer = setTimeout(() => { notice.value = '' }, 2000)
}
onBeforeUnmount(() => clearTimeout(noticeTimer))
</script>
<style scoped>
.square-stats { display:flex; align-items:stretch; flex-wrap:wrap; gap:.75rem; }.square-stat { flex:1 1 130px; padding:1rem 1.25rem; border:1px solid #e5e7eb; border-radius:.75rem; background:white; }.square-stat span { display:block; color:#64748b; font-size:.75rem; }.square-stat strong { display:block; margin-top:.25rem; font-size:1.5rem; color:#0f172a; }
.square-shell { display:grid; grid-template-columns:190px minmax(0,1fr); gap:1.5rem; }.square-catalog { align-self:start; position:sticky; top:1rem; }.square-catalog-items { display:flex; flex-direction:column; gap:.5rem; }.catalog-item { --provider:#0d9488; display:flex; align-items:center; justify-content:space-between; gap:.5rem; padding:.75rem; border:1px solid #e5e7eb; border-radius:.5rem; background:white; text-align:left; color:#334155; font-size:.8rem; }.catalog-item small { display:block; margin-top:.3rem; color:#64748b; font-size:.6rem; }.catalog-item b { font-size:.7rem; }.catalog-item.active { border-color:var(--provider); background:color-mix(in srgb,var(--provider) 8%,white); }.provider-dot { width:.5rem; height:.5rem; border-radius:50%; background:var(--provider,#0d9488); flex-shrink:0; }
.square-model-grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:1rem; }.model-card { display:flex; flex-direction:column; padding:1.25rem; border:1px solid #e5e7eb; border-top:3px solid var(--provider,#0d9488); border-radius:.6rem; background:white; box-shadow:0 2px 5px #0f172a08; cursor:pointer; transition:box-shadow .15s,border-color .15s; }.model-card:hover { box-shadow:0 5px 15px #0f172a12; }.model-card:focus-visible { outline:2px solid var(--provider); outline-offset:3px; }
.rate-chip { display:inline-flex; padding:.25rem .45rem; border-radius:.3rem; background:#fffbeb; color:#b45309; font:600 .7rem ui-monospace,monospace; }.group-chip { max-width:65%; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; padding:.25rem .4rem; border:1px solid #e2e8f0; border-radius:.3rem; font-size:.65rem; color:#475569; }.detail-button { margin-left:auto; padding:.3rem .45rem; border:1px solid #e2e8f0; border-radius:.3rem; white-space:nowrap; font-size:.7rem; color:#475569; }
:global(.dark .model-square .square-stat),:global(.dark .model-square .model-card),:global(.dark .model-square .catalog-item){ border-color:#334155; background:#1e293b; color:#cbd5e1; }:global(.dark .model-square .square-stat strong){ color:#f8fafc; }:global(.dark .model-square .catalog-item.active){ background:color-mix(in srgb,var(--provider) 12%,#1e293b); border-color:var(--provider); }:global(.dark .model-square .group-chip),:global(.dark .model-square .detail-button){ border-color:#475569; color:#cbd5e1; }:global(.dark .model-square .rate-chip){ background:#78350f40; color:#fbbf24; }
@media(min-width:1500px) { .square-model-grid { grid-template-columns:repeat(3,minmax(0,1fr)); } }@media(max-width:900px) { .square-shell { grid-template-columns:minmax(0,1fr); gap:1rem; }.square-catalog { position:static; }.square-catalog-items { flex-direction:row; overflow-x:auto; padding-bottom:.5rem; }.catalog-item { flex-shrink:0; white-space:nowrap; }.catalog-item small { display:none; } }@media(max-width:560px) { .square-model-grid { grid-template-columns:minmax(0,1fr); }.square-stat { padding:.75rem; }.model-card { padding:1rem; } }
</style>
