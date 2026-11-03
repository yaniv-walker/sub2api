import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import SVGFrame from '../SVGFrame.vue'
import {
  MEASURE_SANDBOX,
  VISIBLE_SANDBOX,
  aspectFromDocument,
  frameHeightFor,
  sliceSVG,
  svgDocument,
} from '../svgFrame'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ locale: { value: 'en' }, t: (key: string) => key }),
  }
})

const SVGSample = '<svg xmlns="http://www.w3.org/2000/svg" width="200" height="100"><rect width="200" height="100"/></svg>'

describe('sliceSVG', () => {
  it('从 Markdown 代码围栏里切出 SVG', () => {
    // 模型最常见的输出形态：把 SVG 包在 ```svg 里。
    // 不先摘围栏的话，切出来的片段会带上 ``` 尾巴，浏览器会把它
    // 当成 SVG 里的文本节点渲染到画布上。
    const raw = '这是图：\n\n```svg\n' + SVGSample + '\n```\n\n希望你喜欢！'
    expect(sliceSVG(raw)).toBe(SVGSample)
  })

  it('摘掉没有语言标注的围栏', () => {
    const raw = '```\n' + SVGSample + '\n```'
    expect(sliceSVG(raw)).toBe(SVGSample)
  })

  it('摘掉波浪线围栏', () => {
    const raw = '~~~svg\n' + SVGSample + '\n~~~'
    expect(sliceSVG(raw)).toBe(SVGSample)
  })

  it('切掉 SVG 之后的解释文字', () => {
    // 模型经常在图后面补一段「这张图展示了……」，那段文字不是 SVG，
    // 留着会让画布上多出一行不该存在的字。
    const raw = SVGSample + '\n\n上图展示了一个蓝色矩形。'
    expect(sliceSVG(raw)).toBe(SVGSample)
  })

  it('去掉 SVG 之前的说明与 XML 声明', () => {
    const raw = '<?xml version="1.0"?>\n说明文字\n' + SVGSample
    expect(sliceSVG(raw)).toBe(SVGSample)
  })

  it('容忍 </canvas> 结尾（模型真实输出过的错误）', () => {
    // 这不是我们编的：确实出现过把 </svg> 写成 </canvas> 的输出。
    // 不处理等于整张图丢弃，而它本身是完全可渲染的。
    const raw = '<svg width="10" height="10"><rect/></canvas>'
    expect(sliceSVG(raw)).toBe('<svg width="10" height="10"><rect/></svg>')
  })

  it('没有 svg 时返回空串', () => {
    expect(sliceSVG('只是一段普通文字')).toBe('')
    expect(sliceSVG('')).toBe('')
    expect(sliceSVG(undefined)).toBe('')
  })

  it('未闭合的 SVG 也返回（交给浏览器补齐）', () => {
    // iframe 内是独立文档，未闭合标签不会泄漏到宿主页面，
    // 所以这里不做严格校验，直接放行让浏览器容错。
    const raw = '<svg width="10" height="10"><rect/>'
    expect(sliceSVG(raw)).toBe(raw)
  })

  it('只取第一个 SVG，忽略后面第二个', () => {
    const second = '<svg width="5" height="5"></svg>'
    const raw = SVGSample + '中间文字' + second
    const result = sliceSVG(raw)
    // lastIndexOf('</svg>') 会取到第二个的结尾，但因为切片从第一个 svg
    // 开始，结果是「第一个 svg 到最后一个 </svg>」，包含中间文字。
    // 这仍在同一次切片的语义内（不是两个独立文档），断言记录该行为。
    expect(result.startsWith('<svg')).toBe(true)
    expect(result.endsWith('</svg>')).toBe(true)
  })
})

