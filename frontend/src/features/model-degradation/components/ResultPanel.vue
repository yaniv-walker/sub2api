<script setup lang="ts">
/**
 * 结果面板（右栏）。
 *
 * 展示「当前这一次检测」的完整结果：状态 → 判定 → 指标 → 逐题明细。
 *
 * 关键取舍：运行中不做任何「进度百分比」的猜测。后端是串行跑题的，
 * 我们只知道「跑了几题」，不知道每题要多久 —— 编一个 60% 只会让人
 * 觉得卡住了。因此运行中只显示已完成的题数与已用时。
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ArtworkFrame from './ArtworkFrame.vue'
import MascotBadge from './MascotBadge.vue'
import type { MDItem, MDRunDetail } from '../types'
import {
  errorKindKey,
  formatDuration,
  formatTimeoutMinutes,
  itemVerdictKey,
  protocolShort,
  renderVerdictKey,
  ribbonKey,
  verdictKey
} from '../labels'

const props = defineProps<{
  run: MDRunDetail | null
  loading: boolean
  /** 已用时（运行中时由父组件每秒刷新）。 */
  elapsedMs: number
}>()

const emit = defineEmits<{
  (e: 'cancel'): void
}>()

const { t } = useI18n()

/** 展开查看中的作品。 */
const zoomItem = ref<MDItem | null>(null)
/** 复制的反馈（哪一条已被复制）。 */
const copiedUid = ref<string>('')

/** 是否运行中。 */
const running = computed(() => props.run?.status === 'running')

/** 是否已中止。 */
const cancelled = computed(() => props.run?.status === 'cancelled')

/** 逻辑题子集。 */
const logicItems = computed(() => (props.run?.items ?? []).filter((i) => i.task_type === 'logic'))

/** 生成题子集。 */
const renderItems = computed(() => (props.run?.items ?? []).filter((i) => i.task_type === 'render'))

/** 蒙皮的动物形态：通过→猴（更得意），否则狗。纯装饰。 */
const mascotAnimal = computed(() => (props.run?.verdict === 'normal' ? 'monkey' : 'dog'))

/** 蒙皮的情绪。 */
const mascotState = computed<'thinking' | 'pass' | 'fail'>(() => {
  if (!props.run) return 'thinking'
  if (running.value) return 'thinking'
  if (props.run.verdict === 'normal') return 'pass'
  if (props.run.verdict === 'degraded') return 'fail'
  return 'thinking'
})

/** 逐题的稳定 key。 */
function itemKey(item: MDItem, index: number): string {
  return `${item.task_type}-${item.task_key || index}-${index}`
}

/** 判定说明是否为空（空则不渲染那一行）。 */
function hasDetail(item: MDItem): boolean {
  return (item.detail ?? '').trim() !== ''
}

/** 逐题的判定修饰类。 */
function itemClass(item: MDItem): string {
  return `is-${item.verdict}`
}

/** 复制作品源码。 */
async function copyArtwork(item: MDItem) {
  const svg = item.artwork ?? ''
  if (!svg) return
  try {
    await navigator.clipboard.writeText(svg)
    copiedUid.value = item.task_key
    window.setTimeout(() => {
      if (copiedUid.value === item.task_key) copiedUid.value = ''
    }, 1800)
  } catch {
    // 剪贴板不可用（非安全上下文）时静默失败：
    // 还有「下载」这条路径，不必打扰用户。
  }
}

