<template>
  <div class="iq-svg-frame" :style="containerStyle">
    <!--
      可见帧：sandbox=""（最严格），永远不给 allow-scripts。
      用 srcdoc 而不是 src=data: —— sandbox 对 srcdoc 同样生效，
      而 data: URL 在部分浏览器里会被当作独立的不透明来源，
      高度测量与样式隔离的行为都更难预测。
    -->
    <iframe
      v-if="document"
      :srcdoc="document"
      :sandbox="VISIBLE_SANDBOX"
      class="iq-svg-visible"
      :title="title"
      loading="lazy"
      referrerpolicy="no-referrer"
      @load="$emit('rendered')"
    ></iframe>

    <!--
      隐藏测量帧：唯一带 allow-same-origin 的帧，且**只**用它读尺寸。
      为什么非要单独一个帧：可见帧是 sandbox=""，父页面读不到它的
      contentDocument（这正是隔离起作用的表现）。要在不放宽可见帧的
      前提下知道 SVG 多高，只能另开一个同源帧量一次。
      量完留在文档里但不可见 —— 移除它会让 Vue 重建可见帧，造成闪烁。
    -->
    <iframe
      ref="measureFrame"
      :srcdoc="document"
      :sandbox="MEASURE_SANDBOX"
      class="iq-svg-measure"
      aria-hidden="true"
      tabindex="-1"
      title=""
      @load="measure"
    ></iframe>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { MEASURE_SANDBOX, VISIBLE_SANDBOX, aspectFromDocument, frameHeightFor, svgDocument } from './svgFrame'

const props = withDefaults(
  defineProps<{
    /** 已经切好的 SVG 片段（不含围栏、不含尾部说明文字）。 */
    svg: string
    /** iframe 的可访问名称。 */
    title?: string
    /** 测量失败时的兜底高度（px）。 */
    fallbackHeight?: number
    /** 宽高比夹取范围，避免极扁/极瘦的图把卡片压成一条线或撑成一片空白。 */
    maxAspect?: number
  }>(),
  { title: 'SVG preview', fallbackHeight: 240, maxAspect: 3 },
)

defineEmits<{
  (e: 'rendered'): void
}>()

const measureFrame = ref<HTMLIFrameElement | null>(null)
/** SVG 的宽高比（w/h）；0 表示还没量到或量不出来。 */
const aspect = ref(0)

const document = computed(() => (props.svg ? svgDocument(props.svg) : ''))

/**
 * 容器高度按宽高比算，并夹在合理区间内（规则见 frameHeightFor）。
 */
const containerStyle = computed(() => ({
  height: `${frameHeightFor(aspect.value, props.fallbackHeight, props.maxAspect)}px`,
}))

/**
 * 从测量帧里读 SVG 尺寸，再据此算出可见帧的高度。
 *
 * 读不到同源文档（浏览器拒绝 allow-same-origin，或帧还没加载完）时
 * 保持 0，让高度回落到 fallbackHeight。不抛错、不重试 ——
 * 高度不准是可以接受的退化，为此打断渲染不值得。
 */
function measure(): void {
  aspect.value = aspectFromDocument(measureFrame.value?.contentDocument)
}

/**
 * svg 变化时重置比例：把旧比例套到新图上比不量更糟
 * （一张方图会按上一张长图的高度渲染，看起来像被压扁了）。
 */
watch(
  () => props.svg,
  () => {
    aspect.value = 0
  },
)

defineExpose({ measure, aspect })
</script>

<style scoped>
.iq-svg-frame {
  position: relative;
  width: 100%;
}

.iq-svg-visible {
  display: block;
  width: 100%;
  height: 100%;
  border: 0;
  background: transparent;
}

/*
  测量帧：留在文档里但完全不可见、不占位、不可聚焦。
  用绝对定位 + 近零尺寸而不是 display:none —— display:none 的 iframe
  在部分浏览器里不加载内容，contentDocument 会是空的，量不到东西。
*/
.iq-svg-measure {
  position: absolute;
  left: 0;
  top: 0;
  width: 1px;
  height: 1px;
  border: 0;
  opacity: 0;
  pointer-events: none;
  overflow: hidden;
}
</style>
