/**
 * UserTestDialog 的行为测试。
 *
 * 重点覆盖三类「看起来能用、实际必被拒」的缺陷：
 *   1. 展示了一个提交时会被后端忽略的分组（Key 已绑定分组）；
 *   2. 让用户选中未开放自测的题目；
 *   3. 换分组后保留旧模型，按钮可点但提交必然 MODEL_NOT_AVAILABLE。
 */
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { AxiosError } from 'axios'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      locale: { value: 'en' },
      t: (key: string, params?: Record<string, unknown>) =>
        key.replace(/\{(\w+)\}/g, (_, token: string) => String(params?.[token] ?? `{${token}}`)),
    }),
  }
})

const fetchMyAPIKeys = vi.fn()
const fetchMyTargets = vi.fn()
const fetchPublicCases = vi.fn()
const submitUserTest = vi.fn()

vi.mock('../api', () => ({
  fetchMyAPIKeys: (...a: unknown[]) => fetchMyAPIKeys(...a),
  fetchMyTargets: (...a: unknown[]) => fetchMyTargets(...a),
  fetchPublicCases: (...a: unknown[]) => fetchPublicCases(...a),
  submitUserTest: (...a: unknown[]) => submitUserTest(...a),
}))

import UserTestDialog from '../UserTestDialog.vue'

const KEYS = [
  { id: 11, name: 'bound', key_prefix: 'sk-a', status: 'active', group_id: 3 },
  { id: 12, name: 'free', key_prefix: 'sk-b', status: 'active', group_id: null },
  { id: 13, name: 'off', key_prefix: 'sk-c', status: 'disabled', group_id: 5 },
]

const GROUPS = [
  {
    id: 3,
    name: 'Group Three',
    platform: 'openai',
    allow_image_generation: true,
    capabilities: ['text'],
    models: [{ id: 'gpt-4o', capabilities: ['text'] }],
    model_source: 'runtime' as const,
    unavailable_reason: null,
  },
  {
    id: 5,
    name: 'Group Five',
    platform: 'openai',
    allow_image_generation: false,
    capabilities: ['text'],
    models: [{ id: 'claude-x', capabilities: ['text'] }],
    model_source: 'runtime' as const,
    unavailable_reason: null,
  },
]

const CASES = [
  { id: 7, name: 'Open', kind: 'text', expect_kind: 'text', prompt: 'p', allow_user_test: true },
  { id: 8, name: 'Closed', kind: 'text', expect_kind: 'text', prompt: 'q', allow_user_test: false },
]

function mountDialog() {
  return mount(UserTestDialog, { props: { requireReview: false } })
}

beforeEach(() => {
  vi.clearAllMocks()
  fetchMyAPIKeys.mockResolvedValue(KEYS)
  fetchPublicCases.mockResolvedValue(CASES)
  fetchMyTargets.mockResolvedValue({ groups: GROUPS, truncated: false })
  submitUserTest.mockResolvedValue({ id: 1, status: 'pending' })
})

