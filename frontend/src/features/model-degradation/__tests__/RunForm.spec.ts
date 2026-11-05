import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import RunForm from '../components/RunForm.vue'
import type { MDTask } from '../types'
import type { RunFormState } from '../useRunForm'
import { bankTaskToSelection, useRunForm } from '../useRunForm'

// fetchModels 走真实网络，测试里替换掉。
const fetchModels = vi.hoisted(() => vi.fn())
vi.mock('../api', () => ({ fetchModels }))

function makeI18n() {
  return createI18n({
    legacy: false,
    locale: 'zh',
    missing: (_l: string, key: string) => key,
    messages: { zh: {} }
  })
}

/** 造一个已填好地址和密钥的表单状态。 */
function filledState(): RunFormState {
  const { state } = useRunForm()
  state.baseUrl = 'https://relay-gateway.xyz/v1'
  state.apiKey = 'sk-test-key'
  return state
}

function mountForm(state: RunFormState) {
  return mount(RunForm, {
    props: { state, bank: { logic: [], render: [] }, bankLoading: false, errorCode: '', submitting: false },
    global: { plugins: [makeI18n()] }
  })
}

/** 内置题库：与后端 migration 256 的种子保持一致（各一道）。 */
const bankLogic: MDTask = {
  task_type: 'logic',
  key: 'candy-shape-flavor',
  title: '糖果摸取：保证不同形状的苹果味和桃子味',
  prompt: '不使用任何外部工具回答以下问题：在一个黑色的袋子里放有三种口味的糖果……',
  expected: '21'
}

const bankRender: MDTask = {
  task_type: 'render',
  key: 'pelican-bicycle',
  title: '画一只骑自行车的鹈鹕',
  prompt: '不使用任何外部工具回答以下问题：创建一幅独立的 SVG 鹈鹕骑自行车 2D 循环动画……',
  expected: ''
}

/**
 * 带真实题库的挂载。
 *
 * add-bank 在真实页面里由父组件处理（把题库题塞进 state.questions），
 * 单独挂载 RunForm 时没人接这个事件，所以这里补一个等价处理器 ——
 * 与 ModelDegradationView 里的行为一致。
 */
function mountWithBank(state: RunFormState) {
  return mount(RunForm, {
    props: {
      state,
      bank: { logic: [bankLogic], render: [bankRender] },
      bankLoading: false,
      errorCode: '',
      submitting: false
    },
    attrs: {
      onAddBank: (task: MDTask) => {
        if (state.questions.some((q) => q.fromBank && q.key === task.key && q.taskType === task.task_type)) return
        state.questions.push(bankTaskToSelection(task))
      }
    },
    global: { plugins: [makeI18n()] }
  })
}

/** 找到模型输入框（带 placeholder 的那个 text input）。 */
function modelInput(wrapper: ReturnType<typeof mountForm>) {
  return wrapper.find('input[type="text"]')
}

/** 找到刷新按钮。 */
function refreshButton(wrapper: ReturnType<typeof mountForm>) {
  return wrapper.find('.ma-combo-btn')
}

