import { describe, expect, it } from 'vitest'
import { bankTaskToSelection, nextUid, useRunForm } from '../useRunForm'
import type { MDTask } from '../types'

const logicTask: MDTask = {
  task_type: 'logic',
  key: 'candy-shape-flavor',
  title: '糖果摸取',
  prompt: '袋子里有……回答取到某口味糖果的数量',
  expected: '21'
}

const renderTask: MDTask = {
  task_type: 'render',
  key: 'pelican-bicycle',
  title: '鹈鹕骑自行车',
  prompt: '生成一张鹈鹕骑自行车的 SVG。本次场景：{scene}，校验码 {code}',
  expected: ''
}

/** 把表单填成合法状态。 */
function fillValid(form: ReturnType<typeof useRunForm>) {
  form.state.baseUrl = 'https://api.example.com/v1'
  form.state.apiKey = 'sk-test'
  form.state.modelId = 'gpt-4o'
  form.addFromBank(logicTask)
}

describe('useRunForm 校验顺序', () => {
  it('全空时先报地址', () => {
    const form = useRunForm()
    expect(form.validate()).toBe('needBaseUrl')
  })

  it('有地址缺密钥时报密钥', () => {
    const form = useRunForm()
    form.state.baseUrl = 'https://api.example.com'
    expect(form.validate()).toBe('needApiKey')
  })

  it('有地址密钥缺模型时报模型', () => {
    const form = useRunForm()
    form.state.baseUrl = 'https://api.example.com'
    form.state.apiKey = 'sk'
    expect(form.validate()).toBe('needModel')
  })

  it('有地址密钥模型但没选题时报题目', () => {
    const form = useRunForm()
    form.state.baseUrl = 'https://api.example.com'
    form.state.apiKey = 'sk'
    form.state.modelId = 'gpt-4o'
    expect(form.validate()).toBe('needTask')
  })

  it('纯逻辑题不校验生成超时', () => {
    const form = useRunForm()
    fillValid(form)
    form.state.renderTimeoutMinutes = 999
    expect(form.validate()).toBe('')
  })

  it('含生成题时超时越界会被拦下', () => {
    const form = useRunForm()
    fillValid(form)
    form.addFromBank(renderTask)
    form.state.renderTimeoutMinutes = 0
    expect(form.validate()).toBe('needRenderTimeout')

    form.state.renderTimeoutMinutes = 61
    expect(form.validate()).toBe('needRenderTimeout')

    form.state.renderTimeoutMinutes = 60
    expect(form.validate()).toBe('')
  })

  it('自定义题缺题干时报题目', () => {
    const form = useRunForm()
    fillValid(form)
    form.addCustomLogic()
    expect(form.validate()).toBe('needQuestion')
  })

  it('自定义逻辑题缺期望答案时报期望', () => {
    const form = useRunForm()
    fillValid(form)
    form.addCustomLogic()
    form.state.questions[form.state.questions.length - 1].prompt = '1+1=?'
    expect(form.validate()).toBe('needCustomExpected')
  })

  it('自定义逻辑题填全后通过', () => {
    const form = useRunForm()
    fillValid(form)
    form.addCustomLogic()
    const custom = form.state.questions[form.state.questions.length - 1]
    custom.prompt = '1+1=?'
    custom.expected = '2'
    expect(form.validate()).toBe('')
  })

  it('空白字符不算填写内容', () => {
    const form = useRunForm()
    form.state.baseUrl = '   '
    form.state.apiKey = '  '
    form.state.modelId = '  '
    expect(form.validate()).toBe('needBaseUrl')
  })
})

