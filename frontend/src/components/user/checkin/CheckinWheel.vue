<template>
  <div class="relative mx-auto my-6 h-64 w-64" role="img" :aria-label="result ? `${t('checkin.reward')}: $${formatAmount(result.reward_amount)}` : t('checkin.pool')">
    <div class="absolute -top-2 left-1/2 z-10 -translate-x-1/2 text-2xl text-primary-600" aria-hidden="true">▼</div>
    <div data-testid="checkin-wheel" :data-spin-phase="phase" class="absolute inset-0 rounded-full border-8 border-primary-100 dark:border-primary-900" :style="{ background: gradient, transform: `rotate(${rotation}deg)`, transition: 'none' }">
      <span v-for="(option, index) in options" :key="option.amount" class="absolute left-1/2 top-1/2 text-xs font-semibold text-gray-900" :style="labelStyle(index)">${{ formatAmount(option.amount) }}</span>
    </div>
    <div class="absolute left-1/2 top-1/2 grid h-28 w-28 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full bg-white p-2 text-center shadow dark:bg-dark-900"><div class="min-w-0 max-w-full"><p class="text-xs text-primary-600">{{ t(preview ? 'checkin.testPreview' : 'checkin.daily') }}</p><p data-testid="wheel-amount" class="mt-1 break-all font-bold leading-tight text-primary-600" :class="result && formatAmount(result.reward_amount).length > 6 ? 'text-sm' : 'text-xl'" aria-live="polite">{{ result ? `+$${formatAmount(result.reward_amount)}` : t(checkedIn ? 'checkin.already' : 'checkin.ready') }}</p><p v-if="result" class="text-xs">{{ t(preview ? 'checkin.testNotCredited' : 'checkin.credited') }}</p></div></div>
  </div>
</template>
<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CheckinRecord, RewardOption } from '@/api/checkin'
const props = withDefaults(defineProps<{ options: RewardOption[]; result: Pick<CheckinRecord, 'id' | 'reward_amount'> | null; checkedIn?: boolean; spinning?: boolean; preview?: boolean; animate?: boolean }>(), { animate: true })
const { t } = useI18n()
const rotation = ref(0)
const phase = ref<'idle' | 'spinning' | 'settling'>('idle')
let frame: number | undefined
let lastFrame: number | undefined
const colors = ['#99f6e4', '#ccfbf1', '#a7f3d0', '#d1fae5', '#bae6fd', '#e0f2fe']
function formatAmount(amount: string) {
  return new Intl.NumberFormat('en-US', { useGrouping: false, minimumFractionDigits: 2, maximumFractionDigits: 8 }).format(Number(amount))
}
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
function settle(target: number) {
  phase.value = 'settling'
  const start = rotation.value
  const end = Math.ceil(start / 360) * 360 + 1440 + target
  let startedAt: number | undefined
  function advance(time: number) {
    startedAt ??= time
    const progress = Math.min((time - startedAt) / 3000, 1)
    rotation.value = start + (end - start) * (1 - (1 - progress) ** 4)
    if (progress < 1) frame = requestAnimationFrame(advance)
    else { frame = undefined; phase.value = 'idle' }
  }
  // Drive rotation directly so a fast response or disabled CSS transitions
  // cannot collapse the whole draw into one repaint.
  frame = requestAnimationFrame(advance)
}
watch(() => [props.spinning, props.result?.id, props.result?.reward_amount, props.options, props.animate] as const, (_, previous) => {
  const sameDraw = previous && previous[0] === props.spinning && previous[1] === props.result?.id && previous[2] === props.result?.reward_amount && previous[4] === props.animate
  if (sameDraw && phase.value !== 'idle') return
  stopFrames()
  if (props.spinning) {
    phase.value = 'spinning'
    if (props.animate !== false) frame = requestAnimationFrame(spin)
    return
  }
  const index = props.options.findIndex(option => Number(option.amount) === Number(props.result?.reward_amount))
  if (index < 0) { phase.value = 'idle'; return }
  const target = (360 - (index + .5) / props.options.length * 360) % 360
  // Existing rewards render at their final position. A new claim spins several
  // full turns, then aligns the winning sector's centre with the top pointer.
  const newDraw = previous && (previous[0] === true || previous[1] !== props.result?.id || previous[2] !== props.result?.reward_amount)
  if (props.animate !== false && newDraw) settle(target)
  else { phase.value = 'idle'; rotation.value = target }
}, { immediate: true })
onUnmounted(stopFrames)
</script>
