<template>
  <div class="relative mx-auto my-6 h-64 w-64" role="img" :aria-label="result ? `${t('checkin.reward')}: $${result.reward_amount}` : t('checkin.pool')">
    <div class="absolute -top-2 left-1/2 z-10 -translate-x-1/2 text-2xl text-primary-600" aria-hidden="true">▼</div>
    <div data-testid="checkin-wheel" class="absolute inset-0 rounded-full border-8 border-primary-100 dark:border-primary-900" :style="{ background: gradient, transform: `rotate(${rotation}deg)`, transition: settling && !reducedMotion ? 'transform 3s cubic-bezier(0.12, 0.7, 0.12, 1)' : 'none' }">
      <span v-for="(option, index) in options" :key="option.amount" class="absolute left-1/2 top-1/2 text-xs font-semibold text-gray-900" :style="labelStyle(index)">${{ option.amount }}</span>
    </div>
    <div class="absolute left-1/2 top-1/2 grid h-28 w-28 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full bg-white p-2 text-center shadow dark:bg-dark-900"><div><p class="text-xs text-primary-600">{{ t('checkin.daily') }}</p><p class="mt-1 text-xl font-bold text-primary-600" aria-live="polite">{{ result ? `+$${result.reward_amount}` : t(checkedIn ? 'checkin.already' : 'checkin.ready') }}</p><p v-if="result" class="text-xs">{{ t('checkin.credited') }}</p></div></div>
  </div>
</template>
<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CheckinRecord, RewardOption } from '@/api/checkin'
const props = defineProps<{ options: RewardOption[]; result: CheckinRecord | null; checkedIn?: boolean; spinning?: boolean }>()
const { t } = useI18n()
const rotation = ref(0)
const settling = ref(false)
let frame: number | undefined
let lastFrame: number | undefined
const reducedMotion = typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
const colors = ['#99f6e4', '#ccfbf1', '#a7f3d0', '#d1fae5', '#bae6fd', '#e0f2fe']
const gradient = computed(() => props.options.length ? `conic-gradient(${props.options.map((_, i) => `${colors[i % colors.length]} ${i / props.options.length * 100}% ${(i + 1) / props.options.length * 100}%`).join(',')})` : '#ccfbf1')
function labelStyle(index: number) { const angle = (index + .5) / props.options.length * 360 - 90; return { transform: `translate(-50%, -50%) rotate(${angle}deg) translateX(88px) rotate(${-angle}deg)` } }
function stopFrames() {
  if (frame !== undefined) cancelAnimationFrame(frame)
  frame = undefined
  lastFrame = undefined
}
function spin(time: number) {
  if (lastFrame !== undefined) rotation.value += Math.min(time - lastFrame, 100) * 0.72
  lastFrame = time
  frame = requestAnimationFrame(spin)
}
watch(() => [props.spinning, props.result?.id, props.result?.reward_amount, props.options] as const, (_, previous) => {
  stopFrames()
  if (props.spinning) {
    settling.value = false
    if (!reducedMotion) frame = requestAnimationFrame(spin)
    return
  }
  const index = props.options.findIndex(option => Number(option.amount) === Number(props.result?.reward_amount))
  if (index < 0) { settling.value = false; return }
  const target = (360 - (index + .5) / props.options.length * 360) % 360
  // Existing rewards render at their final position. A new claim spins several
  // full turns, then aligns the winning sector's centre with the top pointer.
  settling.value = !!previous && !reducedMotion && (previous[0] === true || previous[1] !== props.result?.id || previous[2] !== props.result?.reward_amount)
  rotation.value = settling.value ? Math.ceil(rotation.value / 360) * 360 + 1440 + target : target
}, { immediate: true })
onUnmounted(stopFrames)
</script>
