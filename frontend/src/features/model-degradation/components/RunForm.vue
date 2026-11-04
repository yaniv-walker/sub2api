<script setup lang="ts">
/**
 * 发起检测表单。
 *
 * 参考实现里表单是左栏，从上到下：协议切换 → 地址/密钥 → 模型 →
 * 思考等级/生成超时 → 题目选择 → 提交。
 *
 * 两处刻意的设计取舍：
 *   1. 密钥框 type=password 且 autocomplete="off"，并且**不回显**
 *      已经提交过的值 —— 它只在内存里活到提交那一刻。
 *   2. 模型列表拉不到不算错误。上游的 /models 接口各家实现不一，
 *      拉不到就让用户手输。把「拉不到列表」当成阻断会让功能对
 *      一批上游永远不可用。
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { fetchModels } from '../api'
import type { MDProtocol, MDTask, MDTaskBank } from '../types'
import { PROTOCOLS, REASONING_EFFORTS, supportsReasoningEffort } from '../types'
import type { RunFormState } from '../useRunForm'
import { nextUid } from '../useRunForm'

const props = defineProps<{
  state: RunFormState
  bank: MDTaskBank
  bankLoading: boolean
  errorCode: string
  submitting: boolean
  /** 题库是否加载失败（用于提示，不阻断）。 */
  bankFailed?: boolean
}>()

const emit = defineEmits<{
  (e: 'submit'): void
  (e: 'change-protocol', protocol: MDProtocol): void
  (e: 'add-bank', task: MDTask): void
  (e: 'remove', uid: string): void
}>()

const { t } = useI18n()

/** 模型列表拉取的本地状态。 */
const models = ref<string[]>([])
const modelsLoading = ref(false)
const modelsError = ref('')

/** 是否显示模型下拉。 */
const modelOpen = ref(false)

/**
 * 关闭下拉的延时句柄。
 *
 * 为什么不能直接 @blur 关掉：点「刷新」按钮时输入框先失焦，
 * 下拉当场关闭；等请求回来列表已经有数据了，但框是关着的，
 * 用户永远看不到选项 —— 表现为「接口明明拉到了，前端却不显示」。
 * 所以失焦后延一小会儿再关，期间如果重新聚焦或点到列表项就取消。
 */
let closeTimer: ReturnType<typeof setTimeout> | null = null

/** 取消待执行的关闭（重新聚焦、或即将选中某项时用）。 */
function cancelClose() {
  if (closeTimer !== null) {
    clearTimeout(closeTimer)
    closeTimer = null
  }
}

/** 失焦后延时关闭，给「点按钮」「点列表项」留出落点时间。 */
function scheduleClose() {
  cancelClose()
  closeTimer = setTimeout(() => {
    modelOpen.value = false
    closeTimer = null
  }, 150)
}

const protocolOptions = computed(() =>
  PROTOCOLS.map((p) => ({
    value: p,
    label: t(`modelDegradation.form.protocolLabel.${p}`),
    hint: t(`modelDegradation.form.protocolHint.${p}`)
  }))
)

const effortOptions = computed(() =>
  REASONING_EFFORTS.map((e) => ({
    value: e,
    label: t(`modelDegradation.form.reasoningEffortLevel.${e}`)
  }))
)

/** 当前协议的说明，显示在分段控件下方。 */
const protocolHint = computed(() => {
  const found = protocolOptions.value.find((o) => o.value === props.state.protocol)
  return found?.hint ?? ''
})

/**
 * 该协议是否支持思考等级。
 *
 * messages 协议不接受这个参数：硬发过去会被上游当成非法字段返回 400，
 * 而那会被我们的错误分类归到 server_error，看起来像上游坏了。
 */
const effortAvailable = computed(() => supportsReasoningEffort(props.state.protocol))

/** 只要地址和密钥都有内容，就可以拉模型列表。 */
const canFetchModels = computed(() => props.state.baseUrl.trim() !== '' && props.state.apiKey.trim() !== '')

/** 过滤后的模型候选。 */
const filteredModels = computed(() => {
  const q = props.state.modelId.trim().toLowerCase()
  if (!q) return models.value
  return models.value.filter((m) => m.toLowerCase().includes(q))
})