describe('useRunForm 提交体', () => {
  it('渲染超时按分钟换算成秒', () => {
    const form = useRunForm()
    fillValid(form)
    form.addFromBank(renderTask)
    form.state.renderTimeoutMinutes = 20
    expect(form.toPayload().render_timeout_seconds).toBe(1200)
  })

  it('纯逻辑题不带渲染超时字段', () => {
    const form = useRunForm()
    fillValid(form)
    expect(form.toPayload().render_timeout_seconds).toBeUndefined()
  })

  it('tasks 只包含实际选中的题型', () => {
    const form = useRunForm()
    fillValid(form)
    expect(form.toPayload().tasks).toEqual(['logic'])

    form.addFromBank(renderTask)
    expect(form.toPayload().tasks.sort()).toEqual(['logic', 'render'])
  })

  it('messages 协议下不发送思考等级', () => {
    const form = useRunForm()
    fillValid(form)
    form.state.reasoningEffort = 'high'
    form.setProtocol('messages')
    expect(form.state.reasoningEffort).toBe('')
    expect(form.toPayload().reasoning_effort).toBeUndefined()
  })

  it('chat_completions 协议保留思考等级', () => {
    const form = useRunForm()
    fillValid(form)
    form.state.reasoningEffort = 'high'
    expect(form.toPayload().reasoning_effort).toBe('high')
  })

  it('提交体里题干与期望答案去掉首尾空白', () => {
    const form = useRunForm()
    fillValid(form)
    form.addCustomLogic()
    const custom = form.state.questions[form.state.questions.length - 1]
    custom.prompt = '  1+1=?  '
    custom.expected = '  2  '
    const payload = form.toPayload()
    const last = payload.questions[payload.questions.length - 1]
    expect(last.prompt).toBe('1+1=?')
    expect(last.expected).toBe('2')
  })
})

describe('useRunForm 题目管理', () => {
  it('同一道题库题不会重复加入', () => {
    const form = useRunForm()
    form.addFromBank(logicTask)
    form.addFromBank(logicTask)
    expect(form.state.questions).toHaveLength(1)
  })

  it('移除按 uid 精确删除', () => {
    const form = useRunForm()
    form.addFromBank(logicTask)
    form.addFromBank(renderTask)
    const uid = form.state.questions[0].uid
    form.remove(uid)
    expect(form.state.questions).toHaveLength(1)
    expect(form.state.questions[0].key).toBe('pelican-bicycle')
  })

  it('移除不存在的 uid 不报错也不改状态', () => {
    const form = useRunForm()
    form.addFromBank(logicTask)
    form.remove('does-not-exist')
    expect(form.state.questions).toHaveLength(1)
  })

  it('reset 清掉密钥与题目但保留协议', () => {
    const form = useRunForm()
    fillValid(form)
    form.setProtocol('responses')
    form.reset()
    expect(form.state.apiKey).toBe('')
    expect(form.state.questions).toEqual([])
    expect(form.state.protocol).toBe('responses')
  })

  it('nextUid 连续调用不重复', () => {
    const seen = new Set<string>()
    for (let i = 0; i < 200; i += 1) {
      const uid = nextUid()
      expect(seen.has(uid)).toBe(false)
      seen.add(uid)
    }
  })

  it('bankTaskToSelection 保留题库原文并标记来源', () => {
    const sel = bankTaskToSelection(logicTask)
    expect(sel.fromBank).toBe(true)
    expect(sel.key).toBe('candy-shape-flavor')
    expect(sel.prompt).toBe(logicTask.prompt)
    expect(sel.expected).toBe('21')
  })
})

describe('useRunForm 派生状态', () => {
  it('effortAvailable 随协议变化', () => {
    const form = useRunForm()
    expect(form.effortAvailable.value).toBe(true)

    form.setProtocol('responses')
    expect(form.effortAvailable.value).toBe(true)

    form.setProtocol('messages')
    expect(form.effortAvailable.value).toBe(false)
  })

  it('hasRender 只在选了生成题时为真', () => {
    const form = useRunForm()
    expect(form.hasRender.value).toBe(false)
    form.addFromBank(renderTask)
    expect(form.hasRender.value).toBe(true)
  })
})
