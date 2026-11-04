import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import RunForm from '../components/RunForm.vue'
import type { RunFormState } from '../useRunForm'
import { useRunForm } from '../useRunForm'

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