/** 下载作品为 .svg 文件。 */
function downloadArtwork(item: MDItem) {
  const svg = item.artwork ?? ''
  if (!svg) return
  const blob = new Blob([svg], { type: 'image/svg+xml;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${item.task_key || 'artwork'}.svg`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  // 立刻回收会让部分浏览器拿不到数据，延后一拍。
  window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}

/** 在新标签打开作品（用 blob URL，不污染当前文档）。 */
function openArtwork(item: MDItem) {
  const svg = item.artwork ?? ''
  if (!svg) return
  const blob = new Blob([svg], { type: 'image/svg+xml;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  window.open(url, '_blank', 'noopener,noreferrer')
  window.setTimeout(() => URL.revokeObjectURL(url), 60000)
}
</script>

<template>
  <section class="ma-result-col">
    <!-- 空态 -->
    <div v-if="!run && !loading" class="ma-result-empty">
      <MascotBadge animal="dog" state="thinking" />
      <p class="ma-result-empty-text">{{ t('modelDegradation.resultPanel.waiting') }}</p>
    </div>

    <!-- 加载中 -->
    <div v-else-if="loading && !run" class="ma-result-empty">
      <span class="ma-spinner-lg" aria-hidden="true" />
      <p class="ma-result-empty-text">{{ t('modelDegradation.resultPanel.waiting') }}</p>
    </div>

    <template v-else-if="run">
      <!-- 顶部：编号 + 状态 + 中止 -->
      <header class="ma-mine-head">
        <div class="ma-mine-id">
          <span class="ma-h3">#{{ run.id }}</span>
          <span class="ma-mine-model">{{ run.model_id }}</span>
        </div>
        <button v-if="running" type="button" class="ma-stop" @click="emit('cancel')">
          {{ t('modelDegradation.resultPanel.stop') }}
        </button>
      </header>

      <!-- 运行中 -->
      <div v-if="running" class="ma-running">
        <MascotBadge :animal="mascotAnimal" state="thinking" />
        <div class="ma-running-body">
          <p class="ma-running-title">
            {{ run.retrying ? t('modelDegradation.resultPanel.retrying') : t('modelDegradation.resultPanel.running') }}
          </p>
          <p class="ma-running-sub">
            {{ t('modelDegradation.resultPanel.seconds', { n: Math.max(1, Math.round(elapsedMs / 1000)) }) }}
            · {{ run.items.length }} / {{ run.logic_total + renderItems.length }}
          </p>
        </div>
      </div>

      <!-- 已中止 -->
      <div v-else-if="cancelled" class="ma-cancelled">
        <span class="ma-cancelled-title">{{ t('modelDegradation.resultPanel.cancelled') }}</span>
        <p class="ma-cancelled-hint">{{ t('modelDegradation.resultPanel.cancelledHint') }}</p>
      </div>

      <!-- 判定摘要 -->
      <div v-else class="ma-mine-card">
        <MascotBadge :animal="mascotAnimal" :state="mascotState" />
        <div class="ma-mine-card-body">
          <span class="ma-logic-verdict" :class="`is-${run.verdict}`">
            {{ t(verdictKey(run.verdict)) }}
          </span>
          <span v-if="run.logic_total > 0" class="ma-logic-score">
            {{ run.logic_passed }} / {{ run.logic_total }}
          </span>
          <span v-else class="ma-logic-score">{{ t(ribbonKey(run)) }}</span>
        </div>
      </div>

      <!-- 判定说明 -->
      <p v-if="!running && !cancelled" class="ma-verdict-hint">
        {{
          run.logic_total === 0
            ? t('modelDegradation.verdictHint.renderOnly')
            : t(`modelDegradation.verdictHint.${run.verdict}`)
        }}
      </p>

      <!-- 上游错误 -->
      <div v-if="run.error_message && !running" class="ma-detail-error">
        <span class="ma-detail-error-kind">{{ t(errorKindKey(run.error_kind)) }}</span>
        <p class="ma-detail-error-msg">{{ run.error_message }}</p>
      </div>

      <!-- 指标 -->
      <dl class="ma-mini">
        <div class="ma-mini-item">
          <dt>{{ t('modelDegradation.metrics.protocol') }}</dt>
          <dd>{{ protocolShort(run.protocol) }}</dd>
        </div>
        <div class="ma-mini-item">
          <dt>{{ t('modelDegradation.metrics.duration') }}</dt>
          <dd>{{ formatDuration(run.duration_ms) }}</dd>
        </div>
        <div v-if="run.logic_total > 0" class="ma-mini-item">
          <dt>{{ t('modelDegradation.metrics.logic') }}</dt>
          <dd>{{ run.logic_passed }}/{{ run.logic_total }}</dd>
        </div>
        <div v-if="renderItems.length" class="ma-mini-item">
          <dt>{{ t('modelDegradation.metrics.render') }}</dt>
          <dd>{{ t(renderVerdictKey(run.render_verdict)) }}</dd>
        </div>
        <div v-if="renderItems.length" class="ma-mini-item">
          <dt>{{ t('modelDegradation.metrics.renderTimeout') }}</dt>
          <dd>{{ formatTimeoutMinutes(run.render_timeout_seconds) }} {{ t('modelDegradation.form.renderTimeoutUnit') }}</dd>
        </div>
      </dl>

      <!-- 逻辑题明细 -->
      <div v-if="logicItems.length" class="ma-section">
        <h3 class="ma-section-title">{{ t('modelDegradation.result.questionDetail') }}</h3>
        <div class="ma-qlist">
          <article
            v-for="(item, index) in logicItems"
            :key="itemKey(item, index)"
            class="ma-item"
            :class="itemClass(item)"
          >
            <div class="ma-item-head">
              <span class="ma-item-badge">{{ t(itemVerdictKey(item.verdict)) }}</span>
              <span class="ma-item-dur">{{ formatDuration(item.duration_ms) }}</span>
            </div>
            <p class="ma-item-row">
              <span class="ma-item-key">{{ t('modelDegradation.result.expected') }}</span>
              <span class="ma-item-val">{{ item.expected || '—' }}</span>
            </p>
            <p class="ma-item-row">
              <span class="ma-item-key">{{ t('modelDegradation.result.answer') }}</span>
              <span class="ma-item-val ma-item-answer">{{ item.response || '—' }}</span>
            </p>
            <p v-if="hasDetail(item)" class="ma-item-row">
              <span class="ma-item-key">{{ t('modelDegradation.result.reason') }}</span>
              <span class="ma-item-val ma-item-detail">{{ item.detail }}</span>
            </p>
          </article>
        </div>
      </div>

      <!-- 生成题明细 -->
      <div v-if="renderItems.length" class="ma-section">
        <h3 class="ma-section-title">{{ t('modelDegradation.metrics.render') }}</h3>
        <div class="ma-render-grid">
          <article
            v-for="(item, index) in renderItems"
            :key="itemKey(item, index)"
            class="ma-render-card"
            :class="itemClass(item)"
          >
            <div class="ma-render-head">
              <span class="ma-item-badge">{{ t(itemVerdictKey(item.verdict)) }}</span>
              <span class="ma-item-dur">{{ formatDuration(item.duration_ms) }}</span>
            </div>

            <ArtworkFrame
              v-if="item.artwork"
              :source="item.artwork"
              :aspect="1"
              @expand="zoomItem = item"
            />
            <p v-else class="ma-render-empty">{{ item.detail || '—' }}</p>

            <div v-if="item.artwork" class="ma-render-actions">
              <button type="button" class="ma-mini-btn" @click="copyArtwork(item)">
                {{
                  copiedUid === item.task_key
                    ? t('modelDegradation.result.artworkCopied')
                    : t('modelDegradation.result.copyArtwork')
                }}
              </button>
              <button type="button" class="ma-mini-btn" @click="downloadArtwork(item)">
                {{ t('modelDegradation.result.downloadArtwork') }}
              </button>
              <button type="button" class="ma-mini-btn" @click="openArtwork(item)">
                {{ t('modelDegradation.result.openArtwork') }}
              </button>
            </div>
          </article>
        </div>
      </div>
    </template>

    <!-- 作品放大 -->
    <Teleport to="body">
      <div
        v-if="zoomItem"
        class="ma-page ma-zoom"
        role="dialog"
        aria-modal="true"
        aria-label="artwork"
        @click.self="zoomItem = null"
      >
        <div class="ma-zoom-panel">
          <button type="button" class="ma-zoom-close" @click="zoomItem = null" aria-label="close">×</button>
          <ArtworkFrame :source="zoomItem.artwork ?? ''" zoom />
        </div>
      </div>
    </Teleport>
  </section>
</template>

<style scoped>
.ma-result-col {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 18px;
  border: 1px solid var(--ma-border);
  border-radius: 16px;
  background: var(--ma-panel);
  backdrop-filter: blur(10px);
}

.ma-result-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  min-height: 240px;
  color: var(--ma-faint);
}

.ma-result-empty-text {
  margin: 0;
  font-size: 12px;
}

.ma-spinner-lg {
  width: 22px;
  height: 22px;
  border: 2px solid var(--ma-border);
  border-top-color: var(--ma-primary);
  border-radius: 50%;
  animation: ma-spin 0.7s linear infinite;
}

@keyframes ma-spin {
  to {
    transform: rotate(360deg);
  }
}

/* 顶部 */
.ma-mine-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.ma-mine-id {
  display: flex;
  align-items: baseline;
  gap: 8px;
  min-width: 0;
}

.ma-h3 {
  font-size: 13px;
  font-weight: 700;
  color: var(--ma-control-fg);
  font-variant-numeric: tabular-nums;
}

.ma-mine-model {
  overflow: hidden;
  font-size: 12px;
  color: var(--ma-muted);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ma-stop {
  padding: 5px 12px;
  border: 1px solid var(--ma-fail);
  border-radius: 8px;
  background: transparent;
  color: var(--ma-fail);
  font-size: 12px;
  cursor: pointer;
}

.ma-stop:hover {
  background: color-mix(in srgb, var(--ma-fail) 12%, transparent);
}

/* 运行中 */
.ma-running {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  border-radius: 12px;
  background: var(--ma-panel-soft);
}

.ma-running-body {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.ma-running-title {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: var(--ma-control-fg);
}

.ma-running-sub {
  margin: 0;
  font-size: 11px;
  color: var(--ma-muted);
  font-variant-numeric: tabular-nums;
}

/* 已中止 */
.ma-cancelled {
  padding: 12px;
  border-radius: 12px;
  background: var(--ma-panel-soft);
}

.ma-cancelled-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--ma-faint);
}

.ma-cancelled-hint {
  margin: 4px 0 0;
  font-size: 11px;
  line-height: 1.5;
  color: var(--ma-muted);
}

/* 判定卡片 */
.ma-mine-card {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px;
  border-radius: 12px;
  background: var(--ma-panel-soft);
}

.ma-mine-card-body {
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-width: 0;
}

.ma-logic-verdict {
  font-size: 17px;
  font-weight: 700;
}

.ma-logic-verdict.is-normal {
  color: var(--ma-pass);
}

.ma-logic-verdict.is-degraded {
  color: var(--ma-fail);
}

.ma-logic-verdict.is-generated {
  color: #5a96c8;
}

.ma-logic-verdict.is-unknown {
  color: var(--ma-muted);
}

.ma-logic-score {
  font-size: 13px;
  color: var(--ma-muted);
  font-variant-numeric: tabular-nums;
}

.ma-verdict-hint {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--ma-muted);
}

/* 错误 */
.ma-detail-error {
  padding: 10px 12px;
  border-radius: 10px;
  background: color-mix(in srgb, var(--ma-fail) 10%, transparent);
}

.ma-detail-error-kind {
  font-size: 11px;
  font-weight: 600;
  color: var(--ma-fail);
}

.ma-detail-error-msg {
  margin: 3px 0 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--ma-control-fg);
}

/* 指标 */
.ma-mini {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
  gap: 8px;
  margin: 0;
}

.ma-mini-item {
  padding: 7px 9px;
  border-radius: 9px;
  background: var(--ma-panel-soft);
}

.ma-mini-item dt {
  font-size: 10px;
  color: var(--ma-faint);
}

.ma-mini-item dd {
  margin: 2px 0 0;
  font-size: 12px;
  font-weight: 600;
  color: var(--ma-control-fg);
  font-variant-numeric: tabular-nums;
}

/* 分段 */
.ma-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 8px;
  border-top: 1px solid var(--ma-line);
}

