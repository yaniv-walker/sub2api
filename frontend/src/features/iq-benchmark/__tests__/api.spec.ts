import { beforeEach, describe, expect, it, vi } from 'vitest'

const client = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  post: vi.fn(),
  delete: vi.fn(),
}))
vi.mock('@/api/client', () => ({ apiClient: client }))

import {
  createCase,
  createTask,
  deleteArtifact,
  deleteCase,
  deleteTask,
  fetchAdminArtifacts,
  fetchAdminCases,
  fetchAdminTasks,
  fetchArtifact,
  fetchArtifacts,
  fetchCompare,
  fetchOverview,
  fetchPublicCases,
  fetchRuns,
  fetchSettings,
  fetchTargets,
  runTaskNow,
  saveSettings,
  scoreArtifact,
  setTaskEnabled,
  updateCase,
  updateTask,
} from '../api'
import type { IQBenchmarkSettings, IQCaseInput, IQTaskInput } from '../types'

function envelope<T>(data: T) {
  return { data: { code: 0, message: 'ok', data } }
}

describe('IQ benchmark API — route namespaces', () => {
  beforeEach(() => {
    Object.values(client).forEach((mock) => mock.mockReset())
  })

  it('keeps every public read under /iq-gallery (no admin prefix)', async () => {
    client.get.mockResolvedValue(envelope({ total_artifacts: 0, image_artifacts: 0, text_artifacts: 0, case_count: 0, buckets: [] }))
    await fetchOverview()
    expect(client.get).toHaveBeenCalledWith('/iq-gallery/overview')

    client.get.mockResolvedValue(envelope({ items: [], total: 0, page: 1, page_size: 20, pages: 0 }))
    await fetchArtifacts({ page: 1, page_size: 20 })
    expect(client.get).toHaveBeenCalledWith('/iq-gallery/artifacts', { params: { page: 1, page_size: 20 } })

    client.get.mockResolvedValue(envelope(null))
    await fetchArtifact(7)
    expect(client.get).toHaveBeenCalledWith('/iq-gallery/artifacts/7')

    client.get.mockResolvedValue(envelope([]))
    await fetchCompare([1, 2])
    expect(client.get).toHaveBeenCalledWith('/iq-gallery/artifacts/compare', { params: { ids: '1,2' } })

    client.get.mockResolvedValue(envelope({ items: [], total: 0, page: 1, page_size: 50, pages: 0 }))
    await fetchPublicCases()
    expect(client.get).toHaveBeenCalledWith('/iq-gallery/cases', { params: { page_size: 50 } })
  })

  it('never touches an upstream admin route for the read-only surface', async () => {
    client.get.mockResolvedValue(envelope({ items: [], total: 0, page: 1, page_size: 20, pages: 0 }))
    await fetchArtifacts({ page: 1, page_size: 20 })
    const url = client.get.mock.calls[0][0] as string
    expect(url.startsWith('/iq-gallery/')).toBe(true)
    expect(url).not.toContain('/admin/')
  })

  it('keeps every admin call under /admin/iq-benchmark', async () => {
    client.get.mockResolvedValue(envelope(null))
    await fetchSettings()
    expect(client.get).toHaveBeenCalledWith('/admin/iq-benchmark/settings')

    client.get.mockResolvedValue(envelope({ items: [], total: 0, page: 1, page_size: 50, pages: 0 }))
    await fetchAdminCases()
    expect(client.get).toHaveBeenCalledWith('/admin/iq-benchmark/cases', { params: { page: 1, page_size: 50 } })

    client.get.mockResolvedValue(envelope({ items: [], total: 0, page: 1, page_size: 50, pages: 0 }))
    await fetchAdminTasks()
    expect(client.get).toHaveBeenCalledWith('/admin/iq-benchmark/tasks', { params: { page: 1, page_size: 50 } })

    client.get.mockResolvedValue(envelope({ items: [], total: 0, page: 1, page_size: 20, pages: 0 }))
    await fetchRuns(3)
    expect(client.get).toHaveBeenCalledWith('/admin/iq-benchmark/tasks/3/runs', { params: { page: 1, page_size: 20 } })

    client.get.mockResolvedValue(envelope({ items: [], total: 0, page: 1, page_size: 50, pages: 0 }))
    await fetchAdminArtifacts({ page: 1, page_size: 50 })
    expect(client.get).toHaveBeenCalledWith('/admin/iq-benchmark/artifacts', { params: { page: 1, page_size: 50 } })
  })
})

