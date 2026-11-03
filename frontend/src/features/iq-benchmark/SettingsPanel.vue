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

    <!--
      可展示分组白名单。

      这是「公开作品展能看到哪些分组」的唯一开关，默认空集 = 什么都不显示。
      之前这个字段只能靠手工 PUT /admin/iq-benchmark/settings 改，
      等于实际上没人能维护它 —— 于是画廊永远是空的。
    -->
    <section class="space-y-2 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
      <div class="flex items-center justify-between">
        <div>
          <p class="text-sm font-semibold">{{ t('iqBenchmark.admin.settings.exposeGroups') }}</p>
          <p class="mt-1 text-[11px] text-gray-400">
            {{ t('iqBenchmark.admin.settings.exposeGroupsHint') }}
          </p>
        </div>
        <button class="btn btn-ghost btn-sm" :disabled="readonly" @click="addExposeGroup">
          {{ t('iqBenchmark.admin.settings.exposeGroupsAdd') }}
        </button>
      </div>

      <p
        v-if="form.expose_groups.length === 0"
        class="rounded bg-amber-50 p-2 text-[11px] text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
      >
        {{ t('iqBenchmark.admin.settings.exposeGroupsEmpty') }}
      </p>

      <div v-else class="space-y-2">
        <div
          v-for="(row, index) in form.expose_groups"
          :key="index"
          class="grid grid-cols-1 gap-2 sm:grid-cols-[8rem_1fr_6rem_auto]"
        >
          <div>
            <label class="input-label">{{ t('iqBenchmark.admin.settings.exposeGroupId') }}</label>
            <input
              v-model.number="row.group_id"
              type="number"
              min="1"
              class="input"
              :disabled="readonly"
            />
          </div>
          <div>
            <label class="input-label">{{ t('iqBenchmark.admin.settings.exposeGroupName') }}</label>
            <input v-model="row.expose_name" class="input" :disabled="readonly" maxlength="60" />
          </div>
          <div>
            <label class="input-label">{{ t('iqBenchmark.admin.settings.exposeGroupSort') }}</label>
            <input
              v-model.number="row.sort_order"
              type="number"
              class="input"
              :disabled="readonly"
            />
          </div>
          <div class="flex items-end">
            <button
              class="btn btn-ghost btn-sm text-red-600 dark:text-red-400"
              :disabled="readonly"
              :aria-label="t('iqBenchmark.admin.settings.exposeGroupRemove')"
              @click="removeExposeGroup(index)"
            >
              {{ t('iqBenchmark.admin.settings.exposeGroupRemove') }}
            </button>
          </div>
        </div>
      </div>

      <!-- 空名条目的后果要说清楚：它不会被「回退成上游分组名」显示 -->
      <p
        v-if="hasBlankExposeName"
        class="text-[11px] text-amber-600 dark:text-amber-400"
      >
        {{ t('iqBenchmark.admin.settings.exposeGroupBlankName') }}
      </p>
    </section>

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
import { computed, reactive, watch } from 'vue'
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
    // 与后端一致：默认空集 = 一个分组都不展示（fail-closed）。
    expose_groups: [],
  }
}

const form = reactive<IQBenchmarkSettings>(defaults())

watch(
  () => props.initial,
  (next) => {
    Object.assign(form, next ? { ...next } : defaults())
    // 首版固定不启用 LLM 裁判：无论后端回什么值都不改。
    form.llm_judge_enabled = false
    // 后端可能返回 null（JSON 里 null 与 [] 都会出现），
    // 统一成数组，否则模板里的 .length 会炸。
    if (!Array.isArray(form.expose_groups)) form.expose_groups = []
    // 深拷贝每一行：reactive 表单直接引用 props 里的对象时，
    // 编辑会就地改动父组件的状态，取消保存也回不去。
    form.expose_groups = form.expose_groups.map((row) => ({ ...row }))
  },
  { immediate: true },
)

/** 有白名单条目但展示名为空 —— 后端不会展示它，也不回退成上游名。 */
const hasBlankExposeName = computed(() =>
  form.expose_groups.some((row) => row.group_id > 0 && row.expose_name.trim() === ''),
)

function addExposeGroup(): void {
  if (props.readonly) return
  // sort_order 递增排列，新行排在最后。
  const nextSort =
    form.expose_groups.length === 0
      ? 0
      : Math.max(...form.expose_groups.map((r) => r.sort_order || 0)) + 10
  form.expose_groups.push({ group_id: 0, expose_name: '', sort_order: nextSort })
}

function removeExposeGroup(index: number): void {
  if (props.readonly) return
  form.expose_groups.splice(index, 1)
}

function submit(): void {
  if (props.readonly) return
  emit('submit', {
    ...form,
    llm_judge_enabled: false,
    // 提交前剔除无效行（group_id <= 0）：后端 Normalize 也会丢，
    // 但在前端先丢掉，管理员能看到「我填的行没生效」而不是静默消失。
    expose_groups: form.expose_groups
      .filter((row) => Number.isFinite(row.group_id) && row.group_id > 0)
      .map((row) => ({
        group_id: row.group_id,
        expose_name: row.expose_name.trim(),
        sort_order: Number.isFinite(row.sort_order) ? row.sort_order : 0,
      })),
  })
}
</script>
