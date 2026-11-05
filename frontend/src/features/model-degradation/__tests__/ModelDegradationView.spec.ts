import { expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ModelDegradationView from '@/views/user/ModelDegradationView.vue'
import RunForm from '../components/RunForm.vue'

const api = vi.hoisted(() => ({
  cancelRun: vi.fn(), fetchRunDetail: vi.fn(), fetchModels: vi.fn(),
  fetchRuns: vi.fn(async () => ({ items: [], total: 0 })),
  fetchStats: vi.fn(async () => ({ pass_rate: 0, last_duration_ms: 0, run_count: 0 })),
  fetchTaskBank: vi.fn(async () => ({ logic: [], render: [] })),
  startRun: vi.fn(async (_payload: unknown) => ({ id: 1, status: 'completed', duration_ms: 1000 }))
}))
vi.mock('../api', () => api)
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<div><slot /></div>' } }))

it('成功提交后更换题目和协议仍保留本页 API Key，下一次提交无需重填', async () => {
  const wrapper = mount(ModelDegradationView, { global: {
    plugins: [createI18n({ legacy: false, locale: 'zh', missingWarn: false, fallbackWarn: false, messages: { zh: {} } })],
    stubs: { RunForm: true, ResultPanel: true, HistoryWall: true, MonitorBars: true, WaveBackdrop: true }
  } })
  await flushPromises()
  const form = wrapper.findComponent(RunForm)
  const state = form.props('state')
  Object.assign(state, { baseUrl: 'https://example.com', apiKey: 'test-key', modelId: 'test-model',
    questions: [{ uid: '1', taskType: 'logic', key: 'logic', prompt: 'p', expected: '21', fromBank: true }] })
  form.vm.$emit('submit')
  await flushPromises()
  expect(state.apiKey).toBe('test-key')
  form.vm.$emit('change-protocol', 'responses')
  form.vm.$emit('add-bank', { task_type: 'render', key: 'render', prompt: 'svg', expected: '', title: 'Render' })
  form.vm.$emit('submit')
  await flushPromises()
  expect(api.startRun).toHaveBeenCalledTimes(2)
  expect(api.startRun.mock.calls[1]?.[0]).toMatchObject({ api_key: 'test-key', protocol: 'responses' })
  wrapper.unmount()
})