.ma-section-title {
  margin: 0;
  font-size: 12px;
  font-weight: 600;
  color: var(--ma-label);
}

.ma-qlist {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ma-item {
  display: flex;
  flex-direction: column;
  gap: 5px;
  padding: 10px;
  border: 1px solid var(--ma-border-soft);
  border-left-width: 3px;
  border-radius: 9px;
  background: var(--ma-panel-soft);
}

.ma-item.is-ok {
  border-left-color: var(--ma-pass);
}

.ma-item.is-degraded {
  border-left-color: var(--ma-fail);
}

.ma-item.is-unknown {
  border-left-color: var(--ma-muted);
}

.ma-item.is-generated {
  border-left-color: #5a96c8;
}

.ma-item-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.ma-item-badge {
  padding: 1px 7px;
  border-radius: 999px;
  background: var(--ma-seg-bg);
  color: var(--ma-label);
  font-size: 10px;
  font-weight: 600;
}

.ma-item-dur {
  font-size: 10px;
  color: var(--ma-faint);
  font-variant-numeric: tabular-nums;
}

.ma-item-row {
  display: flex;
  gap: 8px;
  margin: 0;
  font-size: 12px;
  line-height: 1.55;
}

.ma-item-key {
  flex: 0 0 52px;
  color: var(--ma-faint);
  font-size: 11px;
}

.ma-item-val {
  flex: 1;
  min-width: 0;
  color: var(--ma-control-fg);
  word-break: break-word;
}

.ma-item-answer {
  max-height: 120px;
  overflow-y: auto;
  white-space: pre-wrap;
}

.ma-item-detail {
  color: var(--ma-muted);
}

/* 生成题网格 */
.ma-render-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 10px;
}

