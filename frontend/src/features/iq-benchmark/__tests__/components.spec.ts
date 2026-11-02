import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ArtifactCard from '../ArtifactCard.vue'
import ArtifactGrid from '../ArtifactGrid.vue'
import ComparePanel from '../ComparePanel.vue'
import FilterBar from '../FilterBar.vue'
import RunTable from '../RunTable.vue'
import type { IQAdminRun, IQPublicArtifact, IQPublicCase } from '../types'
import { defaultFilterState } from '../filters'

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

function artifact(overrides: Partial<IQPublicArtifact> = {}): IQPublicArtifact {
  return {
    id: 1,
    case_id: 10,
    case_name: 'Prompt One',
    case_kind: 'text',
    model: 'gpt-4o',
    render_kind: 'text',
    content: 'hello world',
    status: 'success',
    duration_ms: 1200,
    is_unsafe: false,
    created_at: '2026-11-01T10:00:00Z',
    ...overrides,
  }
}

describe('ArtifactCard', () => {
  it('renders text content and metadata', () => {
    const wrapper = mount(ArtifactCard, { props: { artifact: artifact() } })
    expect(wrapper.text()).toContain('gpt-4o')
    expect(wrapper.text()).toContain('Prompt One')
    expect(wrapper.text()).toContain('hello world')
    expect(wrapper.text()).toContain('1.2 s')
  })

  it('omits cost and tokens when the backend withholds them', () => {
    const wrapper = mount(ArtifactCard, { props: { artifact: artifact() } })
    // 默认不暴露成本/token：卡片上不应出现这两个标签。
    expect(wrapper.text()).not.toContain('iqBenchmark.card.cost')
    expect(wrapper.text()).not.toContain('iqBenchmark.card.tokens')
  })

  it('shows cost and tokens only when present', () => {
    const wrapper = mount(ArtifactCard, {
      props: { artifact: artifact({ cost: 0.0123, input_tokens: 100, output_tokens: 200 }) },
    })
    expect(wrapper.text()).toContain('iqBenchmark.card.cost')
    expect(wrapper.text()).toContain('iqBenchmark.card.tokens')
  })

  it('hides the compare button when not selectable (readonly mode)', () => {
    const wrapper = mount(ArtifactCard, { props: { artifact: artifact(), selectable: false } })
    const labels = wrapper.findAll('button').map((b) => b.text())
    expect(labels.some((l) => l.includes('iqBenchmark.card.compare'))).toBe(false)
    // 详情按钮仍然可用。
    expect(labels.some((l) => l.includes('iqBenchmark.card.viewDetail'))).toBe(true)
  })

  it('renders the compare button when selectable', () => {
    const wrapper = mount(ArtifactCard, { props: { artifact: artifact(), selectable: true } })
    expect(wrapper.findAll('button').some((b) => b.text().includes('iqBenchmark.card.compare'))).toBe(true)
  })

  it('never renders raw content for a blocked artifact', () => {
    const wrapper = mount(ArtifactCard, {
      props: { artifact: artifact({ status: 'blocked', content: '<script>alert(1)</script>' }) },
    })
    expect(wrapper.text()).not.toContain('alert(1)')
    expect(wrapper.text()).toContain('iqBenchmark.card.unsafe')
    expect(wrapper.html()).not.toContain('<script')
  })

  it('never renders raw content when is_unsafe is set', () => {
    const wrapper = mount(ArtifactCard, {
      props: { artifact: artifact({ status: 'success', is_unsafe: true, content: 'leaked' }) },
    })
    expect(wrapper.text()).not.toContain('leaked')
  })

  it('renders SVG through an <img> data URI, never inline', () => {
    const svg = '<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>'
    const wrapper = mount(ArtifactCard, {
      props: { artifact: artifact({ render_kind: 'svg', content: svg }) },
    })
    const img = wrapper.get('img')
    expect(img.attributes('src')!.startsWith('data:image/svg+xml;charset=utf-8,')).toBe(true)
    // 关键：SVG 不得作为 DOM 元素内联注入。
    expect(wrapper.element.querySelector('svg')).toBeNull()
  })

  it('renders image_url artifacts with the plain URL', () => {
    const wrapper = mount(ArtifactCard, {
      props: { artifact: artifact({ render_kind: 'image_url', image_url: 'https://cdn.example/x.png', content: '' }) },
    })
    expect(wrapper.get('img').attributes('src')).toBe('https://cdn.example/x.png')
    expect(wrapper.get('img').attributes('referrerpolicy')).toBe('no-referrer')
  })

  it('marks a truncated artifact', () => {
    const wrapper = mount(ArtifactCard, { props: { artifact: artifact({ truncated: true }) } })
    expect(wrapper.text()).toContain('iqBenchmark.card.contentTruncated')
  })

  it('shows the score only when provided', () => {
    const withScore = mount(ArtifactCard, { props: { artifact: artifact({ score: 87 }) } })
    expect(withScore.text()).toContain('87')
    const withoutScore = mount(ArtifactCard, { props: { artifact: artifact() } })
    expect(withoutScore.text()).not.toContain('87')
  })

  it('emits detail, preview and toggle-compare', async () => {
    const wrapper = mount(ArtifactCard, { props: { artifact: artifact() } })
    const detail = wrapper.findAll('button').find((b) => b.text().includes('iqBenchmark.card.viewDetail'))!
    await detail.trigger('click')
    expect(wrapper.emitted('detail')).toBeTruthy()

    const compare = wrapper.findAll('button').find((b) => b.text().includes('iqBenchmark.card.compare'))!
    await compare.trigger('click')
    expect(wrapper.emitted('toggle-compare')).toBeTruthy()
  })
})