/**
 * 模型框是「可搜索 + 可自由输入」的组合。
 * 当用户输入的内容不在列表里，提示「可直接使用」而不是报错 ——
 * 因为很多上游的模型列表不全或干脆没有这个接口。
 */
const modelIsCustom = computed(() => {
  const v = props.state.modelId.trim()
  if (!v) return false
  return !models.value.some((m) => m === v)
})

/** 拉取模型列表。 */
async function loadModels() {
  if (!canFetchModels.value || modelsLoading.value) return
  modelsLoading.value = true
  modelsError.value = ''
  try {
    const result = await fetchModels({
      base_url: props.state.baseUrl.trim(),
      api_key: props.state.apiKey.trim(),
      protocol: props.state.protocol
    })
    models.value = result.models ?? []
    if (result.error) {
      modelsError.value = result.error
    } else if (models.value.length > 0) {
      // 拉到就用打开下拉展示。否则用户点了刷新按钮，输入框已经失焦、
      // 下拉被 blur 关掉，而列表此时才回来 —— 看到的仍是空框。
      modelOpen.value = true
    }
  } catch {
    // 拉取失败不阻断：用户手输即可。
    modelsError.value = t('modelDegradation.errors.modelsFailed')
    models.value = []
  } finally {
    modelsLoading.value = false
  }
}

/** 选中一个模型。 */
function pickModel(name: string) {
  cancelClose()
  props.state.modelId = name
  modelOpen.value = false
}

/** 切换协议的本地代理：清空已有的模型缓存（不同协议模型集不同）。 */
function onProtocolChange(p: MDProtocol) {
  models.value = []
  modelsError.value = ''
  emit('change-protocol', p)
}

/** 本地加入自定义逻辑题（直接改 state，父组件无需中转）。 */
function addCustom() {
  props.state.questions.push({
    uid: nextUid(),
    taskType: 'logic',
    key: '',
    prompt: '',
    expected: '',
    fromBank: false
  })
}

/** 该题库题是否已被选中。 */
function isBankSelected(task: MDTask): boolean {
  return props.state.questions.some((q) => q.fromBank && q.key === task.key && q.taskType === task.task_type)
}

/** 生成题超时 —— 用原生 select 保证移动端体验。 */
const timeoutLabel = computed(() => t('modelDegradation.form.renderTimeoutUnit'))
</script>

