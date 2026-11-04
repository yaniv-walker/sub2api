/**
 * 发起检测表单的状态与校验。
 *
 * 校验顺序**必须**与后端 service.go 的 Start() 完全一致：
 *   base_url → api_key → model → task → render_timeout → question → custom_expected
 *
 * 顺序不一致的后果很具体：用户点了提交，前端说「请填模型」，
 * 后端却说「请填密钥」，两个提示互相打架，用户不知道该听谁。
 * 因此这里逐条对齐，并在注释里标明对应关系。
 */
import { computed, reactive, ref } from 'vue'
import type { MDQuestionInput, MDStartInput, MDTask, MDTaskType } from './types'
import {
  DEFAULT_RENDER_TIMEOUT_MINUTES,
  RENDER_TIMEOUT_CHOICES,
  supportsReasoningEffort
} from './types'
import type { MDProtocol, MDReasoningEffort } from './types'

/** 一道被选中的题（题库题或自定义题的统一形态）。 */
export interface SelectedQuestion {
  /** 稳定的前端 key，用于列表渲染与删除。 */
  uid: string
  taskType: MDTaskType
  /** 题库题的 key；自定义题为空串。 */
  key: string
  prompt: string
  expected: string
  /** 是否来自题库（题库题不可编辑正文）。 */
  fromBank: boolean
}

/** 表单字段。 */
export interface RunFormState {
  baseUrl: string
  apiKey: string
  modelId: string
  protocol: MDProtocol
  reasoningEffort: MDReasoningEffort
  renderTimeoutMinutes: number
  questions: SelectedQuestion[]
}

/** 校验错误码，与 i18n 的 errors.* 一一对应。 */
export type FormErrorCode =
  | ''
  | 'needBaseUrl'
  | 'needApiKey'
  | 'needModel'
  | 'needTask'
  | 'needRenderTimeout'
  | 'needQuestion'
  | 'needCustomExpected'

let uidSeed = 0

/** 生成一个前端唯一 id。 */
export function nextUid(): string {
  uidSeed += 1
  return `q-${uidSeed}`
}

/** 把题库题转成可选题。 */
export function bankTaskToSelection(task: MDTask): SelectedQuestion {
  return {
    uid: nextUid(),
    taskType: task.task_type,
    key: task.key,
    prompt: task.prompt,
    expected: task.expected,
    fromBank: true
  }
}

export function useRunForm() {
  const state = reactive<RunFormState>({
    baseUrl: '',
    apiKey: '',
    modelId: '',
    protocol: 'chat_completions',
    reasoningEffort: '',
    renderTimeoutMinutes: DEFAULT_RENDER_TIMEOUT_MINUTES,
    questions: []
  })

  /** 当前校验错误（提交时才算，避免边打字边报错）。 */
  const errorCode = ref<FormErrorCode>('')
  /** 是否正在提交。 */
  const submitting = ref(false)

  /** 已选的题型集合。 */
  const selectedTypes = computed<MDTaskType[]>(() => {
    const set = new Set<MDTaskType>()
    for (const q of state.questions) set.add(q.taskType)
    return Array.from(set)
  })

  /** 是否包含生成题（决定是否校验超时）。 */
  const hasRender = computed(() => selectedTypes.value.includes('render'))

  /** 该协议是否支持思考等级。 */
  const effortAvailable = computed(() => supportsReasoningEffort(state.protocol))

  /** 生成题超时可选值。 */
  const timeoutChoices = RENDER_TIMEOUT_CHOICES

  /** 切换协议时清掉不再适用的思考等级。 */
  function setProtocol(protocol: MDProtocol) {
    state.protocol = protocol
    if (!supportsReasoningEffort(protocol)) {
      state.reasoningEffort = ''
    }
  }

  /** 加入一道题库题。 */
  function addFromBank(task: MDTask) {
    // 同一道题库题不重复加入。
    const exists = state.questions.some((q) => q.fromBank && q.key === task.key && q.taskType === task.task_type)
    if (exists) return
    state.questions.push(bankTaskToSelection(task))
    errorCode.value = ''
  }

  /** 加入一道自定义逻辑题。 */
  function addCustomLogic() {
    state.questions.push({
      uid: nextUid(),
      taskType: 'logic',
      key: '',
      prompt: '',
      expected: '',
      fromBank: false
    })
    errorCode.value = ''
  }

  /** 移除一道题。 */
  function remove(uid: string) {
    const index = state.questions.findIndex((q) => q.uid === uid)
    if (index >= 0) state.questions.splice(index, 1)
  }

  /** 重置为初始状态（保留协议与超时偏好）。 */
  function reset() {
    state.baseUrl = ''
    state.apiKey = ''
    state.modelId = ''
    state.reasoningEffort = ''
    state.questions = []
    errorCode.value = ''
    submitting.value = false
  }

  /**
   * 校验。返回错误码，空串表示通过。
   *
   * 顺序与后端严格一致，见文件头注释。
   */
  function validate(): FormErrorCode {
    // 1. base_url
    if (!state.baseUrl.trim()) return 'needBaseUrl'
    // 2. api_key
    if (!state.apiKey.trim()) return 'needApiKey'
    // 3. model
    if (!state.modelId.trim()) return 'needModel'
    // 4. task（至少一道题）
    if (state.questions.length === 0) return 'needTask'
    // 5. render_timeout（只在包含生成题时校验）
    if (hasRender.value) {
      const minutes = state.renderTimeoutMinutes
      if (!Number.isFinite(minutes) || minutes < 1 || minutes > 60) return 'needRenderTimeout'
    }
    // 6. question（自定义题必须有题干）
    for (const q of state.questions) {
      if (!q.fromBank && !q.prompt.trim()) return 'needQuestion'
    }
    // 7. custom_expected（自定义逻辑题必须有期望答案）
    for (const q of state.questions) {
      if (!q.fromBank && q.taskType === 'logic' && !q.expected.trim()) return 'needCustomExpected'
    }
    return ''
  }

  /** 构造提交体。校验必须已通过。 */
  function toPayload(): MDStartInput {
    const questions: MDQuestionInput[] = state.questions.map((q) => ({
      task_type: q.taskType,
      key: q.key,
      prompt: q.prompt.trim(),
      expected: q.expected.trim()
    }))

    return {
      base_url: state.baseUrl.trim(),
      api_key: state.apiKey.trim(),
      model_id: state.modelId.trim(),
      protocol: state.protocol,
      reasoning_effort: effortAvailable.value ? state.reasoningEffort : undefined,
      render_timeout_seconds: hasRender.value ? state.renderTimeoutMinutes * 60 : undefined,
      tasks: selectedTypes.value,
      questions
    }
  }

  return {
    state,
    errorCode,
    submitting,
    selectedTypes,
    hasRender,
    effortAvailable,
    timeoutChoices,
    setProtocol,
    addFromBank,
    addCustomLogic,
    remove,
    reset,
    validate,
    toPayload
  }
}