.ma-render-card {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 9px;
  border: 1px solid var(--ma-border-soft);
  border-left-width: 3px;
  border-radius: 10px;
  background: var(--ma-panel-soft);
}

.ma-render-card.is-ok {
  border-left-color: var(--ma-pass);
}

.ma-render-card.is-degraded {
  border-left-color: var(--ma-fail);
}

.ma-render-card.is-unknown {
  border-left-color: var(--ma-muted);
}

.ma-render-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.ma-render-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  aspect-ratio: 1;
  margin: 0;
  padding: 8px;
  border-radius: 8px;
  background: var(--ma-input-bg);
  color: var(--ma-faint);
  font-size: 11px;
  text-align: center;
}

.ma-render-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.ma-mini-btn {
  padding: 3px 8px;
  border: 1px solid var(--ma-input-border);
  border-radius: 6px;
  background: var(--ma-input-bg);
  color: var(--ma-control-fg);
  font-size: 11px;
  cursor: pointer;
}

.ma-mini-btn:hover {
  border-color: var(--ma-primary);
  color: var(--ma-primary);
}

/* 放大层 */
.ma-zoom {
  position: fixed;
  inset: 0;
  z-index: 1000;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 16px;
  background: rgba(0, 0, 0, 0.62);
  backdrop-filter: blur(4px);
}

.ma-zoom-panel {
  position: relative;
  width: min(1100px, 100%);
  max-height: calc(100dvh - 32px);
  padding: 12px;
  overflow: auto;
  border-radius: 14px;
  background: var(--ma-panel-solid);
}

.ma-zoom-close {
  position: absolute;
  top: 8px;
  right: 8px;
  z-index: 2;
  width: 28px;
  height: 28px;
  padding: 0;
  border: 0;
  border-radius: 7px;
  background: var(--ma-seg-bg);
  color: var(--ma-control-fg);
  font-size: 18px;
  line-height: 1;
  cursor: pointer;
}
</style>