<template>
  <section class="ma-form-col">
    <div class="ma-form-head">
      <h2 class="ma-h2">{{ t('modelDegradation.form.title') }}</h2>
    </div>

    <!-- 协议分段控件 -->
    <div class="ma-seg" role="tablist">
      <button
        v-for="opt in protocolOptions"
        :key="opt.value"
        type="button"
        role="tab"
        class="ma-seg-btn"
        :class="{ 'is-active': state.protocol === opt.value }"
        :aria-selected="state.protocol === opt.value"
        @click="onProtocolChange(opt.value)"
      >
        {{ opt.label }}
      </button>
    </div>
    <p class="ma-hint">{{ protocolHint }}</p>

    <div class="ma-fields">
      <!-- 上游地址 -->
      <label class="ma-field">
        <span class="ma-label">{{ t('modelDegradation.form.baseUrl') }}</span>
        <input
          v-model="state.baseUrl"
          class="ma-input"
          type="url"
          inputmode="url"
          autocomplete="off"
          spellcheck="false"
          :placeholder="t('modelDegradation.form.baseUrlPlaceholder')"
        />
        <span class="ma-hint">{{ t('modelDegradation.form.baseUrlHint') }}</span>
      </label>

      <!-- 密钥 -->
      <label class="ma-field">
        <span class="ma-label">{{ t('modelDegradation.form.apiKey') }}</span>
        <input
          v-model="state.apiKey"
          class="ma-input"
          type="password"
          autocomplete="new-password"
          spellcheck="false"
          :placeholder="t('modelDegradation.form.apiKeyPlaceholder')"
        />
        <span class="ma-hint">{{ t('modelDegradation.form.apiKeyHint') }}</span>
      </label>

      <!-- 模型：可搜索 + 可手输 -->
      <div class="ma-field ma-field-combo">
        <span class="ma-label">{{ t('modelDegradation.form.model') }}</span>
        <div class="ma-combo">
          <input
            v-model="state.modelId"
            class="ma-input"
            type="text"
            autocomplete="off"
            spellcheck="false"
            :placeholder="t('modelDegradation.form.modelPlaceholder')"
            @focus="cancelClose(); modelOpen = true"
            @blur="scheduleClose"
          />
          <button
            type="button"
            class="ma-combo-btn"
            :disabled="!canFetchModels || modelsLoading"
            :title="
              canFetchModels ? t('modelDegradation.form.modelFetch') : t('modelDegradation.form.modelNeedCredentials')
            "
            @click="loadModels"
          >
            <svg v-if="!modelsLoading" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
              <path
                d="M2 8a6 6 0 1 1 1.8 4.3M2 13V9h4"
                fill="none"
                stroke="currentColor"
                stroke-width="1.6"
                stroke-linecap="round"
              />
            </svg>
            <span v-else class="ma-spinner" aria-hidden="true" />
          </button>

          <ul v-if="modelOpen && filteredModels.length" class="ma-combo-list" role="listbox">
            <li v-for="m in filteredModels.slice(0, 50)" :key="m">
              <button type="button" class="ma-combo-item" @mousedown.prevent="pickModel(m)">{{ m }}</button>
            </li>
          </ul>
        </div>
        <span v-if="modelIsCustom" class="ma-hint is-accent">
          {{ t('modelDegradation.form.modelCreatablePrefix', { model: state.modelId.trim() }) }}
        </span>
        <span v-else-if="modelsError" class="ma-hint is-warn">{{ modelsError }}</span>
        <span v-else class="ma-hint">{{ t('modelDegradation.form.modelHint') }}</span>
      </div>

      <!-- 思考等级：只在支持它的协议下出现 -->
      <label v-if="effortAvailable" class="ma-field">
        <span class="ma-label">{{ t('modelDegradation.form.reasoningEffort') }}</span>
        <select v-model="state.reasoningEffort" class="ma-input ma-select">
          <option value="">{{ t('modelDegradation.metrics.reasoningEffortUnset') }}</option>
          <option v-for="opt in effortOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
        </select>
      </label>

      <!-- 生成超时：只在选了生成题时出现 -->
      <label v-if="state.questions.some((q) => q.taskType === 'render')" class="ma-field">
        <span class="ma-label">{{ t('modelDegradation.form.renderTimeout') }}</span>
        <div class="ma-inline">
          <select v-model.number="state.renderTimeoutMinutes" class="ma-input ma-select">
            <option v-for="m in [5, 10, 15, 20, 30, 45, 60]" :key="m" :value="m">{{ m }}</option>
          </select>
          <span class="ma-unit">{{ timeoutLabel }}</span>
        </div>
        <span class="ma-hint">{{ t('modelDegradation.form.renderTimeoutHint') }}</span>
      </label>
    </div>

    <!-- 题目选择 -->
    <div class="ma-questions">
      <div class="ma-question-head">
        <span class="ma-label">{{ t('modelDegradation.form.tasks') }}</span>
        <span class="ma-qcount">{{ state.questions.length }}</span>
      </div>

      <!-- 题库题 -->
      <div v-if="bankLoading" class="ma-qempty">{{ t('modelDegradation.form.taskBankLoading') }}</div>
      <div v-else-if="bankFailed" class="ma-qempty">{{ t('modelDegradation.form.taskBankEmpty') }}</div>
      <template v-else>
        <div class="ma-chip-row">
          <button
            v-for="task in [...bank.logic, ...bank.render]"
            :key="`${task.task_type}-${task.key}`"
            type="button"
            class="ma-chip"
            :class="{ 'is-on': isBankSelected(task) }"
            :disabled="isBankSelected(task)"
            @click="emit('add-bank', task)"
          >
            {{ task.title }}
          </button>
        </div>
      </template>

      <!-- 已选题列表 -->
      <div v-if="state.questions.length" class="ma-qlist">
        <div v-for="q in state.questions" :key="q.uid" class="ma-qcard">
          <div class="ma-qcard-head">
            <span class="ma-qcard-type">{{ q.taskType === 'logic' ? t('modelDegradation.form.taskLogic') : t('modelDegradation.form.taskRender') }}</span>
            <button type="button" class="ma-qcard-del" @click="emit('remove', q.uid)" aria-label="remove">×</button>
          </div>

          <template v-if="q.fromBank">
            <p class="ma-qcard-prompt">{{ q.prompt }}</p>
            <p v-if="q.expected" class="ma-qcard-expected">
              {{ t('modelDegradation.result.expected') }}: {{ q.expected }}
            </p>
          </template>

          <template v-else>
            <textarea
              v-model="q.prompt"
              class="ma-input ma-textarea"
              rows="3"
              :placeholder="t('modelDegradation.form.customPromptPlaceholder')"
            />
            <input
              v-if="q.taskType === 'logic'"
              v-model="q.expected"
              class="ma-input"
              type="text"
              :placeholder="t('modelDegradation.form.customExpectedPlaceholder')"
            />
          </template>
        </div>
      </div>
      <div v-else class="ma-qempty">{{ t('modelDegradation.form.presetNone') }}</div>

      <button type="button" class="ma-qcustom-toggle" @click="addCustom">
        + {{ t('modelDegradation.form.customQuestion') }}
      </button>
    </div>

    <!-- 提交 -->
    <div class="ma-toolbar">
      <button type="button" class="ma-submit" :disabled="submitting" @click="emit('submit')">
        {{ submitting ? t('modelDegradation.form.submitting') : t('modelDegradation.form.submit') }}
      </button>
    </div>

    <p v-if="errorCode" class="ma-error">{{ t(`modelDegradation.errors.${errorCode}`) }}</p>
    <p class="ma-note">{{ t('modelDegradation.form.securityNote') }}</p>
  </section>