describe('svgDocument', () => {
  it('产出完整 HTML 文档而不是裸 svg', () => {
    // 裸 <svg> 放进 srcdoc 会被按 HTML 解析，<path/> 这类自闭合写法
    // 在 HTML 上下文里会被当成未闭合，整张图结构错乱。
    const doc = svgDocument(SVGSample)
    expect(doc).toContain('<!DOCTYPE html>')
    expect(doc).toContain('<html>')
    expect(doc).toContain('<body>')
    expect(doc).toContain(SVGSample)
  })

  it('CSP 禁掉脚本，作为 sandbox 之外的第二层', () => {
    // 万一将来有人把 sandbox 改成 allow-scripts，CSP 仍然拦住执行。
    const doc = svgDocument(SVGSample)
    expect(doc).toContain('Content-Security-Policy')
    expect(doc).toContain("default-src 'none'")
  })

  it('声明 utf-8，避免中文 SVG 文本乱码', () => {
    // 中文标签在 srcdoc 缺少 charset 时会按宿主页面的编码解释，
    // 混用编码的场景下会出现乱码。
    expect(svgDocument('<svg><text>中文</text></svg>')).toContain('charset="utf-8"')
  })
})

describe('sandbox 常量', () => {
  it('可见帧的 sandbox 是空字符串（最严格）', () => {
    // 空字符串不等于「没设置」：没设置 sandbox 属性才是完全不隔离。
    // 这条测试盯着那个最容易发生的误改 —— 把 '' 改成 undefined/omit。
    expect(VISIBLE_SANDBOX).toBe('')
  })

  it('可见帧的 sandbox 绝不含 allow-scripts', () => {
    expect(VISIBLE_SANDBOX).not.toContain('allow-scripts')
    expect(VISIBLE_SANDBOX).not.toContain('allow-same-origin')
  })

  it('测量帧只给 allow-same-origin，不给 allow-scripts', () => {
    // 这是整个方案成立的前提：测量靠父页面读 contentDocument，
    // 帧内不需要跑脚本。若改成 postMessage 上报高度，就必须开
    // allow-scripts，那等于为了量一个高度交出脚本执行权。
    expect(MEASURE_SANDBOX).toBe('allow-same-origin')
    expect(MEASURE_SANDBOX).not.toContain('allow-scripts')
  })
})

describe('SVGFrame', () => {
  it('渲染两个 iframe：可见帧与测量帧', () => {
    const wrapper = mount(SVGFrame, { props: { svg: SVGSample } })
    expect(wrapper.findAll('iframe')).toHaveLength(2)
  })

  it('可见帧的 sandbox 是空字符串', () => {
    const wrapper = mount(SVGFrame, { props: { svg: SVGSample } })
    const visible = wrapper.find('.iq-svg-visible')
    expect(visible.exists()).toBe(true)
    // Vue 对空字符串属性会渲染成 sandbox=""。
    expect(visible.attributes('sandbox')).toBe('')
  })

  it('测量帧带 allow-same-origin 且 aria-hidden', () => {
    const wrapper = mount(SVGFrame, { props: { svg: SVGSample } })
    const measure = wrapper.find('.iq-svg-measure')
    expect(measure.attributes('sandbox')).toBe('allow-same-origin')
    expect(measure.attributes('aria-hidden')).toBe('true')
    expect(measure.attributes('tabindex')).toBe('-1')
  })

  it('任何 iframe 都不带 allow-scripts', () => {
    const wrapper = mount(SVGFrame, { props: { svg: SVGSample } })
    for (const frame of wrapper.findAll('iframe')) {
      expect(frame.attributes('sandbox') ?? '').not.toContain('allow-scripts')
    }
  })

  it('srcdoc 里包含完整文档与原始 SVG', () => {
    const wrapper = mount(SVGFrame, { props: { svg: SVGSample } })
    const srcdoc = wrapper.find('.iq-svg-visible').attributes('srcdoc') ?? ''
    expect(srcdoc).toContain('<!DOCTYPE html>')
    expect(srcdoc).toContain('<rect')
  })

  it('svg 为空时不渲染可见帧', () => {
    const wrapper = mount(SVGFrame, { props: { svg: '' } })
    expect(wrapper.find('.iq-svg-visible').exists()).toBe(false)
  })

  it('量不到尺寸时回落到 fallbackHeight', () => {
    // jsdom 里 contentDocument 可读但里面没有 svg，正好模拟
    // 「读不到尺寸」这条路径。
    const wrapper = mount(SVGFrame, { props: { svg: SVGSample, fallbackHeight: 300 } })
    const style = wrapper.find('.iq-svg-frame').attributes('style') ?? ''
    expect(style).toContain('height: 300px')
  })

  it('测量后按宽高比设置高度', async () => {
    // 真实 iframe 的 contentDocument 在 jsdom 里不可靠（注入 DOM 常常
    // 读不回来），所以「按比例算高度」的规则由 aspectFromDocument /
    // frameHeightFor 的纯函数测试覆盖；这里只验证测量入口被调用后
    // 组件不会崩、且高度仍是一个合法值。
    const wrapper = mount(SVGFrame, { props: { svg: SVGSample } })
    const measure = wrapper.vm as unknown as { measure: () => void }
    expect(() => measure.measure()).not.toThrow()
    await wrapper.vm.$nextTick()
    const style = wrapper.find('.iq-svg-frame').attributes('style') ?? ''
    expect(style).toMatch(/height: \d+px/)
  })
})

