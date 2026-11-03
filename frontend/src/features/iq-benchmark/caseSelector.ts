/**
 * 题库选择器的分组逻辑。
 *
 * 参考实现把题目分成 logic / render 两组；我们这边的划分依据是
 * `Case.Kind`（image vs text/code）与 `expect_kind`（是否期望图像）。
 * 两者其实说的是同一件事：这道题考的是「能不能画出一张图」还是
 * 「能不能答对一段文本」。
 *
 * 为什么需要分组而不是平铺：两种题的判定口径完全不同 ——
 * 图片题的 verdict 恒为 generated（没有可比的参考答案），
 * 文本题才有 ok/degraded。混在一起列，用户会以为通过率能混着算。
 */
import type { IQPublicCase } from './types'

/** 题目的判分口径。 */
export type IQCaseGroup = 'logic' | 'render'

/**
 * 判定一道题属于哪一组。
 *
 * 依据是 kind 与 expect_kind 的组合，而不是单一字段：
 *   - kind === 'image'             → render（走 /v1/images/generations）
 *   - expect_kind === 'image_url'   → render（文本入口但要图）
 *   - expect_kind === 'svg'         → render（要的是矢量图）
 *   - 其余（text / markdown）        → logic（有文本可判）
 *
 * 只看 kind 会漏掉「用 chat 接口画 SVG」这类题 —— 它们 kind 是 text，
 * 但产出是图，判分方式与图片题一致。
 */
export function caseGroupOf(c: Pick<IQPublicCase, 'kind' | 'expect_kind'>): IQCaseGroup {
  if (c.kind === 'image') return 'render'
  if (c.expect_kind === 'image_url' || c.expect_kind === 'svg') return 'render'
  return 'logic'
}

/** 按组切分题目，保持原有顺序（组内顺序即后端给的 sort_order）。 */
export function splitCasesByGroup(cases: readonly IQPublicCase[]): {
  logic: IQPublicCase[]
  render: IQPublicCase[]
} {
  const logic: IQPublicCase[] = []
  const render: IQPublicCase[] = []
  for (const c of cases) {
    if (caseGroupOf(c) === 'render') render.push(c)
    else logic.push(c)
  }
  return { logic, render }
}

/**
 * 该题能否被判分。
 *
 * 图片题与 svg/ image_url 题天然不可判分（没有文本参考答案）；
 * 其余题如果没有 expected 也不可判分。
 *
 * 注意公开 DTO **不下发** expected（它就是答案本身），所以前端
 * 无法逐题判断，只能按组推断。这与后端 VerdictForCase 的规则一致：
 * 「有没有判定依据」在公开侧本来就只体现为 verdict 的取值。
 */
export function isScorable(c: Pick<IQPublicCase, 'kind' | 'expect_kind'>): boolean {
  return caseGroupOf(c) === 'logic'
}
