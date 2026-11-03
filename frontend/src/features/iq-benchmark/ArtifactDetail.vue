<template>
  <Teleport to="body">
    <div
      v-if="artifact"
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      @click.self="$emit('close')"
    >
      <div class="card flex max-h-[90vh] w-full max-w-4xl flex-col overflow-hidden">
        <header class="flex items-start justify-between gap-4 border-b border-gray-200 p-4 dark:border-dark-700">
          <div class="min-w-0">
            <h3 class="truncate text-base font-semibold text-gray-900 dark:text-gray-100">
              {{ artifact.model }}
            </h3>
            <p class="truncate text-xs text-gray-500 dark:text-gray-400">{{ artifact.case_name }}</p>
          </div>
          <button class="btn btn-ghost btn-sm" :title="t('iqBenchmark.detail.close')" @click="$emit('close')">
            <Icon name="x" size="sm" />
          </button>
        </header>

        <div class="flex-1 overflow-auto p-4">
          <!-- 产出区：与卡片保持同一套安全渲染策略 -->
          <div class="mb-4">
            <h4 class="mb-2 text-xs font-semibold uppercase tracking-wide text-gray-400">
              {{ t('iqBenchmark.detail.content') }}
            </h4>

            <p v-if="blocked" class="rounded-lg bg-amber-50 p-4 text-sm text-amber-700 dark:bg-amber-900/30 dark:text-amber-300">
              {{ t('iqBenchmark.detail.rawUnavailable') }}
            </p>

            <template v-else-if="resolvedKind === 'image_url' && artifact.image_url">
              <img
                :src="artifact.image_url"
                :alt="artifact.case_name"
                referrerpolicy="no-referrer"
                class="mx-auto max-h-[50vh] object-contain"
              />
            </template>

            <!--
              SVG 走与卡片相同的 sandbox="" iframe（见 SVGFrame.vue）：
              <img> 虽然也不执行脚本，但吃不到 SVG 自有样式，
              而且无法按内容高度自适应，长图会被裁掉。
            -->
            <template v-else-if="resolvedKind === 'svg' && svgContent">
              <SVGFrame
                :svg="svgContent"
                :title="artifact.case_name"
                :fallback-height="320"
                class="mx-auto max-h-[50vh]"
              />
            </template>

            <template v-else-if="resolvedKind === 'markdown' && safeHTML">
              <div class="iq-markdown text-sm leading-relaxed" v-html="safeHTML"></div>
            </template>

            <pre
              v-else-if="artifact.content"
              class="max-h-[50vh] overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-4 text-xs leading-relaxed dark:bg-dark-900"
            >{{ artifact.content }}</pre>

            <p v-else class="text-sm text-gray-400">{{ t('iqBenchmark.card.noPreview') }}</p>
          </div>

          <!-- 元信息 -->
          <h4 class="mb-2 text-xs font-semibold uppercase tracking-wide text-gray-400">
            {{ t('iqBenchmark.detail.metadata') }}
          </h4>
          <dl class="grid grid-cols-2 gap-2 text-xs sm:grid-cols-3">
            <div>
              <dt class="text-gray-400">{{ t('iqBenchmark.admin.artifacts.status') }}</dt>
              <dd class="font-medium">{{ t(`iqBenchmark.status.${artifact.status}`) }}</dd>
            </div>
            <div>
              <dt class="text-gray-400">{{ t('iqBenchmark.card.duration') }}</dt>
              <dd class="font-medium tabular-nums">{{ formatDuration(artifact.duration_ms) }}</dd>
            </div>
            <div>
              <dt class="text-gray-400">{{ t('iqBenchmark.card.createdAt') }}</dt>
              <dd class="font-medium">{{ formattedDate }}</dd>
            </div>
            <div v-if="artifact.score !== undefined">
              <dt class="text-gray-400">{{ t('iqBenchmark.card.score') }}</dt>
              <dd class="font-medium tabular-nums">{{ artifact.score }}</dd>
            </div>
            <div v-if="artifact.cost !== undefined">
              <dt class="text-gray-400">{{ t('iqBenchmark.card.cost') }}</dt>
              <dd class="font-medium tabular-nums">{{ formatCost(artifact.cost) }}</dd>
            </div>
            <div v-if="artifact.input_tokens !== undefined">
              <dt class="text-gray-400">{{ t('iqBenchmark.card.inputTokens') }}</dt>
              <dd class="font-medium tabular-nums">{{ formatTokens(artifact.input_tokens) }}</dd>
            </div>
            <div v-if="artifact.output_tokens !== undefined">
              <dt class="text-gray-400">{{ t('iqBenchmark.card.outputTokens') }}</dt>
              <dd class="font-medium tabular-nums">{{ formatTokens(artifact.output_tokens) }}</dd>
            </div>
          </dl>

          <p v-if="artifact.error_summary" class="mt-3 rounded-lg bg-red-50 p-3 text-xs text-red-600 dark:bg-red-900/30 dark:text-red-300">
            {{ artifact.error_summary }}
          </p>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { IQPublicArtifact } from './types'
import { formatCost, formatDuration, formatTokens } from './filters'
import { markdownToSafeHTML, resolveRenderKind, sanitizeSVG } from './sanitize'
import { sliceSVG } from './svgFrame'
import SVGFrame from './SVGFrame.vue'

const props = defineProps<{ artifact: IQPublicArtifact | null }>()

defineEmits<{ (e: 'close'): void }>()

const { t, locale } = useI18n()

const blocked = computed(
  () => Boolean(props.artifact?.is_unsafe) || props.artifact?.status === 'blocked',
)

const resolvedKind = computed(() => {
  const a = props.artifact
  if (!a) return 'text' as const
  return resolveRenderKind(a.render_kind, a.content, a.image_url)
})

const svgContent = computed(() =>
  blocked.value ? '' : sliceSVG(sanitizeSVG(props.artifact?.content)),
)
const safeHTML = computed(() => (blocked.value ? '' : markdownToSafeHTML(props.artifact?.content)))

const formattedDate = computed(() => {
  const raw = props.artifact?.created_at
  if (!raw) return ''
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-US')
})
</script>
