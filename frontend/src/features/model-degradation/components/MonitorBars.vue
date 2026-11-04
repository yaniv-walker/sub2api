<script setup lang="ts">
/**
 * 顶部监控矩阵。
 *
 * 参考实现是一排等宽竖条，每条代表一次检测，高度编码通过率，
 * 颜色编码判定。它同时承担两个作用：
 *   1. 一眼看出「最近整体怎么样」
 *   2. 点击某条直接跳到那次检测
 *
 * 数据来自最近的 run 列表（按时间正序渲染，最早的靠左）。
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MDRun } from '../types'
import { formatDuration, ribbonKey } from '../labels'

const props = defineProps<{
  runs: MDRun[]
  /** 当前选中的 run id，用于高亮。 */
  selectedId?: number | null
}>()

const emit = defineEmits<{
  (e: 'select', id: number): void
}>()

const { t } = useI18n()

/** 固定槽位数：矩阵的宽度必须稳定，不能因为数据少就缩成一团。 */
const SLOTS = 24

interface Bar {
  key: string
  /** 0..1，决定柱高。 */
  ratio: number
  /** 修饰类后缀。 */
  tone: string
  active: boolean
  run: MDRun | null
  title: string
}

/**
 * 把 run 列表映射成固定长度的柱阵。
 *
 * 不足的部分用空槽补齐（tone='empty'），这样「历史上几乎没有数据」
 * 这件事本身也是可见的，而不是让人以为系统没在跑。
 */
const bars = computed<Bar[]>(() => {
  const recent = [...props.runs].slice(0, SLOTS).reverse()
  const out: Bar[] = []

  for (const run of recent) {
    out.push({
      key: `run-${run.id}`,
      ratio: barRatio(run),
      tone: barTone(run),
      active: props.selectedId === run.id,
      run,
      title: `${run.model_id} · ${formatDuration(run.duration_ms)} · ${t(ribbonKey(run))}`
    })
  }

  // 补齐空槽到 SLOTS。
  const padding = SLOTS - out.length
  for (let i = 0; i < padding; i += 1) {
    out.push({
      key: `empty-${i}`,
      ratio: 0.12,
      tone: 'empty',
      active: false,
      run: null,
      title: t('modelDegradation.monitor.empty')
    })
  }
  return out
})

/**
 * 柱高编码：逻辑题通过率。
 *
 * 没有逻辑题（纯生成模式）时给一个中位高度，而不是 0 ——
 * 0 会被读成「全错了」，那是错误信息。
 */
function barRatio(run: MDRun): number {
  if (run.logic_total > 0) {
    return Math.max(0.12, run.logic_passed / run.logic_total)
  }
  return 0.5
}

/** 颜色编码判定。 */
function barTone(run: MDRun): string {
  if (run.status === 'running') return 'running'
  if (run.status === 'cancelled') return 'cancelled'
  if (run.verdict === 'unknown' && run.error_message) return 'failed'
  return run.verdict
}

/** 通过率统计，用于图注。 */
const summary = computed(() => {
  const completed = props.runs.filter((r) => r.status === 'completed')
  if (completed.length === 0) return { rate: '—', count: 0 }
  const normal = completed.filter((r) => r.verdict === 'normal').length
  return {
    rate: `${Math.round((normal / completed.length) * 100)}%`,
    count: completed.length
  }
})

function onBarClick(bar: Bar) {
  if (bar.run) emit('select', bar.run.id)
}
</script>

