<script setup lang="ts">
/**
 * 模型降智检测。
 *
 * 页面结构对齐参考站点：
 *   顶部（标题 + 说明 + 统计）
 *   监控矩阵（最近若干次的条形总览）
 *   左栏发起表单 / 右栏结果面板
 *   下方按模型分组的历史墙
 *
 * 与参考实现的一处偏差：这里没有「分享」按钮。分享需要一个公开的
 * 只读链接与它的过期策略，那是另一件事；先不做，而不是做一个
 * 半成品的分享入口。
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import HistoryWall from '@/features/model-degradation/components/HistoryWall.vue'
import MonitorBars from '@/features/model-degradation/components/MonitorBars.vue'
import ResultPanel from '@/features/model-degradation/components/ResultPanel.vue'
import RunForm from '@/features/model-degradation/components/RunForm.vue'
import WaveBackdrop from '@/features/model-degradation/components/WaveBackdrop.vue'
import { PAGE_SIZE, useDegradationFeed } from '@/features/model-degradation/useDegradationFeed'
import { useRunForm } from '@/features/model-degradation/useRunForm'
import { formatDuration, formatPassRate } from '@/features/model-degradation/labels'
import type { MDProtocol, MDTask } from '@/features/model-degradation/types'
import '@/features/model-degradation/theme.css'

const { t } = useI18n()

const feed = useDegradationFeed()
const form = useRunForm()

/** 是否已初次加载。 */
const ready = ref(false)

onMounted(async () => {
  await feed.bootstrap()
  ready.value = true
})

/** 提交：先本地校验，通过后交给 feed。 */
async function onSubmit() {
  const code = form.validate()
  form.errorCode.value = code
  if (code) return

  form.submitting.value = true
  const ok = await feed.submit(form.toPayload())
  form.submitting.value = false

  // 只有成功才清空题目与密钥；失败时保留输入，让用户改一处重试，
  // 而不是把整张表单连同他贴的密钥一起清掉。
  if (ok) {
    form.state.apiKey = ''
    form.state.questions = []
  }
}

/** 题库题加入。 */
function onAddBank(task: MDTask) {
  form.addFromBank(task)
}

/** 协议切换。 */
function onProtocolChange(p: MDProtocol) {
  form.setProtocol(p)
}

/** 顶部统计卡片。 */
const statCards = computed(() => [
  {
    label: t('modelDegradation.stats.passRate'),
    value: formatPassRate(feed.stats.value.pass_rate)
  },
  {
    label: t('modelDegradation.stats.lastDuration'),
    value: formatDuration(feed.stats.value.last_duration_ms)
  },
  {
    label: t('modelDegradation.stats.runCount'),
    value: String(feed.stats.value.run_count)
  }
])

/** 监控矩阵只展示最近一页的数据。 */
const monitorRuns = computed(() => feed.runs.value)
</script>

<template>
  <AppLayout>
    <div class="ma-page">
      <WaveBackdrop />

      <div class="ma-page-inner">
        <!-- 顶部 -->
        <header class="ma-top">
          <div class="ma-top-head">
            <div class="ma-top-copy">
              <div class="ma-top-title-line">
                <h1 class="ma-top-title">{{ t('modelDegradation.title') }}</h1>
              </div>
              <p class="ma-top-sub">{{ t('modelDegradation.description') }}</p>
              <div class="ma-top-chips">
                <span class="ma-chip">{{ t('modelDegradation.subtitle') }}</span>
              </div>
            </div>

            <div class="ma-stats">
              <div v-for="card in statCards" :key="card.label" class="ma-stat">
                <span class="ma-stat-label">{{ card.label }}</span>
                <span class="ma-stat-value">{{ card.value }}</span>
              </div>
            </div>
          </div>
        </header>

        <!-- 监控矩阵 -->
        <MonitorBars :runs="monitorRuns" :selected-id="feed.current.value?.id ?? null" @select="feed.select" />

        <!-- 左表单 / 右结果 -->
        <div class="ma-panel">
          <RunForm
            :state="form.state"
            :bank="feed.bank.value"
            :bank-loading="feed.bankLoading.value"
            :bank-failed="feed.bankFailed.value"
            :error-code="form.errorCode.value"
            :submitting="form.submitting.value"
            @submit="onSubmit"
            @change-protocol="onProtocolChange"
            @add-bank="onAddBank"
            @remove="form.remove"
          />

          <ResultPanel
            :run="feed.current.value"
            :loading="feed.detailLoading.value"
            :elapsed-ms="feed.elapsedMs.value"
            @cancel="feed.cancel"
          />
        </div>

        <!-- 提交错误 -->
        <p v-if="feed.detailError.value" class="ma-banner is-error">
          {{ t(`modelDegradation.errors.${feed.detailError.value}`) }}
        </p>

        <!-- 轮询到上限 -->
        <p v-if="feed.pollExhausted.value" class="ma-banner is-warn">
          {{ t('modelDegradation.resultPanel.pollExhausted') }}
        </p>

        <!-- 历史墙 -->
        <p v-if="feed.listError.value" class="ma-banner is-error">
          {{ t('modelDegradation.errors.loadFailed') }}
        </p>
        <HistoryWall
          :runs="feed.runs.value"
          :page="feed.page.value"
          :page-size="PAGE_SIZE"
          :total="feed.total.value"
          :selected-id="feed.current.value?.id ?? null"
          :verdict-filter="feed.verdictFilter.value"
          :type-filter="feed.typeFilter.value"
          @select="feed.select"
          @page="feed.goPage"
          @update:verdict-filter="feed.setVerdictFilter"
          @update:type-filter="feed.setTypeFilter"
        />
      </div>
    </div>
  </AppLayout>
</template>

<style scoped>
.ma-banner {
  margin: 0;
  padding: 9px 12px;
  border-radius: 10px;
  font-size: 12px;
  line-height: 1.5;
}

.ma-banner.is-error {
  background: color-mix(in srgb, var(--ma-fail) 12%, transparent);
  color: var(--ma-fail);
}

.ma-banner.is-warn {
  background: color-mix(in srgb, #e4bc50 18%, transparent);
  color: #947112;
}
</style>
