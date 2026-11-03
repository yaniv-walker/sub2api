<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" @click.self="$emit('close')">
    <div class="card w-full max-w-lg overflow-hidden">
      <header class="flex items-center justify-between border-b border-gray-200 p-4 dark:border-dark-700">
        <div>
          <h3 class="text-sm font-semibold">{{ t('iqBenchmark.userTest.dialogTitle') }}</h3>
          <p class="mt-1 text-[11px] text-gray-500 dark:text-gray-400">
            {{ t('iqBenchmark.userTest.subtitle') }}
          </p>
        </div>
        <button class="btn btn-ghost btn-sm" @click="$emit('close')" :aria-label="t('iqBenchmark.userTest.cancel')">
          ✕
        </button>
      </header>

      <div class="space-y-4 p-4">
        <!-- 没有可用 Key：直接给去创建的指引，而不是渲染一个空下拉 -->
        <div v-if="!keysLoading && availableKeys.length === 0" class="space-y-2">
          <p class="text-xs font-medium text-gray-700 dark:text-gray-200">
            {{ t('iqBenchmark.userTest.apiKeyEmpty') }}
          </p>
          <p class="text-[11px] text-gray-500 dark:text-gray-400">
            {{ t('iqBenchmark.userTest.apiKeyEmptyHint') }}
          </p>
        </div>

        <template v-else>
          <!-- API Key -->
          <label class="block">
            <span class="mb-1 block text-xs font-medium">{{ t('iqBenchmark.userTest.apiKey') }}</span>
            <select v-model.number="form.api_key_id" class="input w-full" :disabled="keysLoading">
              <option :value="0" disabled>{{ t('iqBenchmark.userTest.apiKeyPlaceholder') }}</option>
              <option v-for="key in availableKeys" :key="key.id" :value="key.id">
                {{ keyLabel(key) }}
              </option>
            </select>
          </label>

          <!--
            题目：按判分口径分两组（logic / render）。
            平铺会让用户以为两组的「通过率」能混着算 —— 图片题的
            verdict 恒为 generated，没有可比的参考答案，混在一起列
            只会让人误解结果。
          -->
          <label class="block">
            <span class="mb-1 block text-xs font-medium">{{ t('iqBenchmark.userTest.case') }}</span>
            <select v-model.number="form.case_id" class="input w-full">
              <option :value="0" disabled>{{ t('iqBenchmark.userTest.casePlaceholder') }}</option>
              <optgroup
                v-if="groupedCases.logic.length > 0"
                :label="t('iqBenchmark.caseGroup.logic')"
              >
                <option v-for="c in groupedCases.logic" :key="c.id" :value="c.id">
                  {{ c.name }} · {{ t(`iqBenchmark.kind.${c.kind}`) }}
                </option>
              </optgroup>
              <optgroup
                v-if="groupedCases.render.length > 0"
                :label="t('iqBenchmark.caseGroup.render')"
              >
                <option v-for="c in groupedCases.render" :key="c.id" :value="c.id">
                  {{ c.name }} · {{ t(`iqBenchmark.kind.${c.kind}`) }}
                </option>
              </optgroup>
            </select>
            <!--
              图片/渲染类题不判分：提前说清楚，避免用户提交后
              看到 verdict=generated 以为是系统出错。
            -->
            <p
              v-if="selectedCaseIsRenderOnly"
              class="mt-1 text-[10px] text-amber-600 dark:text-amber-400"
            >
              {{ t('iqBenchmark.userTest.renderOnlyHint') }}
            </p>
          </label>

          <!-- 分组：Key 绑了分组时锁定（后端会忽略这里的值） -->
          <label class="block">
            <span class="mb-1 block text-xs font-medium">{{ t('iqBenchmark.userTest.group') }}</span>
            <select
              v-model.number="form.group_id"
              class="input w-full disabled:opacity-60"
              :disabled="keyBoundGroupID !== null"
            >
              <!--
                Key 绑定分组可能不在候选列表里（例如分组已停用）。
                补一个占位项，避免 <select> 视觉上「跳回第一项」误导用户。
              -->
              <option v-if="keyBoundGroupID !== null && !groups.some((g) => g.id === keyBoundGroupID)" :value="keyBoundGroupID">
                #{{ keyBoundGroupID }}
              </option>
              <option v-for="g in groups" :key="g.id" :value="g.id">{{ g.name }}</option>
            </select>
            <p v-if="keyBoundGroupID !== null" class="mt-1 text-[10px] text-gray-500 dark:text-gray-400">
              {{ t('iqBenchmark.userTest.groupBoundToKey') }}
            </p>
          </label>

          <!-- 模型：候选由所选分组决定 -->
          <label class="block">
            <span class="mb-1 block text-xs font-medium">{{ t('iqBenchmark.userTest.model') }}</span>
            <select v-model="form.model" class="input w-full" :disabled="availableModels.length === 0">
              <option value="" disabled>{{ t('iqBenchmark.userTest.modelPlaceholder') }}</option>
              <option v-for="m in availableModels" :key="m" :value="m">{{ m }}</option>
            </select>
          </label>
        </template>

        <!-- 错误：用 reason 映射出的文案，不显示后端原始 message -->
        <p v-if="errorKey" class="rounded bg-red-50 p-2 text-[11px] text-red-600 dark:bg-red-900/20 dark:text-red-400">
          {{ t(`iqBenchmark.userTestError.${errorKey}`) }}
        </p>

        <!-- 两条必须让用户看见的告知 -->
        <div class="space-y-1 rounded bg-amber-50 p-2 dark:bg-amber-900/20">
          <p class="text-[11px] text-amber-700 dark:text-amber-300">
            {{ t('iqBenchmark.userTest.costNotice') }}
          </p>
          <p v-if="requireReview" class="text-[11px] text-amber-700 dark:text-amber-300">
            {{ t('iqBenchmark.userTest.reviewNotice') }}
          </p>
        </div>
        <p class="text-[10px] text-gray-400">{{ t('iqBenchmark.userTest.keyFieldNote') }}</p>
      </div>

      <footer class="flex items-center justify-end gap-2 border-t border-gray-200 p-4 dark:border-dark-700">
        <button class="btn btn-secondary btn-sm" @click="$emit('close')">
          {{ t('iqBenchmark.userTest.cancel') }}
        </button>
        <button class="btn btn-primary btn-sm" :disabled="!canSubmit || submitting" @click="onSubmit">
          {{ submitting ? t('iqBenchmark.userTest.submitting') : t('iqBenchmark.userTest.submit') }}
        </button>
      </footer>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 自测对话框。
 *
 * 三条刻意的设计选择：
 *   1. **不在前端做任何额度判断**。日次数、成本、Key 状态全部以后端为准；
 *      前端再算一遍只会和真实规则漂移，让用户看到「按钮可点但一提交就报错」。
 *   2. **不预填默认模型**。分组一换，旧模型可能不在候选里；预填会诱导
 *      用户提交一个必然被拒的组合。
 *   3. 没有可用 Key 时直接给指引，而不是渲染空下拉 —— 空下拉让人不知道
 *      下一步该做什么。
 */
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { fetchMyAPIKeys, fetchMyTargets, fetchPublicCases, submitUserTest } from './api'
import type { IQPublicCase, IQTargetGroup, IQUserAPIKey, IQUserTest } from './types'
import { caseGroupOf, splitCasesByGroup } from './caseSelector'
import { userTestErrorKeyOf } from './userTestError'

