import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const api = vi.hoisted(() => ({ fetchTargets: vi.fn() }))
vi.mock('../api', async () => {
  const actual = await vi.importActual<typeof import('../api')>('../api')
  return { ...actual, fetchTargets: api.fetchTargets }
})

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

import TaskForm from '../TaskForm.vue'
import type { IQAdminTask, IQTargetGroup, IQTargets } from '../types'

function group(overrides: Partial<IQTargetGroup> = {}): IQTargetGroup {
  return {
    id: 1,
    name: 'Primary',
    platform: 'gemini',
    allow_image_generation: true,
    capabilities: ['text', 'image'],
    models: [
      { id: 'gemini-2.5-pro', capabilities: ['text', 'image'] },
      { id: 'gemini-2.5-flash', capabilities: ['text'] },
    ],
    model_source: 'runtime',
    ...overrides,
  }
}

function targets(groups: IQTargetGroup[]): IQTargets {
  return { groups, truncated: false }
}

/** 建一个已存在的任务，用于验证编辑态不会丢配置。 */
function task(overrides: Partial<IQAdminTask> = {}): IQAdminTask {
  return {
    id: 5,
    name: 'existing',
    group_id: 1,
    model: 'gemini-2.5-pro',
    case_ids: [11],
    interval_minutes: 60,
    jitter_seconds: 0,
    enabled: true,
    status: 'idle',
    status_reason: '',
    lease_owner: '',
    created_at: '2026-11-01T00:00:00Z',
    updated_at: '2026-11-01T00:00:00Z',
    ...overrides,
  } as IQAdminTask
}

const caseOptions = [
  { id: 11, name: 'Case A', kind: 'text' },
  { id: 12, name: 'Case B', kind: 'image' },
] as never

async function mountForm(props: Record<string, unknown> = {}) {
  const wrapper = mount(TaskForm, {
    props: { caseOptions, intervalMin: 15, intervalMax: 10080, ...props },
  })
  await flushPromises()
  return wrapper
}

describe('TaskForm 级联下拉', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.fetchTargets.mockResolvedValue(targets([group()]))
  })

  it('renders groups and only the selected group\'s models', async () => {
    const wrapper = await mountForm()

    const groupSelect = wrapper.findAll('select')[0]
    expect(groupSelect.text()).toContain('Primary')
    expect(groupSelect.text()).toContain('gemini')

    const modelSelect = wrapper.findAll('select')[1]
    expect(modelSelect.text()).toContain('gemini-2.5-pro')
    expect(modelSelect.text()).toContain('gemini-2.5-flash')
  })

  it('does not leak models from another group into the model select', async () => {
    api.fetchTargets.mockResolvedValue(
      targets([group(), group({ id: 2, name: 'Other', models: [{ id: 'other-model', capabilities: ['text'] }] })]),
    )
    const wrapper = await mountForm()

    const modelSelect = wrapper.findAll('select')[1]
    expect(modelSelect.text()).not.toContain('other-model')
  })

  it('clears the model when the group changes', async () => {
    api.fetchTargets.mockResolvedValue(
      targets([group(), group({ id: 2, name: 'Other', models: [{ id: 'other-model', capabilities: ['text'] }] })]),
    )
    const wrapper = await mountForm({ initial: task() })
    await flushPromises()

    const groupSelect = wrapper.findAll('select')[0]
    await groupSelect.setValue('2')
    await flushPromises()

    // 后端会在校验时拒掉跨分组的模型组合，前端必须先把旧值清掉。
    expect(wrapper.findAll('select')[1].element).toHaveProperty('value', '')
  })

  it('keeps the saved model when editing an existing task (no silent clearing)', async () => {
    const wrapper = await mountForm({ initial: task() })
    await flushPromises()

    // 载入 props.initial 会先写 group_id；若清空逻辑误触发，这里会是空字符串。
    const modelSelect = wrapper.findAll('select')[1]
    expect((modelSelect.element as HTMLSelectElement).value).toBe('gemini-2.5-pro')
  })

  it('keeps an offline model visible and blocks saving', async () => {
    // 历史任务的模型已从分组可用集合中下线。
    const wrapper = await mountForm({
      initial: task({ model: 'retired-model' }),
    })
    await flushPromises()

    const modelSelect = wrapper.findAll('select')[1]
    // 原值仍被保留（不是被静默清空），并且作为禁用项出现。
    expect((modelSelect.element as HTMLSelectElement).value).toBe('retired-model')
    const retired = modelSelect.findAll('option').find((o) => o.text() === 'retired-model')
    expect(retired).toBeDefined()
    expect(retired?.attributes('disabled')).toBeDefined()

    // 保存按钮被禁用 + 出现提示文案。
    expect(wrapper.text()).toContain('iqBenchmark.admin.tasks.orphanModelHint')
    const saveButton = wrapper.findAll('button').at(-1)
    expect(saveButton?.attributes('disabled')).toBeDefined()
  })

  it('shows the reason when a group has no selectable models', async () => {
    api.fetchTargets.mockResolvedValue(
      targets([
        group({
          id: 3,
          name: 'Empty',
          models: [],
          capabilities: [],
          model_source: 'none',
          unavailable_reason: 'NO_SCHEDULABLE_ACCOUNT',
        }),
      ]),
    )
    const wrapper = await mountForm({ initial: task({ group_id: 3, model: '' }) })
    await flushPromises()

    expect(wrapper.text()).toContain('iqBenchmark.admin.targets.reason.NO_SCHEDULABLE_ACCOUNT')
  })

  it('shows a placeholder group when the saved group is no longer active', async () => {
    // 任务引用的分组已被禁用/删除，ListActive 不会返回它。
    const wrapper = await mountForm({ initial: task({ group_id: 99, model: 'gone-model' }) })
    await flushPromises()

    const groupSelect = wrapper.findAll('select')[0]
    expect(groupSelect.text()).toContain('iqBenchmark.admin.tasks.orphanGroupName')

    // 原分组 id 与模型都不能被静默改写。
    expect((groupSelect.element as HTMLSelectElement).value).toBe('99')
    expect((wrapper.findAll('select')[1].element as HTMLSelectElement).value).toBe('gone-model')
  })

  it('renders an empty-state label when no groups are available', async () => {
    api.fetchTargets.mockResolvedValue(targets([]))
    const wrapper = await mountForm()

    expect(wrapper.findAll('select')[0].text()).toContain('iqBenchmark.admin.tasks.noTargets')
  })

  it('always reloads targets on mount (throttle only guards refresh=1)', async () => {
    const wrapper = await mountForm()
    expect(api.fetchTargets).toHaveBeenCalledTimes(1)
    expect(api.fetchTargets).toHaveBeenCalledWith(false)

    // 打开第二个编辑框时必须重新拉取：否则会落在上一个的节流窗口里，
    // 下拉变成空的。
    const second = await mountForm()
    expect(api.fetchTargets).toHaveBeenCalledTimes(2)

    wrapper.unmount()
    second.unmount()
  })
})
