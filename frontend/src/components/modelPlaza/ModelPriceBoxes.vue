<template>
  <div class="price-boxes">
    <div v-for="(box, index) in boxes" :key="box.key" class="price-box" :class="`price-tone-${index}`">
      <span>{{ t(`modelPlaza.square.${box.key}`) }}</span>
      <strong>{{ box.value == null ? t('modelPlaza.square.unset') : `$${amount(box.value)}` }}</strong>
      <s v-if="box.reference != null && box.value != null && box.value < box.reference">${{ amount(box.reference) }}</s>
      <small>{{ box.perToken ? t('modelPlaza.table.unitPerMillion') : t(`modelPlaza.square.${box.key}Unit`) }}</small>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { modelPrices, type ModelOffer } from './modelSquare'
const props = defineProps<{ offer: ModelOffer }>()
const { t } = useI18n()
const boxes = computed(() => modelPrices(props.offer))
const amount = (value: number) => new Intl.NumberFormat('en-US', { maximumFractionDigits: 6 }).format(value)
</script>
<style scoped>
.price-boxes { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:.5rem; }
.price-box { min-width:0; display:flex; flex-direction:column; padding:.65rem .75rem; border:1px solid color-mix(in srgb,var(--price-color) 30%,transparent); border-radius:.5rem; background:color-mix(in srgb,var(--price-color) 7%,transparent); color:var(--price-color); gap:.15rem; }
.price-box span { font-size:.7rem; }.price-box strong { font-family:ui-monospace,monospace; font-size:.95rem; }.price-box small,.price-box s { font-size:.65rem; opacity:.75; }
.price-tone-0 { --price-color:#0d9488; }.price-tone-1 { --price-color:#ea580c; }.price-tone-2 { --price-color:#2563eb; }.price-tone-3 { --price-color:#7c3aed; }
:global(.dark .model-square .price-tone-0){ --price-color:#5eead4; }:global(.dark .model-square .price-tone-1){ --price-color:#fdba74; }:global(.dark .model-square .price-tone-2){ --price-color:#93c5fd; }:global(.dark .model-square .price-tone-3){ --price-color:#c4b5fd; }
</style>
