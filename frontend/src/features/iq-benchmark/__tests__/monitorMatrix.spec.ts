import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import MonitorMatrix from '../MonitorMatrix.vue'
import type { IQMatrix, IQMatrixCell } from '../types'

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

function cell(overrides: Partial<IQMatrixCell> = {}): IQMatrixCell {
  return {
    case_id: 1,
    case_name: 'Prompt A',
    model: 'gpt-4o',
    state: 'passed',
    artifact_id: 100,
    verdict: 'ok',
    ...overrides,
  }
}

function matrix(overrides: Partial<IQMatrix> = {}): IQMatrix {
  return {
    cases: [
      { id: 1, name: 'Prompt A', kind: 'text' },
      { id: 2, name: 'Prompt B', kind: 'image' },
    ],
    models: ['gpt-4o', 'claude-x'],
    cells: [
      cell({ case_id: 1, model: 'gpt-4o', state: 'passed', artifact_id: 100 }),
      cell({ case_id: 1, model: 'claude-x', state: 'incorrect', artifact_id: 101, verdict: 'degraded' }),
      cell({ case_id: 2, model: 'gpt-4o', state: 'error', artifact_id: 102, error_kind: 'timeout' }),
      // Prompt B × claude-x 刻意缺失：必须渲染成空格子而不是消失。
    ],
    total: 3,
    truncated: false,
    ...overrides,
  }
}

describe('MonitorMatrix', () => {
  it('渲染题目 × 模型的完整网格，缺失格子补成空格', () => {
    const wrapper = mount(MonitorMatrix, { props: { matrix: matrix() } })

    // 2 题 × 2 模型 = 4 个格子按钮，即使只有 3 条数据。
    // 少了这一条，缺数据的格子会从表里消失，读者看到的会是一个
    // 参差不齐的表格，误以为那是正常形状。
    const buttons = wrapper.findAll('tbody button')
    expect(buttons).toHaveLength(4)
  })

  it('每种状态都同时用颜色和文字编码', () => {
    const wrapper = mount(MonitorMatrix, { props: { matrix: matrix() } })

    // 只有颜色不够：色弱用户看不出红绿差异。文字是第二重编码。
    expect(wrapper.text()).toContain('iqBenchmark.matrix.state.passed')
    expect(wrapper.text()).toContain('iqBenchmark.matrix.state.incorrect')
    expect(wrapper.text()).toContain('iqBenchmark.matrix.state.error')
  })

  it('空格子不可点击', () => {
    const wrapper = mount(MonitorMatrix, { props: { matrix: matrix() } })
    const buttons = wrapper.findAll('tbody button')
    // 最后一个格子是 Prompt B × claude-x（无数据）。
    expect(buttons[3].attributes('disabled')).toBeDefined()
  })

  it('点击有数据的格子发出 select 事件并带上 artifact id', async () => {
    const wrapper = mount(MonitorMatrix, { props: { matrix: matrix() } })
    await wrapper.findAll('tbody button')[0].trigger('click')
    expect(wrapper.emitted('select')).toBeTruthy()
    expect(wrapper.emitted('select')![0]).toEqual([100])
  })

  it('空格子点击不发出事件', async () => {
    const wrapper = mount(MonitorMatrix, { props: { matrix: matrix() } })
    await wrapper.findAll('tbody button')[3].trigger('click')
    expect(wrapper.emitted('select')).toBeFalsy()
  })

  it('截断时显式提示，避免用户以为矩阵是完整的', () => {
    const wrapper = mount(MonitorMatrix, {
      props: { matrix: matrix({ truncated: true, total: 60 }) },
    })
    expect(wrapper.text()).toContain('iqBenchmark.matrix.truncated')
  })

  it('未截断时不显示截断提示', () => {
    const wrapper = mount(MonitorMatrix, { props: { matrix: matrix() } })
    expect(wrapper.text()).not.toContain('iqBenchmark.matrix.truncated')
  })

  it('悬停说明带上错误类别，区分「超时」与「答错」', () => {
    const wrapper = mount(MonitorMatrix, { props: { matrix: matrix() } })
    const buttons = wrapper.findAll('tbody button')
    // Prompt B × gpt-4o 是 error + timeout。
    expect(buttons[2].attributes('title')).toContain('iqBenchmark.verdict.errorKind.timeout')
  })

  it('完全无数据时渲染空态而不是空表格', () => {
    const wrapper = mount(MonitorMatrix, {
      props: { matrix: { cases: [], models: [], cells: [], total: 0, truncated: false } },
    })
    expect(wrapper.find('table').exists()).toBe(false)
    expect(wrapper.text()).toContain('iqBenchmark.matrix.empty')
  })

  it('只有模型没有题目时同样走空态', () => {
    const wrapper = mount(MonitorMatrix, {
      props: { matrix: { cases: [], models: ['gpt-4o'], cells: [], total: 0, truncated: false } },
    })
    expect(wrapper.find('table').exists()).toBe(false)
  })
})
