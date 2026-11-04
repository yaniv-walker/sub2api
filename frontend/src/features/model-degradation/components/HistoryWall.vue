<script setup lang="ts">
/**
 * 历史墙。
 *
 * 参考实现的形态是「按模型分组的横向时间轴」：每个模型一行，
 * 行内按时间从旧到新排卡片，卡片上是那次的产出（生成题就是作品
 * 缩略图，逻辑题就是一个状态字形）。悬停放大，点击看详情。
 *
 * 分组的键选了 model_id 而不是 group：模型名是用户真正关心的维度
 * （「这个模型到底稳不稳」），分组只是归属信息，放在卡片里显示。
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import ArtworkFrame from './ArtworkFrame.vue'
import type { MDRun } from '../types'
import { formatRunTime, ribbonClass, ribbonKey, verdictKey } from '../labels'

const props = defineProps<{
  runs: MDRun[]
  /** 当前浏览的页码，从 1 开始。 */
  page: number
  /** 每页条数。 */
  pageSize: number
  /** 总数（用于计算总页数）。 */
  total: number
  selectedId?: number | null
  /** 当前判定过滤。 */
  verdictFilter: string
  /** 当前题型过滤。 */
  typeFilter: string
}>()

const emit = defineEmits<{
  (e: 'select', id: number): void
  (e: 'update:verdict-filter', v: string): void
  (e: 'update:type-filter', v: string): void
  (e: 'page', page: number): void
}>()

const { t } = useI18n()

/** 判定过滤选项。 */
const verdictFilters = computed(() => [
  { value: '', label: t('modelDegradation.history.filter.all') },
  { value: 'normal', label: t('modelDegradation.verdict.normal') },
  { value: 'degraded', label: t('modelDegradation.verdict.degraded') },
  { value: 'unknown', label: t('modelDegradation.verdict.unknown') },
  { value: 'generated', label: t('modelDegradation.verdict.generated') }
])

/** 题型过滤选项。 */
const typeFilters = computed(() => [
  { value: '', label: t('modelDegradation.history.groupFilter.all') },
  { value: 'logic', label: t('modelDegradation.form.taskLogic') },
  { value: 'render', label: t('modelDegradation.form.taskRender') }
])

interface Group {
  modelId: string
  runs: MDRun[]
}

/**
 * 按 model_id 分组。
 *
 * 组内按时间正序（旧→新），这样时间轴左旧右新，符合阅读习惯；
 * 组间按「组内最新一条的时间」倒序，让最近活跃的模型排在前面。
 */
