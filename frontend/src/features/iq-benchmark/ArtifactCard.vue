<template>
  <article
    class="card flex flex-col overflow-hidden transition hover:shadow-md"
    :class="selected ? 'ring-2 ring-primary-500' : ''"
  >
    <!-- 产出预览区：按渲染类型区分，图片/SVG 一律走 <img>，不内联注入 -->
    <div
      class="relative flex min-h-[12rem] items-center justify-center overflow-hidden bg-gray-50 dark:bg-dark-900"
      :class="previewClass"
    >
      <template v-if="blocked">
        <div class="flex flex-col items-center gap-2 p-6 text-center">
          <Icon name="shield" size="lg" class="text-amber-500" />
          <span class="text-xs text-amber-600 dark:text-amber-400">{{ t('iqBenchmark.card.unsafe') }}</span>
        </div>
      </template>

      <template v-else-if="resolvedKind === 'image_url' && artifact.image_url">
        <img
          :src="artifact.image_url"
          :alt="artifact.case_name"
          loading="lazy"
          referrerpolicy="no-referrer"
          class="max-h-72 w-full cursor-zoom-in object-contain"
          @click="$emit('preview', artifact)"
        />
      </template>

      <template v-else-if="resolvedKind === 'svg' && svgURI">
        <!-- <img> 而非内联 SVG：浏览器保证 <img> 里的 SVG 不执行脚本 -->
        <img
          :src="svgURI"
          :alt="artifact.case_name"
          class="max-h-72 w-full cursor-zoom-in object-contain p-2"
          @click="$emit('preview', artifact)"
        />
      </template>

      <template v-else-if="resolvedKind === 'markdown' && safeHTML">
        <div
          class="iq-markdown max-h-72 w-full overflow-auto p-4 text-xs leading-relaxed"
          v-html="safeHTML"
        ></div>
      </template>

      <template v-else-if="previewText">
        <pre class="max-h-72 w-full overflow-auto whitespace-pre-wrap break-words p-4 text-xs leading-relaxed">{{
          previewText
        }}</pre>
      </template>

      <template v-else>
        <span class="p-6 text-xs text-gray-400">{{ t('iqBenchmark.card.noPreview') }}</span>
      </template>

      <!-- 状态角标 -->
      <span
        class="absolute right-2 top-2 rounded-full px-2 py-0.5 text-[10px] font-semibold"
        :class="statusBadgeClass"
      >{{ t(`iqBenchmark.status.${artifact.status}`) }}</span>

      <!-- 截断提示 -->
      <span
        v-if="artifact.truncated"
        class="absolute left-2 top-2 rounded-full bg-amber-100 px-2 py-0.5 text-[10px] font-semibold text-amber-700 dark:bg-amber-900/40 dark:text-amber-300"
      >{{ t('iqBenchmark.card.contentTruncated') }}</span>
    </div>

    <!-- 元信息区 -->
    <div class="flex flex-1 flex-col gap-2 p-3">
      <div class="flex items-start justify-between gap-2">
        <div class="min-w-0">
          <p class="truncate text-sm font-semibold text-gray-900 dark:text-gray-100" :title="artifact.model">
            {{ artifact.model }}
          </p>
          <p class="truncate text-xs text-gray-500 dark:text-gray-400" :title="artifact.case_name">
            {{ artifact.case_name }}
          </p>
        </div>
        <span
          v-if="artifact.score !== undefined"
          class="shrink-0 rounded-md bg-primary-50 px-1.5 py-0.5 text-[11px] font-bold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
          :title="t('iqBenchmark.card.score')"
        >{{ artifact.score }}</span>
      </div>

      <dl class="flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-gray-400 dark:text-dark-400">
        <div class="flex gap-1">
          <dt>{{ t('iqBenchmark.card.duration') }}:</dt>
          <dd class="tabular-nums">{{ formatDuration(artifact.duration_ms) }}</dd>
        </div>
        <div v-if="artifact.cost !== undefined" class="flex gap-1">
          <dt>{{ t('iqBenchmark.card.cost') }}:</dt>
          <dd class="tabular-nums">{{ formatCost(artifact.cost) }}</dd>
        </div>
        <div v-if="artifact.input_tokens !== undefined" class="flex gap-1">
          <dt>{{ t('iqBenchmark.card.tokens') }}:</dt>
          <dd class="tabular-nums">
            {{ formatTokens(artifact.input_tokens) }} / {{ formatTokens(artifact.output_tokens) }}
          </dd>
        </div>
      </dl>

      <!-- 失败说明（已由后端脱敏） -->
      <p v-if="artifact.error_summary" class="text-[11px] text-red-500 dark:text-red-400">
        {{ artifact.error_summary }}
      </p>

      <div class="mt-auto flex items-center justify-between gap-2 pt-1">
        <span class="text-[10px] text-gray-400">{{ formattedDate }}</span>
        <div class="flex items-center gap-1">
          <button class="btn btn-ghost btn-sm py-1" @click="$emit('detail', artifact)">
            {{ t('iqBenchmark.card.viewDetail') }}
          </button>
          <!--
            只读模式下不渲染对比按钮，而不是渲染成禁用态：
            禁用按钮会让人以为「点一下也许能行」，实际是权限不允许。
          -->
          <button
            v-if="selectable"
            class="btn btn-sm py-1"
            :class="selected ? 'btn-secondary' : 'btn-primary'"
            @click="$emit('toggle-compare', artifact)"
          >
            {{ selected ? t('iqBenchmark.card.compared') : t('iqBenchmark.card.compare') }}
          </button>
        </div>
      </div>
    </div>
  </article>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { IQPublicArtifact } from './types'