describe('IQ benchmark API — request shapes', () => {
  beforeEach(() => {
    Object.values(client).forEach((mock) => mock.mockReset())
  })

  it('sends the task enable flag as a body, not a query parameter', async () => {
    client.put.mockResolvedValue(envelope(null))
    await setTaskEnabled(5, false)
    expect(client.put).toHaveBeenCalledWith('/admin/iq-benchmark/tasks/5/enabled', { enabled: false })
  })

  it('sends the artifact score as a body', async () => {
    client.put.mockResolvedValue(envelope(null))
    await scoreArtifact(9, 88)
    expect(client.put).toHaveBeenCalledWith('/admin/iq-benchmark/artifacts/9/score', { score: 88 })
  })

  it('posts run-now without a body', async () => {
    client.post.mockResolvedValue(envelope(null))
    await runTaskNow(4)
    expect(client.post).toHaveBeenCalledWith('/admin/iq-benchmark/tasks/4/run-now')
  })

  it('posts the case payload verbatim', async () => {
    client.post.mockResolvedValue(envelope(null))
    const input: IQCaseInput = {
      name: 'Draw a house',
      kind: 'image',
      expect_kind: 'image_url',
      prompt: 'draw',
      system_prompt: '',
      params: {},
      scoring: 'manual',
      llm_judge_enabled: false,
      enabled: true,
      sort_order: 1,
    }
    await createCase(input)
    expect(client.post).toHaveBeenCalledWith('/admin/iq-benchmark/cases', input)
  })

  it('posts the task payload verbatim', async () => {
    client.post.mockResolvedValue(envelope(null))
    const input: IQTaskInput = {
      name: 'daily',
      group_id: 2,
      model: 'gpt-4o',
      case_ids: [1, 2],
      interval_minutes: 60,
      jitter_seconds: 30,
      enabled: true,
    }
    await createTask(input)
    expect(client.post).toHaveBeenCalledWith('/admin/iq-benchmark/tasks', input)
  })

  it('puts updates to the id-scoped route', async () => {
    client.put.mockResolvedValue(envelope(null))
    await updateCase(11, {
      name: 'n', kind: 'text', expect_kind: 'text', prompt: 'p', system_prompt: '',
      params: {}, scoring: 'manual', llm_judge_enabled: false, enabled: true, sort_order: 0,
    })
    expect(client.put.mock.calls[0][0]).toBe('/admin/iq-benchmark/cases/11')

    client.put.mockResolvedValue(envelope(null))
    await updateTask(12, {
      name: 'n', group_id: 1, model: 'm', case_ids: [1],
      interval_minutes: 60, jitter_seconds: 0, enabled: false,
    })
    expect(client.put.mock.calls[1][0]).toBe('/admin/iq-benchmark/tasks/12')
  })

  it('deletes through the id-scoped route', async () => {
    client.delete.mockResolvedValue({ data: {} })
    await deleteCase(1)
    await deleteTask(2)
    await deleteArtifact(3)
    expect(client.delete.mock.calls.map((c) => c[0])).toEqual([
      '/admin/iq-benchmark/cases/1',
      '/admin/iq-benchmark/tasks/2',
      '/admin/iq-benchmark/artifacts/3',
    ])
  })

  it('puts the whole settings object', async () => {
    client.put.mockResolvedValue(envelope(null))
    const settings = {
      enabled: true,
      llm_judge_enabled: false,
      rate_limit: { read_limit_per_minute: 120, heavy_limit_per_minute: 60, max_concurrent: 4 },
    } as unknown as IQBenchmarkSettings
    await saveSettings(settings)
    expect(client.put).toHaveBeenCalledWith('/admin/iq-benchmark/settings', settings)
  })
})

