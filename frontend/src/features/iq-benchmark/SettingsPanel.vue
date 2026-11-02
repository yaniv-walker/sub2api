<template>
  <div class="flex flex-col gap-4">
    <!-- 功能开关单独放在最上方：它决定其它项是否生效 -->
    <section class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
      <label class="flex items-center justify-between gap-3">
        <span class="text-sm font-semibold">{{ t('iqBenchmark.admin.settings.enabled') }}</span>
        <input v-model="form.enabled" type="checkbox" :disabled="readonly" />
      </label>
      <p class="mt-1 text-[11px] text-gray-400">{{ t('iqBenchmark.admin.settings.enabledHint') }}</p>
    </section>

    <section class="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.settings.defaultGroupId') }}</label>
        <input v-model.number="form.default_group_id" type="number" min="0" class="input" :disabled="readonly" />
        <p class="mt-1 text-[11px] text-gray-400">{{ t('iqBenchmark.admin.settings.defaultGroupIdHint') }}</p>
      </div>
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.settings.gatewayBaseUrl') }}</label>
        <input v-model="form.internal_gateway_base_url" class="input" :disabled="readonly" />
        <p class="mt-1 text-[11px] text-gray-400">{{ t('iqBenchmark.admin.settings.gatewayBaseUrlHint') }}</p>
      </div>
    </section>

    <section class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.settings.modelTimeout') }}</label>
        <input v-model.number="form.model_timeout_seconds" type="number" min="1" class="input" :disabled="readonly" />
      </div>
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.settings.maxArtifactBytes') }}</label>
        <input v-model.number="form.max_artifact_bytes" type="number" min="0" class="input" :disabled="readonly" />
      </div>
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.settings.maxArtifactsPerRun') }}</label>
        <input v-model.number="form.max_artifacts_per_run" type="number" min="1" class="input" :disabled="readonly" />
      </div>
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.settings.maxTaskConcurrency') }}</label>
        <input v-model.number="form.max_task_concurrency" type="number" min="1" class="input" :disabled="readonly" />
      </div>
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.settings.dailyCostBudget') }}</label>
        <input v-model.number="form.daily_cost_budget" type="number" min="0" step="0.01" class="input" :disabled="readonly" />
        <p class="mt-1 text-[11px] text-gray-400">{{ t('iqBenchmark.admin.settings.dailyCostBudgetHint') }}</p>
      </div>
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.settings.minBalanceFloor') }}</label>
        <input v-model.number="form.min_balance_floor" type="number" min="0" step="0.01" class="input" :disabled="readonly" />
      </div>
    </section>

    <section class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.settings.retentionDays') }}</label>
        <input v-model.number="form.retention_days" type="number" min="1" class="input" :disabled="readonly" />
      </div>
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.settings.maxArtifacts') }}</label>
        <input v-model.number="form.max_artifacts" type="number" min="1" class="input" :disabled="readonly" />
      </div>
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.settings.cleanupInterval') }}</label>
        <input v-model.number="form.cleanup_interval_minutes" type="number" min="5" class="input" :disabled="readonly" />
      </div>
    </section>

    <section class="grid grid-cols-1 gap-3 sm:grid-cols-3">
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.settings.readLimit') }}</label>
        <input v-model.number="form.rate_limit.read_limit_per_minute" type="number" min="0" class="input" :disabled="readonly" />
      </div>
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.settings.heavyLimit') }}</label>
        <input v-model.number="form.rate_limit.heavy_limit_per_minute" type="number" min="0" class="input" :disabled="readonly" />
      </div>
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.settings.maxConcurrent') }}</label>
        <input v-model.number="form.rate_limit.max_concurrent" type="number" min="0" class="input" :disabled="readonly" />
      </div>
    </section>

    <!-- 用户自测：总开关 + 四个配额 -->
    <section class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
      <label class="flex items-center gap-2 text-sm font-medium">
        <input v-model="form.user_test_enabled" type="checkbox" :disabled="readonly" />
        {{ t('iqBenchmark.admin.settings.userTestEnabled') }}
      </label>
      <p class="text-[11px] text-gray-500 dark:text-gray-400">
        {{ t('iqBenchmark.admin.settings.userTestEnabledHint') }}
      </p>

      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <label class="input-label">{{ t('iqBenchmark.admin.settings.userTestDailyLimit') }}</label>
          <input
            v-model.number="form.user_test_daily_limit"
            type="number"
            min="1"
            max="200"
            class="input"
            :disabled="readonly || !form.user_test_enabled"
          />
          <p class="mt-1 text-[10px] text-gray-500 dark:text-gray-400">
            {{ t('iqBenchmark.admin.settings.userTestDailyLimitHint') }}
          </p>
        </div>
        <div>
          <label class="input-label">{{ t('iqBenchmark.admin.settings.userTestMaxConcurrent') }}</label>
          <input
            v-model.number="form.user_test_max_concurrent"
            type="number"
            min="1"
            max="4"
            class="input"
            :disabled="readonly || !form.user_test_enabled"
          />
          <p class="mt-1 text-[10px] text-gray-500 dark:text-gray-400">
            {{ t('iqBenchmark.admin.settings.userTestMaxConcurrentHint') }}
          </p>
        </div>
        <div>
          <label class="input-label">{{ t('iqBenchmark.admin.settings.userTestDailyCostBudget') }}</label>
          <input
            v-model.number="form.user_test_daily_cost_budget"
            type="number"
            min="0"
            step="0.01"
            class="input"
            :disabled="readonly || !form.user_test_enabled"
          />
          <p class="mt-1 text-[10px] text-gray-500 dark:text-gray-400">
            {{ t('iqBenchmark.admin.settings.userTestDailyCostBudgetHint') }}
          </p>
        </div>
        <div class="flex flex-col justify-center">
          <label class="flex items-center gap-2 text-sm">
            <input
              v-model="form.user_test_require_review"
              type="checkbox"
              :disabled="readonly || !form.user_test_enabled"
            />
            {{ t('iqBenchmark.admin.settings.userTestRequireReview') }}
          </label>
          <p class="mt-1 text-[10px] text-gray-500 dark:text-gray-400">
            {{ t('iqBenchmark.admin.settings.userTestRequireReviewHint') }}
          </p>
        </div>
      </div>
    </section>

    <section class="flex flex-wrap gap-6 rounded-lg border border-gray-200 p-3 text-sm dark:border-dark-600">
      <label class="flex items-center gap-2">
        <input v-model="form.scoring_enabled" type="checkbox" :disabled="readonly" />
        {{ t('iqBenchmark.admin.settings.scoringEnabled') }}
      </label>
      <label class="flex items-center gap-2">
        <input v-model="form.expose_cost_to_user" type="checkbox" :disabled="readonly" />
        {{ t('iqBenchmark.admin.settings.exposeCost') }}
      </label>
      <label class="flex items-center gap-2">
        <input v-model="form.expose_tokens_to_user" type="checkbox" :disabled="readonly" />
        {{ t('iqBenchmark.admin.settings.exposeTokens') }}
      </label>
    </section>

    <!-- LLM 裁判：首版不实现，字段保留但恒为关闭 -->
    <input type="hidden" :value="form.llm_judge_enabled" />

    <div class="flex items-center justify-end gap-2">
      <span v-if="saved" class="text-xs text-emerald-600 dark:text-emerald-400">
        {{ t('iqBenchmark.admin.settings.saved') }}
      </span>
      <button class="btn btn-primary" :disabled="readonly || saving" @click="submit">
        {{ t('iqBenchmark.admin.settings.save') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { IQBenchmarkSettings } from './types'

const props = withDefaults(
  defineProps<{
    /** 后端返回的配置；未加载时为 null。 */
    initial?: IQBenchmarkSettings | null
    saving?: boolean
    saved?: boolean
    readonly?: boolean
  }>(),
  { initial: null, saving: false, saved: false, readonly: false },
)

const emit = defineEmits<{ (e: 'submit', settings: IQBenchmarkSettings): void }>()

const { t } = useI18n()

/** 默认值必须与后端 DefaultSettings() 一致，避免首屏出现 0 误导管理员。 */
function defaults(): IQBenchmarkSettings {
  return {
    enabled: false,
    default_group_id: 0,
    internal_gateway_base_url: '',
    interval_min_minutes: 15,
    interval_max_minutes: 10080,
    model_timeout_seconds: 120,
    max_artifact_bytes: 8388608,
    max_artifacts_per_run: 20,
    max_task_concurrency: 4,
    daily_cost_budget: 0,
    min_balance_floor: 0,
    scoring_enabled: false,
    llm_judge_enabled: false,
    expose_cost_to_user: false,
    expose_tokens_to_user: false,
    retention_days: 30,
    max_artifacts: 5000,
    cleanup_interval_minutes: 60,
    rate_limit: {
      read_limit_per_minute: 120,
      heavy_limit_per_minute: 60,
      max_concurrent: 4,
    },
    // 与后端 DefaultSettings() 一致：默认关闭，5 次/日，1 并发，
    // 需审核，成本不限制（0）。
    user_test_enabled: false,
    user_test_daily_limit: 5,
    user_test_max_concurrent: 1,
    user_test_require_review: true,
    user_test_daily_cost_budget: 0,
  }
}

const form = reactive<IQBenchmarkSettings>(defaults())

watch(
  () => props.initial,
  (next) => {
    Object.assign(form, next ? { ...next } : defaults())
    // 首版固定不启用 LLM 裁判：无论后端回什么值都不改。
    form.llm_judge_enabled = false
  },
  { immediate: true },
)

function submit(): void {
  if (props.readonly) return
  emit('submit', { ...form, llm_judge_enabled: false })
}
</script>