describe('RunForm 模型下拉：拉到列表后必须能看见', () => {
  beforeEach(() => {
    fetchModels.mockReset()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('输入框先失焦、列表后返回，下拉仍然打开', async () => {
    // 这个时序就是线上真实发生的：点刷新按钮 → 输入框 blur →
    // 请求才回来。修复前 blur 当场把下拉关掉，列表有了数据也看不到。
    fetchModels.mockResolvedValue({ models: ['deepseek-v4.1-flash'] })

    const wrapper = mountForm(filledState())
    const input = modelInput(wrapper)

    await input.trigger('focus')
    await refreshButton(wrapper).trigger('click')

    // 点按钮导致失焦
    await input.trigger('blur')

    // 等请求返回并渲染
    await vi.waitFor(() => expect(fetchModels).toHaveBeenCalledTimes(1))
    await vi.waitFor(() => expect(wrapper.findAll('.ma-combo-item').length).toBe(1))

    const items = wrapper.findAll('.ma-combo-item')
    expect(items[0].text()).toBe('deepseek-v4.1-flash')

    wrapper.unmount()
  })

  it('刷新请求在失焦关闭计时器到期前返回时，列表持续可选', async () => {
    vi.useFakeTimers()
    fetchModels.mockResolvedValue({ models: ['deepseek-v4.1-flash'] })
    const state = filledState()
    const wrapper = mountForm(state)
    await modelInput(wrapper).trigger('focus')
    await modelInput(wrapper).trigger('blur')
    await refreshButton(wrapper).trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(200)
    expect(wrapper.findAll('.ma-combo-item')).toHaveLength(1)
    await wrapper.find('.ma-combo-item').trigger('click')
    expect(state.modelId).toBe('deepseek-v4.1-flash')
    wrapper.unmount()
  })

  it('点击列表项能把模型名写回表单', async () => {
    fetchModels.mockResolvedValue({ models: ['deepseek-v4.1-flash', 'gpt-4o'] })

    const state = filledState()
    const wrapper = mountForm(state)

    await modelInput(wrapper).trigger('focus')
    await refreshButton(wrapper).trigger('click')
    // 等 Promise 落地
    await vi.waitFor(() => expect(wrapper.findAll('.ma-combo-item').length).toBe(2))

    await wrapper.findAll('.ma-combo-item')[1].trigger('mousedown')

    expect(state.modelId).toBe('gpt-4o')
    // 选中后下拉应当关闭
    expect(wrapper.findAll('.ma-combo-item').length).toBe(0)

    wrapper.unmount()
  })

  it('拉取失败时列表为空，但错误提示可见', async () => {
    fetchModels.mockResolvedValue({ models: [], error: '上游没有 /models 接口' })

    const wrapper = mountForm(filledState())
    await modelInput(wrapper).trigger('focus')
    await refreshButton(wrapper).trigger('click')

    await vi.waitFor(() => expect(wrapper.find('.ma-hint.is-warn').exists()).toBe(true))
    expect(wrapper.findAll('.ma-combo-item').length).toBe(0)

    wrapper.unmount()
  })

  it('输入的过滤词会筛掉不匹配的项', async () => {
    fetchModels.mockResolvedValue({ models: ['deepseek-v4.1-flash', 'gpt-4o'] })

    const state = filledState()
    const wrapper = mountForm(state)

    await modelInput(wrapper).trigger('focus')
    await refreshButton(wrapper).trigger('click')
    await vi.waitFor(() => expect(wrapper.findAll('.ma-combo-item').length).toBe(2))

    state.modelId = 'gpt'
    await nextTick()

    const items = wrapper.findAll('.ma-combo-item')
    expect(items.length).toBe(1)
    expect(items[0].text()).toBe('gpt-4o')

    wrapper.unmount()
  })

  it('地址或密钥为空时刷新按钮禁用', async () => {
    const { state } = useRunForm()
    state.baseUrl = 'https://relay-gateway.xyz/v1'
    // apiKey 留空

    const wrapper = mountForm(state)
    expect(refreshButton(wrapper).attributes('disabled')).toBeDefined()

    // 补上密钥后可用
    state.apiKey = 'sk-test-key'
    await nextTick()
    expect(refreshButton(wrapper).attributes('disabled')).toBeUndefined()

    wrapper.unmount()
  })
})

describe('RunForm 题型页签：逻辑题 / 生成题', () => {
  it('默认在逻辑题页签，只显示逻辑题', () => {
    const wrapper = mountWithBank(filledState())

    const tabs = wrapper.findAll('.ma-qtab-btn')
    expect(tabs.length).toBe(2)
    expect(tabs[0].classes()).toContain('is-active')

    const chips = wrapper.findAll('.ma-chip')
    expect(chips.length).toBe(1)
    expect(chips[0].text()).toBe(bankLogic.title)

    wrapper.unmount()
  })

  it('切到生成题页签后显示生成题', async () => {
    const wrapper = mountWithBank(filledState())

    await wrapper.findAll('.ma-qtab-btn')[1].trigger('click')

    const chips = wrapper.findAll('.ma-chip')
    expect(chips.length).toBe(1)
    expect(chips[0].text()).toBe(bankRender.title)

    wrapper.unmount()
  })

  it('点题库题会加进已选，且切页签不会把它清掉', async () => {
    const state = filledState()
    const wrapper = mountWithBank(state)

    // 逻辑题页签下选中逻辑题
    await wrapper.find('.ma-chip').trigger('click')
    expect(state.questions.length).toBe(1)
    expect(state.questions[0].key).toBe('candy-shape-flavor')

    // 切到生成题，逻辑题必须还在
    await wrapper.findAll('.ma-qtab-btn')[1].trigger('click')
    expect(state.questions.length).toBe(1)

    // 生成题页签下看不到逻辑题的卡片
    expect(wrapper.findAll('.ma-qcard').length).toBe(0)

    // 再切回去，逻辑题卡片还在
    await wrapper.findAll('.ma-qtab-btn')[0].trigger('click')
    const cards = wrapper.findAll('.ma-qcard')
    expect(cards.length).toBe(1)
    expect(cards[0].text()).toContain(bankLogic.prompt)

    wrapper.unmount()
  })

  it('页签角标显示各题型已选题数', async () => {
    const state = filledState()
    const wrapper = mountWithBank(state)

    // 未选时没有角标
    expect(wrapper.findAll('.ma-qtab-badge').length).toBe(0)

    // 各选一道
    await wrapper.find('.ma-chip').trigger('click')
    await wrapper.findAll('.ma-qtab-btn')[1].trigger('click')
    await wrapper.find('.ma-chip').trigger('click')

    expect(state.questions.length).toBe(2)

    const badges = wrapper.findAll('.ma-qtab-badge')
    expect(badges.length).toBe(2)
    expect(badges[0].text()).toBe('1')
    expect(badges[1].text()).toBe('1')

    wrapper.unmount()
  })

  it('自定义题跟随当前页签的题型', async () => {
    const state = filledState()
    const wrapper = mountWithBank(state)

    // 在生成题页签下点「自定义题目」
    await wrapper.findAll('.ma-qtab-btn')[1].trigger('click')
    await wrapper.find('.ma-qcustom-toggle').trigger('click')

    expect(state.questions.length).toBe(1)
    expect(state.questions[0].taskType).toBe('render')
    expect(state.questions[0].fromBank).toBe(false)

    wrapper.unmount()
  })
})
