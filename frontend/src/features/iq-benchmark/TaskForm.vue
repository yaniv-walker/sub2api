<template>
  <div class="flex flex-col gap-3">
    <div>
      <label class="input-label">{{ t('iqBenchmark.admin.tasks.name') }}</label>
      <input v-model="form.name" class="input" maxlength="100" />
    </div>

    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.tasks.group') }}</label>
        <select v-model.number="form.group_id" class="input" :disabled="targetsLoading">
          <option v-if="targetsLoading" :value="form.group_id">
            {{ t('common.loading') }}
          </option>
          <option v-else-if="groupOptions.length === 0" :value="form.group_id">
            {{ t('iqBenchmark.admin.tasks.noTargets') }}
          </option>
          <option v-for="group in groupOptions" :key="group.id" :value="group.id">
            {{ groupLabel(group) }}
          </option>
        </select>
        <p v-if="selectedGroupWarning" class="mt-1 text-[11px] text-amber-600 dark:text-amber-400">
          {{ selectedGroupWarning }}
        </p>
      </div>
      <div>
        <label class="input-label">{{ t('iqBenchmark.admin.tasks.model') }}</label>
        <select v-model="form.model" class="input" :disabled="!selectedGroup || selectedGroup.models.length === 0">
          <option value="">{{ t('iqBenchmark.admin.tasks.selectModel') }}</option>
          <option v-for="model in modelOptions" :key="model.id" :value="model.id" :disabled="model.blocked">
            {{ model.id }}
          </option>
        </select>
        <p v-if="orphanModel" class="mt-1 text-[11px] text-amber-600 dark:text-amber-400">
          {{ t('iqBenchmark.admin.tasks.orphanModelHint', { model: form.model }) }}
        </p>
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
        <!--
          按判分口径分两段列出（logic / render）。
          这里不用 <optgroup>：多选是 checkbox 列表，不是 <select>。
          分段的理由与自测对话框一致 —— 两组的 verdict 口径不同，
          混在一列里会让人以为通过率能合着算。
        -->
        <template v-for="group in caseGroupSections" :key="group.key">
          <p
            v-if="group.items.length > 0"
            class="px-1 pt-1 pb-0.5 text-[11px] font-medium uppercase tracking-wide text-gray-400"
          >
            {{ t(`iqBenchmark.caseGroup.${group.key}`) }}
            <span class="font-normal normal-case">({{ group.items.length }})</span>
          </p>
          <label
            v-for="item in group.items"
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
        </template>
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
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { fetchTargets } from './api'
import { splitCasesByGroup } from './caseSelector'
import type { IQAdminTask, IQPublicCase, IQTargetGroup, IQTargets, IQTaskInput } from './types'

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

/**
 * 题目候选按判分口径分段（logic / render）。
 *
 * 顺序固定为 logic 在前：定时任务的主用途是「看模型答对率有没有退化」，
 * render 题的 verdict 恒为 generated，把它排在前面会让人误以为
 * 一次运行什么都没判出来。
 */
const caseGroupSections = computed(() => {
  const split = splitCasesByGroup(props.caseOptions)
  return [
    { key: 'logic' as const, items: split.logic },
    { key: 'render' as const, items: split.render },
  ]
})

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

/**
 * 正在应用 props.initial：用于让「切换分组清空模型」的 watch 闭嘴。
 * 见该 watch 的注释。
 */
let applyingInitial = false

// ---------------- 目标级联下拉 ----------------

/** 后端返回的分组 + 模型候选。 */
const targets = ref<IQTargets>({ groups: [], truncated: false })
const targetsLoading = ref(false)

/**
 * 30s 节流只约束 `refresh=1`（强制失效后端模型缓存），不约束首次加载。
 *
 * 首次加载必须每次都请求：否则打开第二个任务编辑框时，如果刚好落在
 * 上一个的节流窗口内，下拉会是空的。
 */
const REFRESH_THROTTLE_MS = 30_000
let lastRefreshAt = 0

async function loadTargets(forceRefresh = false): Promise<void> {
  if (forceRefresh) {
    const now = Date.now()
    if (now - lastRefreshAt < REFRESH_THROTTLE_MS) return
    lastRefreshAt = now
  }
  targetsLoading.value = true
  try {
    targets.value = await fetchTargets(forceRefresh)
  } finally {
    targetsLoading.value = false
  }
}

