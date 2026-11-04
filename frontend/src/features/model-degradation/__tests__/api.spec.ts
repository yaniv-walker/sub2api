import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => {
  const get = vi.fn()
  const post = vi.fn()
  const put = vi.fn()
  return { apiClient: { get, post, put }, default: { get, post, put } }
})

import { apiClient } from '@/api/client'
import {
  cancelRun,
  fetchModels,
  fetchRunDetail,
  fetchRuns,
  fetchStats,
  fetchTaskBank,
  fetchVisibility,
  setGroupHidden,
  setHostHidden,
  startRun
} from '../api'

const mocked = apiClient as unknown as {
  get: ReturnType<typeof vi.fn>
  post: ReturnType<typeof vi.fn>
  put: ReturnType<typeof vi.fn>
}

/** 构造一个标准信封响应。 */
function envelope<T>(data: T) {
  return { data: { code: 0, message: 'ok', data } }
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('model-degradation api', () => {
  it('所有接口都挂在 /model-degradation 与 /admin/model-degradation 下', async () => {
    mocked.get.mockResolvedValue(envelope({ logic: [], render: [] }))
    await fetchTaskBank()
    expect(mocked.get).toHaveBeenCalledWith('/model-degradation/tasks')

    mocked.post.mockResolvedValue(envelope({ models: [] }))
    await fetchModels({ base_url: 'https://a.example.com', api_key: 'k', protocol: 'chat_completions' })
    expect(mocked.post).toHaveBeenCalledWith('/model-degradation/models', expect.any(Object))

    mocked.get.mockResolvedValue(envelope({ hidden_group_ids: [], hidden_hosts: [], hosts: [] }))
    await fetchVisibility()
    expect(mocked.get).toHaveBeenCalledWith('/admin/model-degradation/history-visibility')
  })

  it('fetchTaskBank 解包信封，并在缺字段时回退成空数组', async () => {
    mocked.get.mockResolvedValue(envelope({}))
    const bank = await fetchTaskBank()
    expect(bank.logic).toEqual([])
    expect(bank.render).toEqual([])
  })

  it('fetchTaskBank 原样保留题库内容', async () => {
    mocked.get.mockResolvedValue(
      envelope({
        logic: [{ task_type: 'logic', key: 'candy', title: '糖果', prompt: 'p', expected: '21' }],
        render: [
          { task_type: 'render', key: 'pelican', title: '鹈鹕', prompt: 'p', expected: '' }
        ]
      })
    )
    const bank = await fetchTaskBank()
    expect(bank.logic).toHaveLength(1)
    expect(bank.logic[0].expected).toBe('21')
    expect(bank.render).toHaveLength(1)
  })

  it('fetchModels 把 {models,error} 一起带出来', async () => {
    mocked.post.mockResolvedValue(envelope({ models: ['gpt-4o'], error: 'no list endpoint' }))
    const result = await fetchModels({
      base_url: 'https://a.example.com',
      api_key: 'k',
      protocol: 'chat_completions'
    })
    expect(result.models).toEqual(['gpt-4o'])
    expect(result.error).toBe('no list endpoint')
  })

  it('fetchModels 在接口完全不返回 models 时给空数组', async () => {
    mocked.post.mockResolvedValue(envelope({}))
    const result = await fetchModels({
      base_url: 'https://a.example.com',
      api_key: 'k',
      protocol: 'messages'
    })
    expect(result.models).toEqual([])
  })

  it('startRun 透传完整提交体', async () => {
    mocked.post.mockResolvedValue(envelope({ id: 7, status: 'running' }))
    const payload = {
      base_url: 'https://a.example.com',
      api_key: 'secret',
      model_id: 'gpt-4o',
      protocol: 'chat_completions' as const,
      reasoning_effort: 'high' as const,
      render_timeout_seconds: 900,
      tasks: ['logic' as const],
      questions: [{ task_type: 'logic' as const, key: 'candy', prompt: 'p', expected: '21' }]
    }
    const detail = await startRun(payload)
    expect(mocked.post).toHaveBeenCalledWith('/model-degradation/runs', payload)
    expect(detail.id).toBe(7)
  })

  it('fetchRuns 把分页与过滤放进 params，而不是手拼查询串', async () => {
    mocked.get.mockResolvedValue(envelope({ items: [], total: 0 }))

    await fetchRuns({ limit: 12, offset: 24 })
    expect(mocked.get).toHaveBeenCalledWith('/model-degradation/runs', {
      params: { limit: 12, offset: 24 }
    })

    mocked.get.mockClear()
    await fetchRuns({ limit: 12, offset: 24, verdict: 'degraded', task_type: 'render' })
    expect(mocked.get).toHaveBeenCalledWith('/model-degradation/runs', {
      params: { limit: 12, offset: 24, verdict: 'degraded', task_type: 'render' }
    })
  })

  it('fetchRuns 空过滤值与 0 偏移不进 params', async () => {
    mocked.get.mockResolvedValue(envelope({ items: [], total: 0 }))
    await fetchRuns({ limit: 12, offset: 0, verdict: '', task_type: '' })
    expect(mocked.get).toHaveBeenCalledWith('/model-degradation/runs', { params: { limit: 12 } })
  })

  it('fetchRuns mine=1 只在显式要求时出现', async () => {
    mocked.get.mockResolvedValue(envelope({ items: [], total: 0 }))
    await fetchRuns({ limit: 5, mine: true })
    expect(mocked.get).toHaveBeenCalledWith('/model-degradation/runs', {
      params: { limit: 5, mine: 1 }
    })
  })

  it('fetchRuns 缺 items 时给空数组与 0 总数', async () => {
    mocked.get.mockResolvedValue(envelope({}))
    const result = await fetchRuns({ limit: 12, offset: 0 })
    expect(result.items).toEqual([])
    expect(result.total).toBe(0)
  })

  it('fetchRunDetail / fetchStats 走对路径', async () => {
    mocked.get.mockResolvedValue(envelope({ id: 3 }))
    await fetchRunDetail(3)
    expect(mocked.get).toHaveBeenCalledWith('/model-degradation/runs/3')

    mocked.get.mockClear()
    mocked.get.mockResolvedValue(envelope({}))
    await fetchStats()
    expect(mocked.get).toHaveBeenCalledWith('/model-degradation/stats', { params: {} })

    mocked.get.mockClear()
    await fetchStats(true)
    expect(mocked.get).toHaveBeenCalledWith('/model-degradation/stats', { params: { mine: 1 } })
  })

  it('fetchStats 缺字段时回退成零值而不是 undefined', async () => {
    mocked.get.mockResolvedValue(envelope({}))
    const stats = await fetchStats()
    expect(stats).toEqual({ pass_rate: 0, last_duration_ms: 0, run_count: 0 })
  })

  it('cancelRun 正常返回时不抛错', async () => {
    mocked.post.mockResolvedValue(envelope({ ok: true }))
    await expect(cancelRun(5)).resolves.toBeUndefined()
    expect(mocked.post).toHaveBeenCalledWith('/model-degradation/runs/5/cancel')
  })

  it('cancelRun 把 409 当成成功：检测已经结束了，中止不再是错误', async () => {
    const alreadyEnded = {
      isAxiosError: true,
      response: { status: 409, data: { code: 409, message: 'run is not running' } }
    }
    mocked.post.mockRejectedValue(alreadyEnded)
    await expect(cancelRun(5)).resolves.toBeUndefined()
  })

  it('cancelRun 其它错误照常抛出，不能悄悄吞掉真正的失败', async () => {
    mocked.post.mockRejectedValue({ isAxiosError: true, response: { status: 500 } })
    await expect(cancelRun(5)).rejects.toBeTruthy()
  })

  it('setGroupHidden / setHostHidden 发出正确的 PUT', async () => {
    mocked.put.mockResolvedValue(envelope({}))
    await setGroupHidden(9, true)
    expect(mocked.put).toHaveBeenCalledWith('/admin/model-degradation/history-visibility/groups/9', {
      hidden: true
    })

    mocked.put.mockClear()
    await setHostHidden('a.example.com', false)
    expect(mocked.put).toHaveBeenCalledWith('/admin/model-degradation/history-visibility/hosts', {
      host: 'a.example.com',
      hidden: false
    })
  })
})