describe('UserTestDialog', () => {
  it('filters out cases that are not open for self-test', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    // 只应出现开放自测的题目；「Closed」提交必被后端拒绝，不能出现在候选里。
    // 选项文本形如 "Open · iqBenchmark.kind.text"，所以用包含匹配。
    const labels = wrapper.findAll('select')[1].findAll('option').map((o) => o.text())
    expect(labels).toHaveLength(2) // 占位项 + 唯一开放的题目
    expect(labels.some((l) => l.startsWith('Open'))).toBe(true)
    expect(labels.some((l) => l.startsWith('Closed'))).toBe(false)
  })

  it('excludes inactive API keys', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const keyOptions = wrapper.findAll('select')[0].findAll('option')
    const labels = keyOptions.map((o) => o.text())
    expect(labels.some((l) => l.includes('bound'))).toBe(true)
    expect(labels.some((l) => l.includes('free'))).toBe(true)
    expect(labels.some((l) => l.includes('off'))).toBe(false)
  })

  it('locks the group to the key binding and says so', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    // 默认选中第一把 Key（id=11，绑定分组 3）。
    const groupSelect = wrapper.findAll('select')[2]
    expect((groupSelect.element as HTMLSelectElement).disabled).toBe(true)
    expect(wrapper.text()).toContain('iqBenchmark.userTest.groupBoundToKey')
    // 提交时会用绑定分组，所以下拉值必须也是 3。
    expect((groupSelect.element as HTMLSelectElement).value).toBe('3')
  })

  it('lets an unbound key pick a group freely', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.findAll('select')[0].setValue('12')
    await flushPromises()

    const groupSelect = wrapper.findAll('select')[2]
    expect((groupSelect.element as HTMLSelectElement).disabled).toBe(false)
    expect(wrapper.text()).not.toContain('iqBenchmark.userTest.groupBoundToKey')
  })

  it('clears the model when switching to a key bound to another group', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    // 分组 3 只有一个模型，初始应自动选中它。
    expect((wrapper.findAll('select')[3].element as HTMLSelectElement).value).toBe('gpt-4o')

    // 切到未绑定的 Key → 分组候选变成全部；再手动选分组 5。
    await wrapper.findAll('select')[0].setValue('12')
    await flushPromises()
    await wrapper.findAll('select')[2].setValue('5')
    await flushPromises()

    // 分组 5 没有 gpt-4o，旧模型必须被清掉并落到组内第一个可用模型。
    const modelSelect = wrapper.findAll('select')[3]
    expect((modelSelect.element as HTMLSelectElement).value).toBe('claude-x')
  })

  it('renders a translated error key instead of the raw backend message', async () => {
    // 必须构造真正的 AxiosError：reasonOf 用 instanceof 判断，
    // 传一个普通对象会走 UNKNOWN 分支，测不到我们要测的映射。
    const err = new AxiosError('Request failed', 'ERR_BAD_REQUEST')
    err.response = {
      status: 429,
      statusText: 'Too Many Requests',
      headers: {},
      config: {} as never,
      data: { reason: 'USER_TEST_CONCURRENT' },
    }
    submitUserTest.mockRejectedValue(err)
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('footer .btn-primary').trigger('click')
    await flushPromises()

    // 关键：不能把后端的英文 reason 直接糊给用户看 ——
    // 渲染的必须是翻译后的子键（USER_TEST_CONCURRENT → CONCURRENT_LIMIT）。
    expect(wrapper.text()).toContain('iqBenchmark.userTestError.CONCURRENT_LIMIT')
    expect(wrapper.text()).not.toContain('USER_TEST_CONCURRENT')
  })

  it('disables submit until every required field is chosen', async () => {
    fetchMyTargets.mockResolvedValue({ groups: [], truncated: false })
    const wrapper = mountDialog()
    await flushPromises()

    const submit = wrapper.find('footer .btn-primary')
    // 没有可用分组 ⇒ 必然无法提交（否则会发一个 group_id=0 的请求）。
    expect(submit.attributes('disabled')).toBeDefined()
  })

  it('submits the effective group, not a stale selection', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('footer .btn-primary').trigger('click')
    await flushPromises()

    expect(submitUserTest).toHaveBeenCalledTimes(1)
    const payload = submitUserTest.mock.calls[0][0] as { group_id: number; api_key_id: number }
    expect(payload.api_key_id).toBe(11)
    // Key 11 绑定分组 3 —— 必须提交 3。
    expect(payload.group_id).toBe(3)
  })

  // 诚实说明：上面这条断言**抓不到**「把 effectiveGroupID 换成 form.group_id」
  // 这个变异。原因是同一个不变量在这里被三重保护：Key 绑定 watcher 会把
  // form.group_id 同步成 3，loadTargets 在分组集合变化时也会把它落回合法值。
  // 三重保护是有意的（这是一条会花用户真金白银的路径，宁可重复），
  // 但代价是任何单点变异都是等价变异、测试无法区分。
  // 真正的防线在后端：Submit 会忽略请求里的 group_id，直接以 Key 绑定为准
  //（见 user_test_service.go 的 TestUserTestGroupBoundToKeyWins）。

  it('emits submitted on success', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.find('footer .btn-primary').trigger('click')
    await flushPromises()

    expect(wrapper.emitted('submitted')).toBeTruthy()
  })

  it('shows a failure state instead of rendering empty dropdowns when loading fails', async () => {
    fetchMyAPIKeys.mockRejectedValue(new Error('boom'))
    const wrapper = mountDialog()
    await flushPromises()

    expect(wrapper.text()).toContain('iqBenchmark.userTestError.UNKNOWN')
  })
})