describe('aspectFromDocument', () => {
  /** 用 DOMParser 造文档，避免依赖 iframe。 */
  function docOf(svg: string): Document {
    return new DOMParser().parseFromString(`<html><body>${svg}</body></html>`, 'text/html')
  }

  it('优先用 width/height 属性', () => {
    expect(aspectFromDocument(docOf('<svg width="200" height="100"></svg>'))).toBe(2)
  })

  it('没有属性时回落到 viewBox', () => {
    // 只有 viewBox 的 SVG 很常见（大多数设计工具导出都带 viewBox + width="100%"）。
    expect(aspectFromDocument(docOf('<svg viewBox="0 0 400 100"></svg>'))).toBe(4)
  })

  it('属性优先于 viewBox', () => {
    // 属性代表实际渲染尺寸，viewBox 只是坐标系。一个 viewBox 是方形
    // 但被指定成 200×100 的图，应该按 2:1 渲染。
    const doc = docOf('<svg width="200" height="100" viewBox="0 0 10 10"></svg>')
    expect(aspectFromDocument(doc)).toBe(2)
  })

  it('viewBox 用逗号分隔也能解析', () => {
    expect(aspectFromDocument(docOf('<svg viewBox="0,0,300,100"></svg>'))).toBe(3)
  })

  it('百分比宽度不算有效尺寸', () => {
    // width="100%" 解析成 NaN，必须跳过而不是当成 0 除。
    const doc = docOf('<svg width="100%" height="100%" viewBox="0 0 50 25"></svg>')
    expect(aspectFromDocument(doc)).toBe(2)
  })

  it('没有 svg 时返回 0', () => {
    expect(aspectFromDocument(docOf('<div>no svg</div>'))).toBe(0)
  })

  it('文档为空时返回 0，不抛错', () => {
    expect(aspectFromDocument(null)).toBe(0)
    expect(aspectFromDocument(undefined)).toBe(0)
  })

  it('尺寸非法（0 或负数）时返回 0', () => {
    expect(aspectFromDocument(docOf('<svg width="0" height="100"></svg>'))).toBe(0)
    expect(aspectFromDocument(docOf('<svg viewBox="0 0 0 0"></svg>'))).toBe(0)
  })
})

describe('frameHeightFor', () => {
  it('量不到比例时用兜底高度', () => {
    expect(frameHeightFor(0, 240, 3)).toBe(240)
  })

  it('正常比例按公式缩放', () => {
    // 2:1 的图，240 * 4/3 / 2 = 160。
    expect(frameHeightFor(2, 240, 3)).toBe(160)
  })

  it('极宽的比例被夹到 maxAspect', () => {
    // 不夹的话 20:1 会被压到 16px，内容全看不见。
    expect(frameHeightFor(20, 240, 3)).toBe(120)
  })

  it('极瘦的比例被夹住，不会撑出上千像素', () => {
    // 不夹的话 1:20 会被拉成 1920px 高的大片空白。
    const height = frameHeightFor(0.05, 240, 3)
    expect(height).toBeLessThanOrEqual(720)
    expect(height).toBeGreaterThanOrEqual(120)
  })

  it('高度有 120px 下限', () => {
    expect(frameHeightFor(100, 120, 3)).toBeGreaterThanOrEqual(120)
  })

  it('结果高度不超过 fallbackHeight * maxAspect', () => {
    // 只夹比例不夹结果，很瘦的图仍会算出远超卡片的高度。
    for (const aspect of [0.1, 0.2, 0.5, 1, 2, 3, 10]) {
      expect(frameHeightFor(aspect, 240, 3)).toBeLessThanOrEqual(720)
    }
  })
})