const groups = computed<Group[]>(() => {
  const map = new Map<string, MDRun[]>()
  for (const run of props.runs) {
    const key = run.model_id || '—'
    const list = map.get(key)
    if (list) list.push(run)
    else map.set(key, [run])
  }

  const out: Group[] = []
  for (const [modelId, list] of map) {
    const sorted = [...list].sort(
      (a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
    )
    out.push({ modelId, runs: sorted })
  }

  // 组间：最新一条的创建时间倒序。
  out.sort((a, b) => {
    const ta = new Date(a.runs[a.runs.length - 1].created_at).getTime()
    const tb = new Date(b.runs[b.runs.length - 1].created_at).getTime()
    return tb - ta
  })

  return out
})

/** 总页数。 */
const totalPages = computed(() => Math.max(1, Math.ceil(props.total / props.pageSize)))

/** 是否可以上一页/下一页。 */
const canPrev = computed(() => props.page > 1)
const canNext = computed(() => props.page < totalPages.value)

/** 卡片上的主视觉：优先展示生成作品。 */
function primaryArtwork(run: MDRun): string {
  return run.artwork_preview ?? ''
}

/** 卡片字形（没有作品时）。 */
function glyphOf(run: MDRun): string {
  if (run.status === 'running') return '…'
  if (run.status === 'cancelled') return '–'
  if (run.verdict === 'normal') return '✓'
  if (run.verdict === 'degraded') return '×'
  if (run.verdict === 'generated') return '◈'
  return '?'
}

/** 时间轴节点时间。 */
function nodeTime(run: MDRun): string {
  return formatRunTime(run.created_at, t)
}

/** 卡片 title 提示。 */
function cardTitle(run: MDRun): string {
  return `${run.model_id} · ${t(verdictKey(run.verdict))}`
}
</script>

<template>
  <section class="ma-history">
    <header class="ma-history-head">
      <h2 class="ma-h2">{{ t('modelDegradation.history.title') }}</h2>
      <span class="ma-section-sub">{{ total }}</span>
    </header>

    <!-- 过滤 -->
    <div class="ma-gallery-filter">
      <div class="ma-filter-group">
        <span class="ma-filter-label">{{ t('modelDegradation.history.filterLabel') }}</span>
        <button
          v-for="f in verdictFilters"
          :key="f.value"
          type="button"
          class="ma-filter-btn"
          :class="{ 'is-active': verdictFilter === f.value }"
          @click="emit('update:verdict-filter', f.value)"
        >
          {{ f.label }}
        </button>
      </div>
      <div class="ma-filter-group">
        <span class="ma-filter-label">{{ t('modelDegradation.history.typeFilterLabel') }}</span>
        <button
          v-for="f in typeFilters"
          :key="f.value"
          type="button"
          class="ma-filter-btn"
          :class="{ 'is-active': typeFilter === f.value }"
          @click="emit('update:type-filter', f.value)"
        >
          {{ f.label }}
        </button>
      </div>
    </div>

    <!-- 空态 -->
    <div v-if="!groups.length" class="ma-empty">
      <svg viewBox="0 0 48 48" width="34" height="34" aria-hidden="true">
        <rect x="6" y="10" width="36" height="28" rx="4" fill="none" stroke="currentColor" stroke-width="1.6" />
        <path d="M6 30l10-9 7 6 7-7 12 10" fill="none" stroke="currentColor" stroke-width="1.6" />
      </svg>
      <p>{{ t('modelDegradation.history.empty') }}</p>
    </div>

    <!-- 分组 -->
    <div v-else class="ma-groups">
      <div v-for="group in groups" :key="group.modelId" class="ma-group-row">
        <div class="ma-group-head">
          <span class="ma-group-name">{{ group.modelId }}</span>
          <span class="ma-group-count">{{ group.runs.length }}</span>
        </div>

        <!-- 横向时间轴 -->
        <div class="ma-group-nav">
          <div class="ma-group-rule" aria-hidden="true" />
          <button
            v-for="run in group.runs"
            :key="run.id"
            type="button"
            class="ma-card"
            :class="[ribbonClass(run), { 'is-selected': selectedId === run.id }]"
            :title="cardTitle(run)"
            @click="emit('select', run.id)"
          >
            <!-- 卡片主视觉 -->
            <span class="ma-card-cover">
              <ArtworkFrame v-if="primaryArtwork(run)" :source="primaryArtwork(run)" :aspect="1" />
              <span v-else class="ma-card-glyph" :class="`is-${run.verdict}`">{{ glyphOf(run) }}</span>
            </span>

            <!-- 卡片底栏 -->
            <span class="ma-card-foot">
              <span class="ma-card-ribbon">{{ t(ribbonKey(run)) }}</span>
              <span class="ma-card-time">{{ nodeTime(run) }}</span>
            </span>

            <!-- 悬停信息 -->
            <span class="ma-hover-card">
              <span class="ma-hover-title">{{ run.model_id }}</span>
              <span class="ma-hover-row">
                <span class="ma-hover-key">{{ t('modelDegradation.metrics.logic') }}</span>
                <span class="ma-hover-val">{{ run.logic_passed }}/{{ run.logic_total }}</span>
              </span>
              <span class="ma-hover-row">
                <span class="ma-hover-key">{{ t('modelDegradation.history.owner') }}</span>
                <span class="ma-hover-val">
                  {{ run.is_owner ? t('modelDegradation.history.ownerSelf') : run.owner_email || t('modelDegradation.history.ownerUnknown') }}
                </span>
              </span>
              <span class="ma-hover-row">
                <span class="ma-hover-key">{{ t('modelDegradation.history.groupLabel') }}</span>
                <span class="ma-hover-val">{{ run.group_name || t('modelDegradation.history.groupUnknown') }}</span>
              </span>
            </span>
          </button>
        </div>
      </div>
    </div>

    <!-- 分页 -->
    <div v-if="total > pageSize" class="ma-pager">
      <button type="button" class="ma-pager-btn" :disabled="!canPrev" @click="emit('page', page - 1)">
        {{ t('modelDegradation.history.prevPage') }}
      </button>
      <span class="ma-pager-status">
        {{ t('modelDegradation.history.pageStatus', { page, total: totalPages }) }}
      </span>
      <button type="button" class="ma-pager-btn" :disabled="!canNext" @click="emit('page', page + 1)">
        {{ t('modelDegradation.history.nextPage') }}
      </button>
    </div>
  </section>
</template>

<style scoped>
.ma-history {
  position: relative;
  z-index: 1;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 18px;
  border: 1px solid var(--ma-border);
  border-radius: 16px;
  background: var(--ma-panel);
  backdrop-filter: blur(10px);
}

.ma-history-head {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.ma-h2 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--ma-control-fg);
}

.ma-section-sub {
  font-size: 11px;
  color: var(--ma-faint);
  font-variant-numeric: tabular-nums;
}

/* 过滤 */
.ma-gallery-filter {
  display: flex;
  flex-wrap: wrap;
  gap: 14px;
}

.ma-filter-group {
  display: flex;
  align-items: center;
  gap: 5px;
}

.ma-filter-label {
  font-size: 11px;
  color: var(--ma-faint);
}

.ma-filter-btn {
  padding: 3px 9px;
  border: 1px solid var(--ma-input-border);
  border-radius: 999px;
  background: transparent;
  color: var(--ma-label);
  font-size: 11px;
  cursor: pointer;
  transition: all 0.14s ease;
}

.ma-filter-btn:hover {
  border-color: var(--ma-primary);
}

