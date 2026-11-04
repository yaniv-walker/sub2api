<script setup lang="ts">
/**
 * 作品渲染框。
 *
 * 后端抽出来的 SVG 是**上游模型返回的不可信内容**。这里绝不用 v-html
 * 直接插进主文档 —— 那等于把上游的 <script> 执行在自己的域名下，
 * 可以读走 localStorage 里的登录态。
 *
 * 做法与 iq-benchmark 作品展一致：
 *   - 先 sanitizeSVG 洗一遍（去 script / on事件 / javascript: 协议）
 *   - 再塞进 sandbox="" 的 iframe（无任何权限，连脚本都不允许）
 *   - 只给「测量高度」用的隐藏帧 allow-same-origin，且不加载任何脚本
 *
 * 复用既有 sanitize / svgFrame，而不是再写一套：安全代码分叉两份，
 * 迟早有一份会漏掉某个向量。
 */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { sanitizeSVG, isUsableSVG } from '@/features/iq-benchmark/sanitize'
import { MEASURE_SANDBOX, VISIBLE_SANDBOX, svgDocument } from '@/features/iq-benchmark/svgFrame'

const props = withDefaults(
  defineProps<{
    /** 原始 SVG 源码。 */
    source: string
    /** 放大查看模式。 */
    zoom?: boolean
    /** 固定宽高比（宽/高）；0 表示按内容自适应。 */
    aspect?: number
  }>(),
  {
    zoom: false,
    aspect: 0
  }
)

const emit = defineEmits<{
  (e: 'expand'): void
}>()

/** 洗过的、可安全渲染的 SVG。 */
const safe = computed(() => sanitizeSVG(props.source))
const usable = computed(() => isUsableSVG(safe.value))

/** 可见帧的 srcdoc。 */
const srcdoc = computed(() => (usable.value ? svgDocument(safe.value) : ''))

/** 测量帧，用来量出内容真实高度。 */
const measureFrame = ref<HTMLIFrameElement | null>(null)
/** 量出来的高度（px）。 */
const measuredHeight = ref(0)

let observer: ResizeObserver | null = null

/**
 * 量高度。
 *
 * 必须在 allow-same-origin 的隐藏帧里做 —— 可见帧是 sandbox=""，
 * 跨源访问不到 contentDocument。
 */
function measure() {
  const frame = measureFrame.value
  if (!frame || !usable.value) return

  const doc = frame.contentDocument
  if (!doc) return

  const svg = doc.querySelector('svg')
  if (!svg) return

  // 优先用 viewBox 的宽高比算，比 scrollHeight 稳。
  const ratio = readAspect(svg)
  if (ratio > 0) {
    const width = frame.clientWidth || 240
    measuredHeight.value = Math.round(width / ratio)
    return
  }

  const box = svg.getBoundingClientRect()
  if (box.height > 1) {
    measuredHeight.value = Math.round(box.height)
  }
}

/** 从 svg 元素读宽高比。 */
function readAspect(svg: Element): number {
  const viewBox = svg.getAttribute('viewBox')
  if (viewBox) {
    const parts = viewBox.trim().split(/[\s,]+/).map(Number)
    if (parts.length === 4 && parts[2] > 0 && parts[3] > 0) {
      return parts[2] / parts[3]
    }
  }
  const w = parseFloat(svg.getAttribute('width') || '')
  const h = parseFloat(svg.getAttribute('height') || '')
  if (w > 0 && h > 0) return w / h
  return 0
}

/** 最终应用到可见帧的高度。 */
const frameHeight = computed(() => {
  if (props.aspect > 0) {
    // 调用方指定了比例（历史卡片缩略图），按比例算，避免抖动。
    return undefined
  }
  if (measuredHeight.value > 0) return `${measuredHeight.value}px`
  return undefined
})

onMounted(() => {
  if (measureFrame.value) {
    observer = new ResizeObserver(() => measure())
    observer.observe(measureFrame.value)
  }
  measure()
})

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
})

watch(safe, () => {
  measuredHeight.value = 0
  measure()
})
</script>

<template>
  <div class="ma-artwork" :class="{ 'is-zoom': zoom }" :style="aspect > 0 ? { aspectRatio: String(aspect) } : undefined">
    <template v-if="usable">
      <iframe
        class="ma-artwork-frame"
        :srcdoc="srcdoc"
        :sandbox="VISIBLE_SANDBOX"
        :style="frameHeight ? { height: frameHeight } : undefined"
        loading="lazy"
        referrerpolicy="no-referrer"
        title="artwork"
      />
      <!-- 仅供测量，不显示；同样不加载脚本。 -->
      <iframe
        ref="measureFrame"
        class="ma-artwork-measure"
        :srcdoc="srcdoc"
        :sandbox="MEASURE_SANDBOX"
        aria-hidden="true"
        tabindex="-1"
      />
      <button v-if="!zoom" type="button" class="ma-artwork-expand" @click="emit('expand')">
        <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
          <path
            d="M6 2H2v4M10 2h4v4M14 10v4h-4M2 10v4h4"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
          />
        </svg>
      </button>
    </template>
    <div v-else class="ma-artwork-empty">
      <svg viewBox="0 0 40 40" width="28" height="28" aria-hidden="true">
        <rect x="5" y="8" width="30" height="24" rx="3" fill="none" stroke="currentColor" stroke-width="1.6" />
        <path d="M5 26l8-7 6 5 6-6 10 8" fill="none" stroke="currentColor" stroke-width="1.6" />
      </svg>
    </div>
  </div>
</template>

<style scoped>
.ma-artwork {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  overflow: hidden;
  border-radius: 10px;
  background: var(--ma-input-bg);
}

.ma-artwork.is-zoom {
  background: transparent;
}

.ma-artwork-frame {
  width: 100%;
  border: 0;
  display: block;
  background: transparent;
}

/* 测量帧必须留在渲染树里（display:none 量不到尺寸），
   但要彻底不可见、不可交互、不占位。 */
.ma-artwork-measure {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 0;
  border: 0;
  opacity: 0;
  pointer-events: none;
  visibility: hidden;
}

.ma-artwork-expand {
  position: absolute;
  top: 6px;
  right: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  padding: 0;
  border: 1px solid var(--ma-border);
  border-radius: 6px;
  background: var(--ma-panel);
  color: var(--ma-control-fg);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.16s ease;
}

.ma-artwork:hover .ma-artwork-expand {
  opacity: 1;
}

.ma-artwork-expand:focus-visible {
  opacity: 1;
  outline: 2px solid var(--ma-primary);
  outline-offset: 1px;
}

.ma-artwork-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  aspect-ratio: 1;
  color: var(--ma-faint);
}
</style>