const props = defineProps<{
  /** 产出是否需要审核（用于提示文案）。 */
  requireReview: boolean
}>()
// 在脚本里显式读一次，避免「声明了却只用模板」被 TS 判为未使用。
void props.requireReview

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submitted', record: IQUserTest): void
}>()

const { t } = useI18n()

const keys = ref<IQUserAPIKey[]>([])
const cases = ref<IQPublicCase[]>([])
const keysLoading = ref(true)
const submitting = ref(false)
const errorKey = ref('')

/** 分组候选（由 /user-tests/targets 拉取，未绑 Key 时用户可挑）。 */
const groups = ref<IQTargetGroup[]>([])

const form = reactive({
  api_key_id: 0,
  case_id: 0,
  group_id: 0,
  model: '',
})

/** 题目按判分口径分组展示（见 caseSelector.ts）。 */
const groupedCases = computed(() => splitCasesByGroup(cases.value))

/**
 * 当前选中的是不是「只出图、不判分」的题。
 *
 * 这类题的 verdict 恒为 generated —— 不是失败，而是没有判定依据。
 * 提前提示一句，用户看到结果时才知道自己没有「考砸」。
 */
const selectedCaseIsRenderOnly = computed(() => {
  const selected = cases.value.find((c) => c.id === form.case_id)
  return selected ? caseGroupOf(selected) === 'render' : false
})

/** 只有 active 的 Key 可选：disabled 的提交必然被后端拒绝。 */
const availableKeys = computed(() => keys.value.filter((k) => k.status === 'active'))

/**
 * 选中 Key 所绑定的分组。
 *
 * 后端**以 Key 绑定的分组为准**（网关就是这么路由的），所以前端这里
 * 只是把事实展示出来，而不是让用户挑一个可能与实际执行不符的分组。
 */
const keyBoundGroupID = computed<number | null>(() => {
  const key = availableKeys.value.find((k) => k.id === form.api_key_id)
  const gid = key?.group_id
  return typeof gid === 'number' && gid > 0 ? gid : null
})

/**
 * 实际生效的分组。
 *
 * - Key 绑了分组 → 用绑定的（提交时会忽略用户选择）；
 * - Key 未绑分组 → 用用户在分组下拉里的选择。
 */
