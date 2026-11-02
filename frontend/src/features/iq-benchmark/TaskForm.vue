<template>
  <div class="flex flex-col gap-3">
    <div>
      <label class="input-label">{{ t('iqBenchmark.admin.tasks.name') }}</label>
      <input v-model="form.name" class="input" maxlength="100" />
    </div>

    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.tasks.group') }}</label>
        <input v-model.number="form.group_id" type="number" min="1" class="input" />
      </div>
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.tasks.model') }}</label>
        <input v-model="form.model" class="input" placeholder="gpt-4o" />
      </div>
    </div>

    <div>
      <label class="input-label">
        {{ t('iqBenchmark.admin.tasks.cases') }}
        <span class="ml-1 text-gray-400">
          {{ t('iqBenchmark.admin.tasks.casesSelected', { n: form.case_ids.length }) }}
        </span>
      </label>
      <div class="max-h-48 overflow-auto rounded-lg border border-gray-200 p-2 dark:border-dark-600">
        <label
          v-for="item in caseOptions"
          :key="item.id"
          class="flex items-center gap-2 rounded px-1 py-1 text-xs hover:bg-gray-50 dark:hover:bg-dark-800"
        >
          <input
            type="checkbox"
            :checked="form.case_ids.includes(item.id)"
            @change="toggleCase(item.id)"
          />
          <span class="truncate">{{ item.name }}</span>
          <span class="ml-auto shrink-0 text-gray-400">{{ t(`iqBenchmark.kind.${item.kind}`) }}</span>
        </label>
        <p v-if="caseOptions.length === 0" class="p-2 text-xs text-gray-400">
          {{ t('iqBenchmark.empty.cases') }}
        </p>
      </div>
    </div>

    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.tasks.interval') }}</label>
        <input v-model.number="form.interval_minutes" type="number" :min="intervalMin" :max="intervalMax" class="input" />
        <p class="mt-1 text-[11px] text-gray-400">
          {{ t('iqBenchmark.admin.tasks.intervalHint', { min: intervalMin, max: intervalMax }) }}
        </p>
      </div>
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.tasks.jitter') }}</label>
        <input v-model.number="form.jitter_seconds" type="number" min="0" class="input" />
        <p class="mt-1 text-[11px] text-gray-400">
          {{ t('iqBenchmark.admin.tasks.jitterHint', { n: form.jitter_seconds }) }}
        </p>
      </div>
    </div>

    <label class="flex items-center gap-2 text-sm">
      <input v-model="form.enabled" type="checkbox" />
      {{ t('iqBenchmark.admin.tasks.enabled') }}
      <span class="text-[11px] text-gray-400">— {{ t('iqBenchmark.admin.tasks.enableToRecover') }}</span>
    </label>

    <p v-if="violation" class="rounded-lg bg-red-50 p-2 text-xs text-red-600 dark:bg-red-900/30 dark:text-red-300">
      {{ violation }}
    </p>

    <div class="flex justify-end gap-2 pt-2">
      <button class="btn btn-ghost" @click="$emit('cancel')">{{ t('common.cancel') }}</button>
      <button class="btn btn-primary" :disabled="!canSubmit || saving" @click="submit">
        {{ t('common.save') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { IQAdminTask, IQPublicCase, IQTaskInput } from './types'

const props = withDefaults(
  defineProps<{
    initial?: IQAdminTask | null
    caseOptions?: IQPublicCase[]
    saving?: boolean
    /** 后端配置的上下限（由 SettingsPanel 提供）。 */
    intervalMin?: number
    intervalMax?: number
    /** 后端的「最小有效间隔」约束：interval*60 - jitter >= 900 秒。 */
    minEffectiveIntervalSeconds?: number
  }>(),
  {
    initial: null,
    caseOptions: () => [],
    saving: false,
    intervalMin: 15,
    intervalMax: 10080,
    minEffectiveIntervalSeconds: 900,
  },
)

const emit = defineEmits<{
  (e: 'submit', input: IQTaskInput): void
  (e: 'cancel'): void
}>()

const { t } = useI18n()

interface FormState {
  name: string
  group_id: number
  model: string
  case_ids: number[]
  interval_minutes: number
  jitter_seconds: number
  enabled: boolean
}

const form = reactive<FormState>(blank())

function blank(): FormState {
  return {
    name: '',
    group_id: 1,
    model: '',
    case_ids: [],
    interval_minutes: 60,
    jitter_seconds: 0,
    enabled: false,
  }
}

watch(
  () => props.initial,
  (next) => {
    if (!next) {
      Object.assign(form, blank())
      return
    }
    Object.assign(form, {
      name: next.name,
      group_id: next.group_id,
      model: next.model,
      case_ids: [...next.case_ids],
      interval_minutes: next.interval_minutes,
      jitter_seconds: next.jitter_seconds,
      enabled: next.enabled,
    })
  },
  { immediate: true },
)

function toggleCase(id: number): void {
  const index = form.case_ids.indexOf(id)
  if (index >= 0) {
    form.case_ids.splice(index, 1)
  } else {
    form.case_ids.push(id)
  }
}

/**
 * 前端预校验，规则与后端保持一致。
 *
 * 前端校验不是为了替代后端，而是让用户在点保存前就看到具体问题；
 * 后端仍会独立校验一次（前端可被绕过）。
 */
const violation = computed(() => {
  if (form.interval_minutes < props.intervalMin || form.interval_minutes > props.intervalMax) {
    return t('iqBenchmark.admin.tasks.intervalHint', { min: props.intervalMin, max: props.intervalMax })
  }
  if (form.jitter_seconds < 0) {
    return t('iqBenchmark.admin.tasks.jitterHint', { n: form.jitter_seconds })
  }
  // 间隔减去抖动后必须仍然 >= 最小有效间隔，否则调度会退化成高频轮询。
  if (form.interval_minutes * 60 - form.jitter_seconds < props.minEffectiveIntervalSeconds) {
    return t('iqBenchmark.admin.tasks.jitterHint', { n: form.jitter_seconds })
  }
  return ''
})

const canSubmit = computed(
  () =>
    form.name.trim().length > 0 &&
    form.model.trim().length > 0 &&
    form.group_id > 0 &&
    form.case_ids.length > 0 &&
    violation.value === '',
)

function submit(): void {
  if (!canSubmit.value) return
  emit('submit', {
    name: form.name.trim(),
    group_id: form.group_id,
    model: form.model.trim(),
    case_ids: [...form.case_ids],
    interval_minutes: form.interval_minutes,
    jitter_seconds: form.jitter_seconds,
    enabled: form.enabled,
  })
}
</script>
