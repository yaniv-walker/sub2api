<template>
  <div class="flex flex-col gap-3">
    <div>
      <label class="input-label">{{ t('iqBenchmark.admin.cases.name') }}</label>
      <input v-model="form.name" class="input" maxlength="100" />
    </div>

    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.cases.kind') }}</label>
        <select v-model="form.kind" class="input" :disabled="isBuiltin">
          <option value="image">{{ t('iqBenchmark.kind.image') }}</option>
          <option value="text">{{ t('iqBenchmark.kind.text') }}</option>
          <option value="code">{{ t('iqBenchmark.kind.code') }}</option>
        </select>
      </div>
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.cases.expectKind') }}</label>
        <select v-model="form.expect_kind" class="input" :disabled="isBuiltin">
          <option value="image_url">{{ t('iqBenchmark.kind.image_url') }}</option>
          <option value="svg">{{ t('iqBenchmark.kind.svg') }}</option>
          <option value="markdown">{{ t('iqBenchmark.kind.markdown') }}</option>
          <option value="text">{{ t('iqBenchmark.kind.text') }}</option>
        </select>
      </div>
    </div>

    <div>
      <label class="input-label">{{ t('iqBenchmark.admin.cases.prompt') }}</label>
      <textarea v-model="form.prompt" class="input min-h-[8rem]" rows="6"></textarea>
    </div>

    <div>
      <label class="input-label">{{ t('iqBenchmark.admin.cases.systemPrompt') }}</label>
      <textarea v-model="form.system_prompt" class="input min-h-[4rem]" rows="3"></textarea>
    </div>

    <!--
      参考答案。图片题与已发布的内置题不显示：
        - 图片题产出是图，没有可比的文本（后端 IsImageCase 也豁免）；
        - 内置题豁免是为了让管理员能先建题再补答案，
          强制填写会挡住「先上线基准题」这条路径。
      其余情况必填 —— 没有参考答案就没有判分依据，
      那道题的 verdict 恒为 generated，通过率对它永远不计入。
    -->
    <div v-if="needsExpected">
      <label class="input-label">{{ t('iqBenchmark.admin.cases.expected') }}</label>
      <textarea v-model="form.expected" class="input min-h-[4rem]" rows="3"></textarea>
      <p class="mt-1 text-[11px] text-gray-500 dark:text-gray-400">
        {{ t('iqBenchmark.admin.cases.expectedHint') }}
      </p>
      <p v-if="expectedMissing" class="mt-1 text-[11px] text-amber-600 dark:text-amber-400">
        {{ t('iqBenchmark.admin.cases.expectedRequired') }}
      </p>
    </div>

    <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.cases.sortOrder') }}</label>
        <input v-model.number="form.sort_order" type="number" class="input" />
      </div>
      <div class="flex items-end">
        <label class="flex items-center gap-2 text-sm">
          <input v-model="form.enabled" type="checkbox" />
          {{ t('iqBenchmark.admin.cases.enabled') }}
        </label>
      </div>
      <div class="flex items-end">
        <label class="flex items-center gap-2 text-sm" :title="t('iqBenchmark.admin.cases.allowUserTestHint')">
          <input v-model="form.allow_user_test" type="checkbox" />
          {{ t('iqBenchmark.admin.cases.allowUserTest') }}
        </label>
      </div>
    </div>

    <!-- 一致性提示：图片类题目必须期望 image_url -->
    <p
      v-if="kindMismatch"
      class="rounded-lg bg-amber-50 p-2 text-xs text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
    >
      {{ t('iqBenchmark.admin.cases.paramsHint') }}
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
import type { IQAdminCase, IQCaseInput, IQCaseKind, IQExpectKind } from './types'

const props = withDefaults(
  defineProps<{
    /** 编辑时传入；新建时为 null。 */
    initial?: IQAdminCase | null
    saving?: boolean
  }>(),
  { initial: null, saving: false },
)

const emit = defineEmits<{
  (e: 'submit', input: IQCaseInput): void
  (e: 'cancel'): void
}>()

const { t } = useI18n()

interface FormState {
  name: string
  kind: IQCaseKind
  expect_kind: IQExpectKind
  prompt: string
  system_prompt: string
  sort_order: number
  enabled: boolean
  expected: string
  allow_user_test: boolean
}

const form = reactive<FormState>(blank())

function blank(): FormState {
  return {
    name: '',
    kind: 'text',
    expect_kind: 'text',
    prompt: '',
    system_prompt: '',
    sort_order: 0,
    enabled: true,
    expected: '',
    // 默认关闭（与后端一致）：自测花的是用户自己的额度，
    // 开放应由管理员逐题决定。
    allow_user_test: false,
  }
}

/** 内置题目不允许改类型与期望产出（改动会破坏历史作品的可比性）。 */
const isBuiltin = computed(() => Boolean(props.initial?.builtin))

watch(
  () => props.initial,
  (next) => {
    if (!next) {
      Object.assign(form, blank())
      return
    }
    Object.assign(form, {
      name: next.name,
      kind: next.kind,
      expect_kind: next.expect_kind,
      prompt: next.prompt,
      system_prompt: next.system_prompt ?? '',
      sort_order: next.sort_order,
      enabled: next.enabled,
      expected: next.expected ?? '',
      allow_user_test: next.allow_user_test ?? false,
    })
  },
  { immediate: true },
)

/** 图片类题目必须期望 image_url，其余类型不能期望 image_url。 */
const kindMismatch = computed(() => {
  const imageCase = form.kind === 'image'
  const expectsImage = form.expect_kind === 'image_url'
  return imageCase !== expectsImage
})

/**
 * 是否需要展示「参考答案」输入框。
 *
 * 图片题不需要（产出是图，没有可比的文本）。
 * 内置题不强制（允许先建题再补答案），但**仍然展示** ——
 * 管理员编辑内置题时应该能看到并修正已有答案，藏起来会让
 * 「补上 expected」这件事无处可做。
 */
const needsExpected = computed(() => form.kind !== 'image')

/**
 * 是否缺了必填的参考答案。
 *
 * 判据必须与后端 validateCase 完全一致，否则会出现
 * 「前端放行、后端 400」这种最难排查的组合：
 *   后端条件是 `!builtin && !IsImageCase(kind) && expected == ""`。
 */
const expectedMissing = computed(
  () => !isBuiltin.value && form.kind !== 'image' && form.expected.trim().length === 0,
)

const canSubmit = computed(
  () =>
    form.name.trim().length > 0 &&
    form.prompt.trim().length > 0 &&
    !kindMismatch.value &&
    !expectedMissing.value,
)

function submit(): void {
  if (!canSubmit.value) return
  emit('submit', {
    name: form.name.trim(),
    kind: form.kind,
    expect_kind: form.expect_kind,
    prompt: form.prompt,
    system_prompt: form.system_prompt,
    sort_order: form.sort_order,
    enabled: form.enabled,
    expected: form.expected.trim(),
    allow_user_test: form.allow_user_test,
    params: props.initial?.params ?? {},
    scoring: props.initial?.scoring ?? 'manual',
  })
}
</script>