.ma-filter-btn.is-active {
  border-color: var(--ma-seg-on-border);
  background: var(--ma-seg-on-bg);
  color: var(--ma-control-fg);
  font-weight: 600;
}

/* 空态 */
.ma-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 140px;
  color: var(--ma-faint);
}

.ma-empty p {
  margin: 0;
  font-size: 12px;
}

/* 分组 */
.ma-groups {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.ma-group-row {
  display: flex;
  flex-direction: column;
  gap: 7px;
}

.ma-group-head {
  display: flex;
  align-items: center;
  gap: 7px;
}

.ma-group-name {
  font-size: 12px;
  font-weight: 600;
  color: var(--ma-control-fg);
}

.ma-group-count {
  padding: 0 6px;
  border-radius: 9px;
  background: var(--ma-seg-bg);
  color: var(--ma-muted);
  font-size: 10px;
  font-variant-numeric: tabular-nums;
}

/* 时间轴：横向滚动，卡片左旧右新 */
.ma-group-nav {
  position: relative;
  display: flex;
  gap: 8px;
  padding-bottom: 4px;
  overflow-x: auto;
  scrollbar-width: thin;
}

.ma-group-rule {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 8px;
  height: 1px;
  background: var(--ma-line);
  pointer-events: none;
}

/* 卡片 */
.ma-card {
  position: relative;
  flex: 0 0 auto;
  width: 84px;
  padding: 5px;
  border: 1px solid var(--ma-border-soft);
  border-radius: 10px;
  background: var(--ma-panel-soft);
  cursor: pointer;
  transition:
    transform 0.16s ease,
    border-color 0.16s ease;
}

.ma-card:hover {
  transform: translateY(-3px);
  border-color: var(--ma-primary);
  z-index: 5;
}

.ma-card.is-selected {
  border-color: var(--ma-primary);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--ma-primary) 22%, transparent);
}

/* 主视觉 */
.ma-card-cover {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  aspect-ratio: 1;
  overflow: hidden;
  border-radius: 7px;
  background: var(--ma-input-bg);
}

.ma-card-glyph {
  font-size: 22px;
  font-weight: 700;
  line-height: 1;
}

.ma-card-glyph.is-normal {
  color: var(--ma-pass);
}

.ma-card-glyph.is-degraded {
  color: var(--ma-fail);
}

.ma-card-glyph.is-generated {
  color: #5a96c8;
}

.ma-card-glyph.is-unknown {
  color: var(--ma-muted);
}

/* 底栏 */
.ma-card-foot {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 4px;
  padding: 4px 2px 0;
}

.ma-card-ribbon {
  overflow: hidden;
  color: var(--ma-muted);
  font-size: 9px;
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ma-card-time {
  flex: 0 0 auto;
  color: var(--ma-faint);
  font-size: 9px;
  font-variant-numeric: tabular-nums;
}

/* 缎带颜色：左上角小三角 */
.ma-card::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  width: 0;
  height: 0;
  border-top: 14px solid var(--ma-muted);
  border-right: 14px solid transparent;
  border-top-left-radius: 9px;
  opacity: 0.9;
}

.ma-card.is-normal::before {
  border-top-color: var(--ma-pass);
}

.ma-card.is-degraded::before,
.ma-card.is-failed::before {
  border-top-color: var(--ma-fail);
}

.ma-card.is-generated::before {
  border-top-color: #5a96c8;
}

.ma-card.is-running::before {
  border-top-color: var(--ma-primary);
}

.ma-card.is-cancelled::before {
  border-top-color: var(--ma-faint);
}

/* 悬停浮层 */
.ma-hover-card {
  position: absolute;
  z-index: 10;
  top: calc(100% + 6px);
  left: 0;
  display: none;
  flex-direction: column;
  gap: 3px;
  width: 170px;
  padding: 8px 10px;
  border: 1px solid var(--ma-border);
  border-radius: 9px;
  background: var(--ma-panel-solid);
  box-shadow: 0 8px 22px rgba(0, 0, 0, 0.14);
  text-align: left;
}

.ma-card:hover .ma-hover-card {
  display: flex;
}

.ma-hover-title {
  overflow: hidden;
  font-size: 11px;
  font-weight: 600;
  color: var(--ma-control-fg);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ma-hover-row {
  display: flex;
  justify-content: space-between;
  gap: 6px;
  font-size: 10px;
}

.ma-hover-key {
  color: var(--ma-faint);
}

.ma-hover-val {
  overflow: hidden;
  color: var(--ma-control-fg);
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 分页 */
.ma-pager {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding-top: 4px;
}

.ma-pager-btn {
  padding: 5px 12px;
  border: 1px solid var(--ma-input-border);
  border-radius: 8px;
  background: var(--ma-input-bg);
  color: var(--ma-control-fg);
  font-size: 11px;
  cursor: pointer;
}

.ma-pager-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.ma-pager-status {
  font-size: 11px;
  color: var(--ma-muted);
  font-variant-numeric: tabular-nums;
}

@media (prefers-reduced-motion: reduce) {
  .ma-card {
    transition: none;
  }
}
</style>