import { formatCost, formatDuration, formatTokens } from './filters'
import {
  markdownToSafeHTML,
  resolveRenderKind,
  svgToDataURI,
  truncateText,
} from './sanitize'

const props = withDefaults(
  defineProps<{
    artifact: IQPublicArtifact
    /** 是否允许加入对比（只读模式下为 false）。 */
    selectable?: boolean
    /** 是否已被选中对比。 */
    selected?: boolean
  }>(),
  { selectable: true, selected: false },
)

defineEmits<{
  (e: 'detail', artifact: IQPublicArtifact): void
  (e: 'preview', artifact: IQPublicArtifact): void
  (e: 'toggle-compare', artifact: IQPublicArtifact): void
}>()

const { t, locale } = useI18n()

/** 被拦截的作品不展示任何正文。 */
const blocked = computed(() => props.artifact.is_unsafe || props.artifact.status === 'blocked')

/** 实际渲染形态（后端声明 + 内容形态兜底）。 */
const resolvedKind = computed(() =>
  resolveRenderKind(props.artifact.render_kind, props.artifact.content, props.artifact.image_url),
)

/** SVG → data URI（经 DOMPurify 清洗）。 */
const svgURI = computed(() => (blocked.value ? '' : svgToDataURI(props.artifact.content)))

/** Markdown → 安全 HTML。 */
const safeHTML = computed(() => (blocked.value ? '' : markdownToSafeHTML(props.artifact.content)))

/** 纯文本预览（截断防撑爆布局）。 */
const previewText = computed(() => (blocked.value ? '' : truncateText(props.artifact.content)))

/** 卡片高度按形态微调：图片给更高的展示区。 */
const previewClass = computed(() =>
  resolvedKind.value === 'image_url' || resolvedKind.value === 'svg' ? 'min-h-[16rem]' : '',
)

const statusBadgeClass = computed(() => {
  switch (props.artifact.status) {
    case 'success':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'blocked':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
    default:
      return 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
  }
})

/** 时间按当前语言展示；无效时间不显示 NaT。 */
const formattedDate = computed(() => {
  const raw = props.artifact.created_at
  if (!raw) return ''
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
})
</script>

<style scoped>
.iq-codeblock {
  @apply overflow-auto rounded-md bg-gray-900 p-2 text-[11px] text-gray-100;
}

.iq-markdown :deep(code) {
  @apply rounded bg-gray-100 px-1 py-0.5 text-[11px] dark:bg-dark-700;
}
</style>
