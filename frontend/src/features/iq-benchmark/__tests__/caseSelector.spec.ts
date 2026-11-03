import { describe, expect, it } from 'vitest'
import { caseGroupOf, isScorable, splitCasesByGroup } from '../caseSelector'
import type { IQPublicCase } from '../types'

function kase(over: Partial<IQPublicCase> = {}): IQPublicCase {
  return {
    id: 1,
    name: 'p',
    kind: 'text',
    expect_kind: 'text',
    prompt: 'q',
    allow_user_test: true,
    ...over,
  } as IQPublicCase
}

describe('caseGroupOf', () => {
  it('image 类型的题归为 render', () => {
    expect(caseGroupOf(kase({ kind: 'image', expect_kind: 'image_url' }))).toBe('render')
  })

  it('期望 image_url 的文本题也归为 render', () => {
    // 这条是分组键存在的理由：kind 是 text 但产出是图，
    // 只看 kind 会把它错分到 logic，然后按文本比对去判分 ——
    // 结果是永远判不通过。
    expect(caseGroupOf(kase({ kind: 'text', expect_kind: 'image_url' }))).toBe('render')
  })

  it('期望 svg 的题归为 render', () => {
    expect(caseGroupOf(kase({ kind: 'code', expect_kind: 'svg' }))).toBe('render')
  })

  it('期望 markdown 的题归为 logic', () => {
    expect(caseGroupOf(kase({ kind: 'text', expect_kind: 'markdown' }))).toBe('logic')
  })

  it('普通文本题归为 logic', () => {
    expect(caseGroupOf(kase({ kind: 'text', expect_kind: 'text' }))).toBe('logic')
  })

  it('代码题归为 logic', () => {
    expect(caseGroupOf(kase({ kind: 'code', expect_kind: 'text' }))).toBe('logic')
  })
})

describe('splitCasesByGroup', () => {
  it('保持组内原有顺序', () => {
    // 组内顺序就是后端给的 sort_order，重排会让管理员的排序失效。
    const cases = [
      kase({ id: 1, kind: 'text', expect_kind: 'text' }),
      kase({ id: 2, kind: 'image', expect_kind: 'image_url' }),
      kase({ id: 3, kind: 'code', expect_kind: 'markdown' }),
      kase({ id: 4, kind: 'text', expect_kind: 'svg' }),
    ]
    const { logic, render } = splitCasesByGroup(cases)
    expect(logic.map((c) => c.id)).toEqual([1, 3])
    expect(render.map((c) => c.id)).toEqual([2, 4])
  })

  it('空输入返回两个空数组而不是 undefined', () => {
    const { logic, render } = splitCasesByGroup([])
    expect(logic).toEqual([])
    expect(render).toEqual([])
  })

  it('全是 render 时 logic 为空', () => {
    const { logic, render } = splitCasesByGroup([
      kase({ id: 1, kind: 'image', expect_kind: 'image_url' }),
    ])
    expect(logic).toHaveLength(0)
    expect(render).toHaveLength(1)
  })

  it('不丢题：两组数量之和等于输入数量', () => {
    const cases = [
      kase({ id: 1, kind: 'text', expect_kind: 'text' }),
      kase({ id: 2, kind: 'image', expect_kind: 'image_url' }),
      kase({ id: 3, kind: 'code', expect_kind: 'svg' }),
    ]
    const { logic, render } = splitCasesByGroup(cases)
    expect(logic.length + render.length).toBe(cases.length)
  })
})

describe('isScorable', () => {
  it('logic 组可判分', () => {
    expect(isScorable(kase({ kind: 'text', expect_kind: 'text' }))).toBe(true)
  })

  it('render 组不可判分', () => {
    // 图片题没有文本参考答案，verdict 恒为 generated。
    expect(isScorable(kase({ kind: 'image', expect_kind: 'image_url' }))).toBe(false)
    expect(isScorable(kase({ kind: 'text', expect_kind: 'svg' }))).toBe(false)
  })
})