</template>

<style scoped>
.ma-form-col {
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 18px;
  border: 1px solid var(--ma-border);
  border-radius: 16px;
  background: var(--ma-panel);
  backdrop-filter: blur(10px);
}

.ma-form-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.ma-h2 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--ma-control-fg);
}

/* 分段控件 */
.ma-seg {
  display: flex;
  gap: 3px;
  padding: 3px;
  border-radius: 10px;
  background: var(--ma-seg-bg);
}

.ma-seg-btn {
  flex: 1;
  padding: 6px 8px;
  border: 1px solid transparent;
  border-radius: 8px;
  background: transparent;
  color: var(--ma-label);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.14s ease;
}

.ma-seg-btn:hover {
  color: var(--ma-control-fg);
}

.ma-seg-btn.is-active {
  border-color: var(--ma-seg-on-border);
  background: var(--ma-seg-on-bg);
  color: var(--ma-control-fg);
  font-weight: 600;
}

/* 字段 */
.ma-fields {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.ma-field {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.ma-label {
  font-size: 12px;
  font-weight: 500;
  color: var(--ma-label);
}

.ma-input {
  width: 100%;
  padding: 8px 10px;
  border: 1px solid var(--ma-input-border);
  border-radius: 8px;
  background: var(--ma-input-bg);
  color: var(--ma-control-fg);
  font-size: 13px;
  font-family: inherit;
  outline: none;
  transition: border-color 0.14s ease;
}

.ma-input:focus {
  border-color: var(--ma-primary);
}

.ma-input::placeholder {
  color: var(--ma-faint);
}

.ma-select {
  cursor: pointer;
}

.ma-textarea {
  resize: vertical;
  line-height: 1.5;
}

.ma-inline {
  display: flex;
  align-items: center;
  gap: 8px;
}

.ma-unit {
  font-size: 12px;
  color: var(--ma-muted);
  white-space: nowrap;
}

.ma-hint {
  margin: 0;
  font-size: 11px;
  line-height: 1.5;
  color: var(--ma-muted);
}

.ma-hint.is-warn {
  color: var(--ma-fail);
}

.ma-hint.is-accent {
  color: var(--ma-primary);
}

/* 模型组合框 */
.ma-field-combo {
  position: relative;
}

.ma-combo {
  position: relative;
  display: flex;
  gap: 6px;
}

.ma-combo-btn {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 34px;
  border: 1px solid var(--ma-input-border);
  border-radius: 8px;
  background: var(--ma-input-bg);
  color: var(--ma-control-fg);
  cursor: pointer;
}

.ma-combo-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.ma-combo-list {
  position: absolute;
  z-index: 20;
  top: calc(100% + 4px);
  left: 0;
  right: 34px;
  max-height: 220px;
  margin: 0;
  padding: 4px;
  list-style: none;
  overflow-y: auto;
  border: 1px solid var(--ma-border);
  border-radius: 10px;
  background: var(--ma-panel-solid);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
}

.ma-combo-item {
  width: 100%;
  padding: 6px 8px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--ma-control-fg);
  font-size: 12px;
  text-align: left;
  cursor: pointer;
}

.ma-combo-item:hover {
  background: var(--ma-seg-on-bg);
}

.ma-spinner {
  width: 12px;
  height: 12px;
  border: 2px solid var(--ma-border);
  border-top-color: var(--ma-primary);
  border-radius: 50%;
  animation: ma-spin 0.7s linear infinite;
}

@keyframes ma-spin {
  to {
    transform: rotate(360deg);
  }
}

/* 题目区 */
.ma-questions {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-top: 4px;
  border-top: 1px solid var(--ma-line);
}

.ma-question-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.ma-qcount {
  min-width: 18px;
  padding: 0 6px;
  border-radius: 9px;
  background: var(--ma-seg-bg);
  color: var(--ma-muted);
  font-size: 11px;
  text-align: center;
}

.ma-chip-row {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.ma-chip {
  padding: 5px 10px;
  border: 1px solid var(--ma-input-border);
  border-radius: 999px;
  background: var(--ma-input-bg);
  color: var(--ma-control-fg);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.14s ease;
}

.ma-chip:hover:not(:disabled) {
  border-color: var(--ma-primary);
}

.ma-chip.is-on {
  border-color: var(--ma-seg-on-border);
  background: var(--ma-seg-on-bg);
  color: var(--ma-muted);
  cursor: default;
}

.ma-qempty {
  padding: 12px;
  border: 1px dashed var(--ma-border);
  border-radius: 10px;
  color: var(--ma-faint);
  font-size: 12px;
  text-align: center;
}

.ma-qlist {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ma-qcard {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 10px;
  border: 1px solid var(--ma-border-soft);
  border-radius: 10px;
  background: var(--ma-panel-soft);
}

.ma-qcard-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.ma-qcard-type {
  font-size: 11px;
  font-weight: 600;
  color: var(--ma-primary);
}

.ma-qcard-del {
  width: 20px;
  height: 20px;
  padding: 0;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--ma-faint);
  font-size: 15px;
  line-height: 1;
  cursor: pointer;
}

.ma-qcard-del:hover {
  background: var(--ma-seg-bg);
  color: var(--ma-fail);
}

.ma-qcard-prompt {
  margin: 0;
  font-size: 12px;
  line-height: 1.55;
  color: var(--ma-control-fg);
  white-space: pre-wrap;
}

.ma-qcard-expected {
  margin: 0;
  font-size: 11px;
  color: var(--ma-muted);
}

.ma-qcustom-toggle {
  align-self: flex-start;
  padding: 5px 10px;
  border: 1px dashed var(--ma-input-border);
  border-radius: 8px;
  background: transparent;
  color: var(--ma-primary);
  font-size: 12px;
  cursor: pointer;
}

.ma-qcustom-toggle:hover {
  border-color: var(--ma-primary);
  background: var(--ma-seg-on-bg);
}

/* 提交 */
.ma-toolbar {
  display: flex;
}

.ma-submit {
  flex: 1;
  padding: 10px 16px;
  border: 0;
  border-radius: 10px;
  background: var(--ma-primary);
  color: var(--ma-primary-fg);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.14s ease;
}

.ma-submit:hover:not(:disabled) {
  opacity: 0.9;
}

.ma-submit:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.ma-error {
  margin: 0;
  padding: 8px 10px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--ma-fail) 12%, transparent);
  color: var(--ma-fail);
  font-size: 12px;
}

.ma-note {
  margin: 0;
  font-size: 11px;
  line-height: 1.55;
  color: var(--ma-faint);
}
</style>