const effectiveGroupID = computed(() => keyBoundGroupID.value ?? form.group_id)

const selectedGroup = computed(() => groups.value.find((g) => g.id === effectiveGroupID.value) ?? null)
const availableModels = computed(() => selectedGroup.value?.models.map((m) => m.id) ?? [])

const canSubmit = computed(
  () => form.api_key_id > 0 && form.case_id > 0 && effectiveGroupID.value > 0 && form.model !== '',
)

/** Key 下拉的展示文案：脱敏前缀 + 停用标注。 */
function keyLabel(key: IQUserAPIKey): string {
  const prefix = key.key_prefix ? ` (${key.key_prefix})` : ''
  const base = `${key.name}${prefix}`
  return key.status === 'active' ? base : `${base} · ${t('iqBenchmark.userTest.apiKeyDisabled')}`
}

/**
 * 生效分组变化时清空模型。
 *
 * 不能保留旧值：旧模型大概率不在新分组的候选集合里，
 * 保留会让「提交」按钮看起来可点，实际必被后端拒绝。
 * 这里监听 effectiveGroupID（而非 form.group_id），
 * 这样「切换 Key 导致分组被锁定到另一个值」也能正确清空模型。
 */
watch(
  () => effectiveGroupID.value,
  () => {
    if (!availableModels.value.includes(form.model)) {
      form.model = ''
    }
  },
)

/** 分组首次可用时选中第一个，避免初始状态下模型下拉全空。 */
watch(
  () => groups.value.length,
  () => {
    if (form.group_id === 0 && groups.value.length > 0) {
      form.group_id = groups.value[0].id
    }
  },
  { immediate: true },
)

/**
 * 选中 Key 后把分组同步到它绑定的值。
 *
 * 必须同步而不是只读展示：form.group_id 仍会被提交，
 * 若两者不一致，用户会看到下拉显示 A 而请求里是 B。
 */
watch(
  () => keyBoundGroupID.value,
  (bound) => {
    if (bound !== null) {
      form.group_id = bound
    }
  },
  { immediate: true },
)

/** 模型清空后自动选第一个可用项，减少一次必要点击。 */
watch(
  () => availableModels.value,
  (models) => {
    if (form.model === '' && models.length > 0) {
      form.model = models[0]
    }
  },
  { immediate: true },
)

onMounted(async () => {
  try {
    const [fetchedKeys, fetchedCases] = await Promise.all([fetchMyAPIKeys(), fetchPublicCases()])
    keys.value = fetchedKeys
    // 只保留开放自测的题目。后端公开列表不按 allow_user_test 过滤
    //（它是全部启用题目，供筛选下拉用），所以这一步必须在前端做，
    // 否则用户能选中一个提交必被拒的题目。
    cases.value = fetchedCases.filter((c) => c.allow_user_test)
    if (form.api_key_id === 0 && availableKeys.value.length > 0) {
      form.api_key_id = availableKeys.value[0].id
    }
    if (form.case_id === 0 && cases.value.length > 0) {
      form.case_id = cases.value[0].id
    }
    await loadTargets()
  } catch {
    errorKey.value = 'UNKNOWN'
  } finally {
    keysLoading.value = false
  }
})

/**
 * 拉取目标候选。
 *
 * 带上当前 Key：若它绑定了分组，后端只返回那一个分组 —— 与提交时的
 * 判定完全一致，所以下拉里出现的就是真正会执行的分组。
 */
async function loadTargets(): Promise<void> {
  const targets = await fetchMyTargets(form.api_key_id)
  groups.value = targets.groups
  // 分组集合变了之后，若原选择已不存在，重新落一个合法值。
  if (!groups.value.some((g) => g.id === form.group_id)) {
    form.group_id = groups.value.length > 0 ? groups.value[0].id : 0
  }
}

// 换 Key 会改变可用分组（Key 可能绑定到别的分组），必须重取。
watch(
  () => form.api_key_id,
  async (next, prev) => {
    // 首次赋值由 onMounted 处理，避免重复请求。
    if (prev === 0 || next === prev) return
    try {
      await loadTargets()
    } catch {
      errorKey.value = 'UNKNOWN'
    }
  },
)

async function onSubmit(): Promise<void> {
  if (!canSubmit.value || submitting.value) return
  submitting.value = true
  errorKey.value = ''
  try {
    // 提交 effectiveGroupID：Key 绑了分组时它与下拉值相同，
    // 后端也会以后者为准（这里是「说真话」而不是「绕过」）。
    const record = await submitUserTest({ ...form, group_id: effectiveGroupID.value })
    emit('submitted', record)
  } catch (err) {
    errorKey.value = userTestErrorKeyOf(err)
  } finally {
    submitting.value = false
  }
}
</script>