const groupOptions = computed<IQTargetGroup[]>(() => {
  const groups = targets.value.groups
  // 只在**编辑既有任务**时才注入占位项。新建任务没有历史值，
  // 注入会显示成「分组 #0（已停用）」这种纯噪音。
  if (!props.initial || selectedKnownGroup.value || form.group_id <= 0) return groups
  // 历史任务的 group_id 已不在 ListActive 内（被禁用/删除）：
  // 注入一个占位项，让下拉显示真实原因而不是「空着」。
  // 这**不**会伪造可选性 —— canSubmit 仍会拦住保存。
  return [
    {
      id: form.group_id,
      name: t('iqBenchmark.admin.tasks.orphanGroupName', { id: form.group_id }),
      platform: '',
      allow_image_generation: false,
      capabilities: [],
      models: [],
      model_source: 'none',
      unavailable_reason: undefined,
    } as IQTargetGroup,
    ...groups,
  ]
})

/** 当前 group_id 是否存在于后端返回的活跃分组中。 */
const selectedKnownGroup = computed(() => targets.value.groups.some((g) => g.id === form.group_id))

const selectedGroup = computed<IQTargetGroup | null>(
  () => groupOptions.value.find((g) => g.id === form.group_id) ?? null,
)

/**
 * 历史任务兼容：任务保存的是**原始值**，即便分组/模型已失效也绝不静默清空。
 *
 * 若把失效值清掉，管理员点一次「保存」就会把原配置永久丢掉，
 * 而他们打开的初衷往往只是改个名字。
 */
const orphanModel = computed(
  () => form.model !== '' && !!selectedGroup.value && !selectedGroup.value.models.some((m) => m.id === form.model),
)

/** 分组下拉的展示文案：带平台与模型数量，便于区分同名分组。 */
function groupLabel(group: IQTargetGroup): string {
  if (!group.platform) return group.name
  const count = group.models.length
  if (count === 0) {
    return `${group.name} · ${group.platform}`
  }
  return `${group.name} · ${group.platform} (${count})`
}

/**
 * 模型下拉的候选集合。
 *
 * 当前已选模型若不在集合内（历史任务，模型已下线），把它作为**禁用**占位项
 * 加回列表：这样 <select> 能显示出原值，而不是视觉上「跳回第一项」——
 * 后者会让管理员误以为值被改了。
 */
const modelOptions = computed(() => {
  const models = selectedGroup.value?.models ?? []
  if (form.model === '' || models.some((m) => m.id === form.model)) {
    return models.map((m) => ({ id: m.id, blocked: false }))
  }
  return [{ id: form.model, blocked: true }, ...models.map((m) => ({ id: m.id, blocked: false }))]
})

/** 选中分组的警告文案（例如「当前没有可调度账号」）。 */
const selectedGroupWarning = computed(() => {
  const reason = selectedGroup.value?.unavailable_reason
  if (!reason) return ''
  return t(`iqBenchmark.admin.targets.reason.${reason}`)
})

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
    // 载入既有任务期间禁用「切换分组清空模型」，否则刚写进去的 model 会被抹掉。
    applyingInitial = true
    try {
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
    } finally {
      // 同步 watch 在下一个 tick 才跑，用 nextTick 才能覆盖到它。
      void nextTick(() => {
        applyingInitial = false
      })
    }
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
 * 级联生效：切换分组时清空模型。
 *
 * 为什么必须清空而不是「尽量保留」：模型 ID 是跨分组不兼容的，
 * 保留上一个分组的模型会直接造出一个后端必定拒绝的目标组合。
 *
 * `applyingInitial` 是必要的：编辑既有任务时 props.initial 的 watch 会
 * 先写 group_id 再写 model，若这里无条件清空，就会把刚载入的模型值抹掉，
 * 表现成「打开编辑框模型是空的」。
 */
watch(
  () => form.group_id,
  (next, prev) => {
    if (applyingInitial) return
    if (prev !== undefined && next !== prev) {
      form.model = ''
    }
  },
)

onMounted(() => {
  void loadTargets()
})

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
    // 目标已失效时禁止保存：后端会以 400 拒绝，前端提前拦住能省一次失败请求，
    // 也让「为什么保存不了」在按钮旁边就看得见。
    !orphanModel.value &&
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