<template>
  <section class="ma-monitor">
    <div class="ma-monitor-head">
      <h2 class="ma-monitor-title">{{ t('modelDegradation.monitor.title') }}</h2>
      <span class="ma-monitor-cap">
        {{ t('modelDegradation.monitor.capacity', { rate: summary.rate, n: summary.count }) }}
      </span>
    </div>

    <div class="ma-bars" role="list">
      <button
        v-for="bar in bars"
        :key="bar.key"
        type="button"
        class="ma-bar"
        :class="[`is-${bar.tone}`, { 'is-active': bar.active }]"
        :style="{ '--ma-bar-h': `${Math.round(bar.ratio * 100)}%` }"
        :title="bar.title"
        :disabled="!bar.run"
        role="listitem"
        @click="onBarClick(bar)"
      >
        <span class="ma-bar-fill" />
      </button>
    </div>

    <p class="ma-caption">{{ t('modelDegradation.monitor.caption') }}</p>

    <ul class="ma-legend">
      <li class="ma-legend-item is-pass">{{ t('modelDegradation.monitor.legend.passed') }}</li>
      <li class="ma-legend-item is-fail">{{ t('modelDegradation.monitor.legend.incorrect') }}</li>
      <li class="ma-legend-item is-error">{{ t('modelDegradation.monitor.legend.error') }}</li>
    </ul>
  </section>
</template>

<style scoped>
.ma-monitor {
  position: relative;
  z-index: 1;
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 14px 18px;
  border: 1px solid var(--ma-border);
  border-radius: 14px;
  background: var(--ma-panel);
  backdrop-filter: blur(8px);
}

.ma-monitor-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.ma-monitor-title {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--ma-control-fg);
}

.ma-monitor-cap {
  font-size: 11px;
  color: var(--ma-faint);
  font-variant-numeric: tabular-nums;
}

.ma-bars {
  display: grid;
  grid-auto-flow: column;
  grid-auto-columns: 1fr;
  align-items: end;
  gap: 4px;
  height: var(--ma-cap-height);
}

.ma-bar {
  position: relative;
  height: 100%;
  padding: 0;
  border: 0;
  border-radius: 3px;
  background: var(--ma-bar-empty);
  cursor: pointer;
  overflow: hidden;
  transition: transform 0.16s ease;
}

.ma-bar:disabled {
  cursor: default;
}

.ma-bar:not(:disabled):hover {
  transform: translateY(-2px);
}

.ma-bar-fill {
  position: absolute;
  inset: auto 0 0 0;
  height: var(--ma-bar-h);
  border-radius: 3px 3px 0 0;
  transition: height 0.24s ease;
}

/* 颜色与判定一一对应。 */
.ma-bar.is-normal .ma-bar-fill {
  background: var(--ma-pass);
}
.ma-bar.is-degraded .ma-bar-fill {
  background: var(--ma-fail);
}
.ma-bar.is-unknown .ma-bar-fill,
.ma-bar.is-failed .ma-bar-fill {
  background: var(--ma-muted);
}
.ma-bar.is-generated .ma-bar-fill {
  background: #5a96c8;
}
.ma-bar.is-cancelled .ma-bar-fill {
  background: var(--ma-faint);
  opacity: 0.5;
}
.ma-bar.is-running .ma-bar-fill {
  background: var(--ma-primary);
  animation: ma-bar-pulse 1.6s ease-in-out infinite;
}
.ma-bar.is-empty .ma-bar-fill {
  background: transparent;
}

.ma-bar.is-active {
  outline: 2px solid var(--ma-primary);
  outline-offset: 1px;
}

@keyframes ma-bar-pulse {
  0%,
  100% {
    opacity: 0.45;
  }
  50% {
    opacity: 1;
  }
}

@media (prefers-reduced-motion: reduce) {
  .ma-bar.is-running .ma-bar-fill {
    animation: none;
    opacity: 0.8;
  }
}

.ma-caption {
  margin: 0;
  font-size: 11px;
  line-height: 1.5;
  color: var(--ma-muted);
}

.ma-legend {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.ma-legend-item {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  color: var(--ma-label);
}

/* 图例圆点用 currentColor，与对应柱子同色。 */
.ma-legend-item::before {
  content: '';
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: currentColor;
}

.ma-legend-item.is-pass {
  color: var(--ma-pass);
}
.ma-legend-item.is-fail {
  color: var(--ma-fail);
}
.ma-legend-item.is-error {
  color: var(--ma-muted);
}
</style>