describe('IQ benchmark API — defensive normalization', () => {
  beforeEach(() => {
    Object.values(client).forEach((mock) => mock.mockReset())
  })

  it('falls back to an empty overview when data is missing', async () => {
    client.get.mockResolvedValue({ data: { code: 0, message: 'ok' } })
    await expect(fetchOverview()).resolves.toEqual({
      total_artifacts: 0,
      image_artifacts: 0,
      text_artifacts: 0,
      case_count: 0,
      buckets: [],
    })
  })

  it('normalizes a missing paginated payload into an empty page', async () => {
    client.get.mockResolvedValue({ data: { code: 0, message: 'ok' } })
    await expect(fetchArtifacts({ page: 1, page_size: 20 })).resolves.toEqual({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
    })
  })

  it('returns an empty array when the case list payload is missing', async () => {
    client.get.mockResolvedValue({ data: { code: 0, message: 'ok' } })
    await expect(fetchPublicCases()).resolves.toEqual([])
  })

  it('returns null for a missing single artifact', async () => {
    client.get.mockResolvedValue({ data: { code: 0, message: 'ok' } })
    await expect(fetchArtifact(1)).resolves.toBeNull()
  })

  it('truncates compare to 8 ids and drops invalid ones', async () => {
    client.get.mockResolvedValue(envelope([]))
    await fetchCompare([1, 2, 3, 4, 5, 6, 7, 8, 9, 10])
    expect((client.get.mock.calls[0][1] as { params: { ids: string } }).params.ids).toBe('1,2,3,4,5,6,7,8')

    client.get.mockClear()
    await fetchCompare([0, -1, Number.NaN, 5])
    expect((client.get.mock.calls[0][1] as { params: { ids: string } }).params.ids).toBe('5')
  })

  it('does not call the network for an empty compare selection', async () => {
    await expect(fetchCompare([])).resolves.toEqual([])
    await expect(fetchCompare([0, -5])).resolves.toEqual([])
    expect(client.get).not.toHaveBeenCalled()
  })

  it('omits empty filter params instead of sending them', async () => {
    client.get.mockResolvedValue(envelope({ items: [], total: 0, page: 1, page_size: 20, pages: 0 }))
    await fetchArtifacts({ page: 1, page_size: 20, model: '', status: '', kind: '' })
    expect(client.get).toHaveBeenCalledWith('/iq-gallery/artifacts', { params: { page: 1, page_size: 20 } })
  })
})

describe('IQ benchmark targets (级联下拉)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('requests the admin targets endpoint', async () => {
    client.get.mockResolvedValue(envelope({ groups: [], truncated: false }))
    await fetchTargets()
    expect(client.get).toHaveBeenCalledWith('/admin/iq-benchmark/targets', { params: undefined })
  })

  it('passes refresh=1 only when a refresh is requested', async () => {
    client.get.mockResolvedValue(envelope({ groups: [], truncated: false }))
    await fetchTargets(true)
    expect(client.get).toHaveBeenCalledWith('/admin/iq-benchmark/targets', { params: { refresh: 1 } })
  })

  it('returns a safe empty shape when the backend omits data', async () => {
    // 后端在功能关闭时可能返回 503，client 层已抛错；这里覆盖 data 为空的情形。
    client.get.mockResolvedValue({ data: { code: 0, message: 'ok' } })
    await expect(fetchTargets()).resolves.toEqual({ groups: [], truncated: false })
  })

  it('preserves groups, models and reason codes verbatim', async () => {
    const payload = {
      groups: [
        {
          id: 7,
          name: '主力',
          platform: 'gemini',
          allow_image_generation: true,
          capabilities: ['text', 'image'],
          models: [{ id: 'gemini-2.5-pro', capabilities: ['text', 'image'] }],
          model_source: 'runtime',
        },
        {
          id: 8,
          name: '空',
          platform: 'openai',
          allow_image_generation: false,
          capabilities: [],
          models: [],
          model_source: 'none',
          unavailable_reason: 'NO_SCHEDULABLE_ACCOUNT',
        },
      ],
      truncated: false,
    }
    client.get.mockResolvedValue(envelope(payload))
    await expect(fetchTargets()).resolves.toEqual(payload)
  })
})