describe('ArtifactGrid', () => {
  it('renders one card per item', () => {
    const wrapper = mount(ArtifactGrid, {
      props: { items: [artifact({ id: 1 }), artifact({ id: 2 })], total: 2 },
    })
    expect(wrapper.findAll('article').length).toBe(2)
  })

  it('shows skeletons while loading with no items', () => {
    const wrapper = mount(ArtifactGrid, { props: { items: [], loading: true } })
    expect(wrapper.findAll('.animate-pulse').length).toBeGreaterThan(0)
    expect(wrapper.text()).not.toContain('iqBenchmark.empty.artifacts')
  })

  it('shows the empty state when there are no items', () => {
    const wrapper = mount(ArtifactGrid, { props: { items: [], loading: false } })
    expect(wrapper.text()).toContain('iqBenchmark.empty.artifacts')
  })

  it('shows the filtered-empty message and a reset action when filters are active', () => {
    const wrapper = mount(ArtifactGrid, {
      props: { items: [], loading: false, showReset: true },
    })
    expect(wrapper.text()).toContain('iqBenchmark.empty.filtered')
    expect(wrapper.findAll('button').some((b) => b.text().includes('iqBenchmark.filter.reset'))).toBe(true)
  })

  it('passes selectable=false through to the cards', () => {
    const wrapper = mount(ArtifactGrid, {
      props: { items: [artifact()], total: 1, selectable: false },
    })
    expect(wrapper.findAll('button').some((b) => b.text().includes('iqBenchmark.card.compare'))).toBe(false)
  })

  it('hides pagination when everything fits on one page', () => {
    const wrapper = mount(ArtifactGrid, { props: { items: [artifact()], total: 1, pageSize: 20 } })
    expect(wrapper.text()).not.toContain('1 / 1')
  })

  it('emits page events from the pager', async () => {
    const wrapper = mount(ArtifactGrid, {
      props: { items: [artifact()], total: 100, page: 2, pageSize: 20 },
    })
    const buttons = wrapper.findAll('button')
    // 第一个是「上一页」，第二个是「下一页」。
    const next = buttons.filter((b) => b.attributes('disabled') === undefined).at(-1)!
    await next.trigger('click')
    expect(wrapper.emitted('page')).toBeTruthy()
  })
})

