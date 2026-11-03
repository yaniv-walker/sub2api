import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SettingsPanel from '../SettingsPanel.vue'
import type { IQBenchmarkSettings } from '../types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ locale: { value: 'en' }, t: (key: string) => key }),
  }
})

function settings(over: Partial<IQBenchmarkSettings> = {}): IQBenchmarkSettings {
  return {
    enabled: true,
    default_group_id: 1,
    internal_gateway_base_url: '',
    interval_min_minutes: 15,
    interval_max_minutes: 10080,
    model_timeout_seconds: 120,
    max_artifact_bytes: 8388608,
    max_artifacts_per_run: 20,
    max_task_concurrency: 4,
    daily_cost_budget: 0,
    min_balance_floor: 0,
    scoring_enabled: false,
    llm_judge_enabled: false,
    expose_cost_to_user: false,
    expose_tokens_to_user: false,
    retention_days: 30,
    max_artifacts: 5000,
    cleanup_interval_minutes: 60,
    rate_limit: { read_limit_per_minute: 120, heavy_limit_per_minute: 60, max_concurrent: 4 },
    user_test_enabled: false,
    user_test_daily_limit: 5,
    user_test_max_concurrent: 1,
    user_test_require_review: true,
    user_test_daily_cost_budget: 0,
    expose_groups: [],
    ...over,
  } as IQBenchmarkSettings
}

/** 取最近一次 submit 事件里的 settings。 */
function lastSubmitted(wrapper: ReturnType<typeof mount>): IQBenchmarkSettings {
  const events = wrapper.emitted('submit') as [IQBenchmarkSettings][] | undefined
  if (!events || events.length === 0) throw new Error('submit was never emitted')
  return events[events.length - 1][0]
}

describe('SettingsPanel expose_groups', () => {
  it('白名单为空时给出明确提示', () => {
    // 空集是默认且 fail-closed 的：画廊、矩阵、自测都看不到任何分组。
    // 不提示的话管理员会以为功能坏了。
    const wrapper = mount(SettingsPanel, { props: { initial: settings() } })
    expect(wrapper.text()).toContain('iqBenchmark.admin.settings.exposeGroupsEmpty')
  })

  it('添加一行后出现输入框', async () => {
    const wrapper = mount(SettingsPanel, { props: { initial: settings() } })
    await wrapper.find('button.btn-ghost').trigger('click')
    expect(wrapper.findAll('input[type="number"]').length).toBeGreaterThan(0)
    expect(wrapper.text()).not.toContain('iqBenchmark.admin.settings.exposeGroupsEmpty')
  })

  it('提交时剔除 group_id 无效的行', async () => {
    // 后端 Normalize 也会丢，但在前端先丢掉，管理员能看到
    // 「我填的行没生效」而不是让它静默消失。
    const wrapper = mount(SettingsPanel, {
      props: {
        initial: settings({
          expose_groups: [
            { group_id: 3, expose_name: 'A', sort_order: 0 },
            { group_id: 0, expose_name: 'bad', sort_order: 10 },
          ],
        }),
      },
    })
    await wrapper.find('button.btn-primary').trigger('click')
    const submitted = lastSubmitted(wrapper)
    expect(submitted.expose_groups).toHaveLength(1)
    expect(submitted.expose_groups[0].group_id).toBe(3)
  })

  it('提交时 trim 展示名', async () => {
    const wrapper = mount(SettingsPanel, {
      props: {
        initial: settings({
          expose_groups: [{ group_id: 3, expose_name: '  A  ', sort_order: 0 }],
        }),
      },
    })
    await wrapper.find('button.btn-primary').trigger('click')
    expect(lastSubmitted(wrapper).expose_groups[0].expose_name).toBe('A')
  })

  it('展示名为空时给出警告（不展示，也不回退成上游名）', () => {
    const wrapper = mount(SettingsPanel, {
      props: {
        initial: settings({ expose_groups: [{ group_id: 3, expose_name: '', sort_order: 0 }] }),
      },
    })
    expect(wrapper.text()).toContain('iqBenchmark.admin.settings.exposeGroupBlankName')
  })

  it('展示名不空时不显示警告', () => {
    const wrapper = mount(SettingsPanel, {
      props: {
        initial: settings({ expose_groups: [{ group_id: 3, expose_name: 'A', sort_order: 0 }] }),
      },
    })
    expect(wrapper.text()).not.toContain('iqBenchmark.admin.settings.exposeGroupBlankName')
  })

  it('后端返回 null 时归一成空数组而不是崩掉', () => {
    // JSON 里 null 与 [] 都会出现；直接 .length 会抛。
    const wrapper = mount(SettingsPanel, {
      props: { initial: settings({ expose_groups: null as unknown as [] }) },
    })
    expect(wrapper.text()).toContain('iqBenchmark.admin.settings.exposeGroupsEmpty')
  })

  it('编辑不会就地改动父组件传进来的对象', async () => {
    // reactive 表单直接引用 props 里的对象时，编辑会就地改父组件状态，
    // 取消保存也回不去。这里断言设置了 isolated 拷贝。
    const original = [{ group_id: 3, expose_name: 'A', sort_order: 0 }]
    const wrapper = mount(SettingsPanel, {
      props: { initial: settings({ expose_groups: original }) },
    })
    const input = wrapper.find('input[maxlength="60"]')
    await input.setValue('CHANGED')
    expect(original[0].expose_name).toBe('A')
  })

  it('移除一行后数量减少', async () => {
    const wrapper = mount(SettingsPanel, {
      props: {
        initial: settings({
          expose_groups: [
            { group_id: 3, expose_name: 'A', sort_order: 0 },
            { group_id: 4, expose_name: 'B', sort_order: 10 },
          ],
        }),
      },
    })
    const removeButtons = wrapper.findAll('button').filter((b) => b.text().includes('exposeGroupRemove'))
    expect(removeButtons).toHaveLength(2)
    await removeButtons[0].trigger('click')
    await wrapper.find('button.btn-primary').trigger('click')
    const submitted = lastSubmitted(wrapper)
    expect(submitted.expose_groups.map((r) => r.group_id)).toEqual([4])
  })

  it('只读模式下不能增删', async () => {
    const wrapper = mount(SettingsPanel, {
      props: { initial: settings(), readonly: true },
    })
    const addButton = wrapper.find('button.btn-ghost')
    expect(addButton.attributes('disabled')).toBeDefined()
  })

  it('提交时保持 llm_judge_enabled 恒为 false', async () => {
    // 首版不实现裁判，字段保留但绝不能因为后端回 true 就被打开。
    const wrapper = mount(SettingsPanel, {
      props: { initial: settings({ llm_judge_enabled: true }) },
    })
    await wrapper.find('button.btn-primary').trigger('click')
    expect(lastSubmitted(wrapper).llm_judge_enabled).toBe(false)
  })
})