describe('FilterBar', () => {
  const cases: IQPublicCase[] = [
    { id: 1, name: 'Prompt A', kind: 'text', expect_kind: 'text', prompt: 'p' },
  ]

  it('renders every filter control', () => {
    const wrapper = mount(FilterBar, {
      props: { modelValue: defaultFilterState(), modelOptions: ['gpt-4o'], caseOptions: cases },
    })
    const selects = wrapper.findAll('select')
    expect(selects.length).toBe(5)
    expect(wrapper.text()).toContain('gpt-4o')
    expect(wrapper.text()).toContain('Prompt A')
  })

  it('shows the reset button only when filters deviate from the default', () => {
    const def = mount(FilterBar, { props: { modelValue: defaultFilterState() } })
    expect(def.text()).not.toContain('iqBenchmark.filter.reset')

    const changed = mount(FilterBar, {
      props: { modelValue: { ...defaultFilterState(), model: 'gpt-4o' } },
    })
    expect(changed.text()).toContain('iqBenchmark.filter.reset')
  })

  it('disables refresh during a rate-limit cooldown', () => {
    const wrapper = mount(FilterBar, {
      props: { modelValue: defaultFilterState(), cooldownSeconds: 20 },
    })
    const refresh = wrapper.findAll('button').at(-1)!
    expect(refresh.attributes('disabled')).toBeDefined()
  })

  it('emits update:modelValue when a filter changes', async () => {
    const wrapper = mount(FilterBar, { props: { modelValue: defaultFilterState() } })
    await wrapper.findAll('select')[2].setValue('failed')
    const emitted = wrapper.emitted('update:modelValue')
    expect(emitted).toBeTruthy()
    expect((emitted!.at(-1)![0] as { status: string }).status).toBe('failed')
  })

  it('emits reset and clears the local state', async () => {
    const wrapper = mount(FilterBar, {
      props: { modelValue: { ...defaultFilterState(), model: 'gpt-4o' } },
    })
    const reset = wrapper.findAll('button').find((b) => b.text().includes('iqBenchmark.filter.reset'))!
    await reset.trigger('click')
    expect(wrapper.emitted('reset')).toBeTruthy()
    const last = wrapper.emitted('update:modelValue')!.at(-1)![0] as { model: string }
    expect(last.model).toBe('')
  })
})

describe('ComparePanel', () => {
  it('disables the compare button with fewer than two selections', () => {
    const wrapper = mount(ComparePanel, { props: { selectedIds: [1], results: [] } })
    const compare = wrapper.findAll('button').find((b) => b.text().includes('iqBenchmark.compare.start'))!
    expect(compare.attributes('disabled')).toBeDefined()
  })

  it('shows the empty hint before comparing', () => {
    const wrapper = mount(ComparePanel, { props: { selectedIds: [1, 2], results: [] } })
    expect(wrapper.text()).toContain('iqBenchmark.compare.empty')
  })

  it('groups results by case', () => {
    const wrapper = mount(ComparePanel, {
      props: {
        selectedIds: [1, 2, 3],
        results: [
          artifact({ id: 1, case_id: 10, case_name: 'A' }),
          artifact({ id: 2, case_id: 10, case_name: 'A', model: 'other' }),
          artifact({ id: 3, case_id: 20, case_name: 'B' }),
        ],
      },
    })
    const headings = wrapper.findAll('h4').map((h) => h.text())
    expect(headings).toContain('A')
    expect(headings).toContain('B')
  })

  it('warns when the comparison cap is reached', () => {
    const wrapper = mount(ComparePanel, {
      props: { selectedIds: [1, 2, 3, 4, 5, 6, 7, 8], results: [] },
    })
    expect(wrapper.text()).toContain('iqBenchmark.compare.limit')
  })

  it('emits remove with the right id', async () => {
    const wrapper = mount(ComparePanel, {
      props: { selectedIds: [1, 2], results: [artifact({ id: 1 }), artifact({ id: 2 })] },
    })
    const remove = wrapper.findAll('button').find((b) => b.attributes('title') === 'iqBenchmark.compare.remove')!
    await remove.trigger('click')
    expect(wrapper.emitted('remove')).toBeTruthy()
  })
})

describe('RunTable', () => {
  const run = (overrides: Partial<IQAdminRun> = {}): IQAdminRun => ({
    id: 1,
    task_id: 1,
    trigger: 'schedule',
    status: 'success',
    success_count: 3,
    failed_count: 0,
    timeout_count: 0,
    blocked_count: 0,
    total_cost: 0.01,
    started_at: '2026-11-01T10:00:00Z',
    finished_at: '2026-11-01T10:01:00Z',
    ...overrides,
  })

  it('renders a row per run', () => {
    const wrapper = mount(RunTable, { props: { runs: [run({ id: 1 }), run({ id: 2 })] } })
    expect(wrapper.findAll('tbody tr').length).toBe(2)
  })

  it('shows the empty state with no runs', () => {
    const wrapper = mount(RunTable, { props: { runs: [] } })
    expect(wrapper.text()).toContain('iqBenchmark.empty.runs')
  })

  it('renders a dash for a run that has not finished', () => {
    const wrapper = mount(RunTable, { props: { runs: [run({ status: 'running', finished_at: undefined })] } })
    expect(wrapper.text()).toContain('—')
  })
})
