import { describe, expect, it } from 'vitest'
import {
  escapeHTML,
  isUsableSVG,
  looksLikeSVG,
  markdownToSafeHTML,
  resolveRenderKind,
  sanitizeHTML,
  sanitizeSVG,
  truncateText,
} from '../sanitize'

/**
 * 这一组测试的目的不是「功能对不对」，而是「危险内容会不会被执行」。
 * 每个用例都对应一种真实的 XSS 载体。
 */
describe('sanitizeSVG', () => {
  it('strips <script> from SVG', () => {
    const cleaned = sanitizeSVG('<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script><rect /></svg>')
    expect(cleaned).not.toContain('<script')
    expect(cleaned).not.toContain('alert(1)')
    expect(cleaned).toContain('<rect')
  })

  it('strips on* event handler attributes', () => {
    const cleaned = sanitizeSVG('<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><rect onerror="alert(2)" /></svg>')
    expect(cleaned).not.toContain('onload')
    expect(cleaned).not.toContain('onerror')
    expect(cleaned).not.toContain('alert')
  })

  it('strips <foreignObject> which can smuggle HTML', () => {
    const cleaned = sanitizeSVG(
      '<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><body xmlns="http://www.w3.org/1999/xhtml"><img src=x onerror="alert(1)"/></body></foreignObject></svg>',
    )
    expect(cleaned.toLowerCase()).not.toContain('foreignobject')
    expect(cleaned).not.toContain('onerror')
  })

  it('strips external references (href / xlink:href / src)', () => {
    const cleaned = sanitizeSVG(
      '<svg xmlns="http://www.w3.org/2000/svg"><use href="https://evil.example/x.svg#a"/><image src="https://evil.example/y.png"/></svg>',
    )
    expect(cleaned).not.toContain('evil.example')
    expect(cleaned).not.toContain('href')
  })

  it('strips javascript: URLs even inside allowed attributes', () => {
    const cleaned = sanitizeSVG('<svg xmlns="http://www.w3.org/2000/svg"><a href="javascript:alert(1)"><text>x</text></a></svg>')
    expect(cleaned.toLowerCase()).not.toContain('javascript:')
    expect(cleaned).not.toContain('<a')
  })

  it('keeps harmless drawing elements and attributes', () => {
    const cleaned = sanitizeSVG(
      '<svg xmlns="http://www.w3.org/2000/svg" width="100" height="100" viewBox="0 0 100 100"><circle cx="50" cy="50" r="20" fill="red"/><text x="5" y="95">hi</text></svg>',
    )
    expect(cleaned).toContain('<circle')
    expect(cleaned).toContain('fill="red"')
    expect(cleaned).toContain('<text')
    expect(cleaned).toContain('hi')
  })

  it('returns an empty string for empty or undefined input', () => {
    expect(sanitizeSVG('')).toBe('')
    expect(sanitizeSVG(undefined)).toBe('')
  })

  it('leaves no script element or event handler after real DOM parsing', () => {
    const dirty =
      '<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)">' +
      '<script>alert(2)</script>' +
      '<rect onerror="alert(3)"/>' +
      '<foreignObject><body xmlns="http://www.w3.org/1999/xhtml"><img src=x onerror="alert(4)"/></body></foreignObject>' +
      '<a href="javascript:alert(5)"><text>x</text></a>' +
      '</svg>'
    const host = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
    host.innerHTML = sanitizeSVG(dirty)

    expect(host.querySelector('script')).toBeNull()
    expect(host.querySelector('foreignObject')).toBeNull()
    expect(host.querySelector('a')).toBeNull()
    expect(host.querySelector('image')).toBeNull()
    for (const el of Array.from(host.querySelectorAll('*'))) {
      for (const attr of Array.from(el.attributes)) {
        expect(attr.name.toLowerCase().startsWith('on')).toBe(false)
        expect(attr.value.toLowerCase()).not.toContain('javascript:')
      }
    }
  })
})

describe('isUsableSVG', () => {
  it('accepts a real svg root', () => {
    expect(isUsableSVG('<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>')).toBe(true)
  })


  it('rejects non-svg content', () => {
    expect(isUsableSVG('just text')).toBe(false)
    expect(isUsableSVG('')).toBe(false)
    expect(isUsableSVG(undefined)).toBe(false)
  })
})

// svgToDataURI 已随 <img> 方案一并移除（SVG 改走 sandbox="" 的 iframe）。
// 这里保留一条「它不该回来」的守卫：data URI 方案的问题是它对 SVG 的
// 样式与高度完全没有控制力，且一旦有人重新用它，就等于绕开了新加的
// sandbox 隔离层 —— 而两套方案并存时最容易发生的就是这种回退。
describe('svgToDataURI removal', () => {
  it('is no longer exported from sanitize.ts', async () => {
    const mod = await import('../sanitize')
    expect('svgToDataURI' in mod).toBe(false)
  })
})

describe('sanitizeHTML', () => {
  it('removes scripts and inline handlers', () => {
    const cleaned = sanitizeHTML('<p onclick="alert(1)">hi</p><script>alert(2)</script>')
    expect(cleaned).not.toContain('script')
    expect(cleaned).not.toContain('onclick')
    expect(cleaned).toContain('hi')
  })

  it('removes iframe/object/embed/form/input', () => {
    const cleaned = sanitizeHTML(
      '<iframe src="https://evil.example"></iframe><object></object><embed><form><input></form>',
    )
    expect(cleaned.toLowerCase()).not.toContain('iframe')
    expect(cleaned.toLowerCase()).not.toContain('object')
    expect(cleaned.toLowerCase()).not.toContain('embed')
    expect(cleaned.toLowerCase()).not.toContain('form')
    expect(cleaned.toLowerCase()).not.toContain('input')
  })

  it('strips style attributes (CSS injection / overlay attacks)', () => {
    const cleaned = sanitizeHTML('<div style="position:fixed;top:0;left:0">x</div>')
    expect(cleaned).not.toContain('style=')
  })

  it('returns an empty string for empty input', () => {
    expect(sanitizeHTML('')).toBe('')
    expect(sanitizeHTML(undefined)).toBe('')
  })
})

describe('markdownToSafeHTML', () => {
  it('escapes raw HTML before applying markdown', () => {
    // 先转义再套 markdown：因此 <script> 只能变成字面文本。
    const html = markdownToSafeHTML('<script>alert(1)</script>')
    expect(html).not.toContain('<script')
    expect(html).toContain('&lt;script&gt;')
  })

  it('escapes an img onerror payload into inert text', () => {
    const html = markdownToSafeHTML('<img src=x onerror="alert(1)">')
    // 关键点是它变成了**文本**而不是可执行元素：
    // 标签与引号都被转义，因此浏览器不会解析出 img 元素，也就没有 onerror。
    expect(html).not.toContain('<img')
    expect(html).toContain('&lt;img')
    expect(html).toContain('&quot;')

    // 用真实 DOM 验证：解析后不存在 img 元素，也没有任何 on* 处理器属性。
    const host = document.createElement('div')
    host.innerHTML = html
    expect(host.querySelector('img')).toBeNull()
    expect(host.querySelector('script')).toBeNull()
    for (const el of Array.from(host.querySelectorAll('*'))) {
      for (const attr of Array.from(el.attributes)) {
        expect(attr.name.toLowerCase().startsWith('on')).toBe(false)
      }
    }
  })

  it('renders fenced code blocks as <pre><code>', () => {
    const html = markdownToSafeHTML('```go\nfmt.Println("hi")\n```')
    expect(html).toContain('<pre')
    expect(html).toContain('<code>')
    expect(html).toContain('fmt.Println')
    expect(html).toContain('data-lang="go"')
  })

  it('keeps code inside a fence escaped even if it looks like markup', () => {
    const html = markdownToSafeHTML('```\n<script>alert(1)</script>\n```')
    expect(html).not.toContain('<script')
    expect(html).toContain('&lt;script&gt;')
  })

  it('renders inline code and bold', () => {
    const html = markdownToSafeHTML('use `npm i` and **careful**')
    expect(html).toContain('<code>npm i</code>')
    expect(html).toContain('<strong>careful</strong>')
  })

  it('converts newlines to <br>', () => {
    expect(markdownToSafeHTML('a\nb')).toContain('<br>')
  })

  it('returns an empty string for empty input', () => {
    expect(markdownToSafeHTML('')).toBe('')
    expect(markdownToSafeHTML(undefined)).toBe('')
  })
})

describe('looksLikeSVG', () => {
  it('detects an svg root, with or without an xml prolog', () => {
    expect(looksLikeSVG('<svg><rect/></svg>')).toBe(true)
    expect(looksLikeSVG('<?xml version="1.0"?>\n<svg><rect/></svg>')).toBe(true)
    expect(looksLikeSVG('   <svg viewBox="0 0 1 1"></svg>')).toBe(true)
  })

  it('rejects text, markdown and empty input', () => {
    expect(looksLikeSVG('# Title')).toBe(false)
    expect(looksLikeSVG('plain')).toBe(false)
    expect(looksLikeSVG('')).toBe(false)
    expect(looksLikeSVG(undefined)).toBe(false)
  })
})

describe('resolveRenderKind', () => {
  it('keeps image_url only when a URL is actually present', () => {
    expect(resolveRenderKind('image_url', undefined, 'https://cdn/x.png')).toBe('image_url')
    // 声明是 image_url 但没有 URL：退化成 text，避免渲染一个空图框。
    expect(resolveRenderKind('image_url', undefined, undefined)).toBe('text')
  })

  it('upgrades text content that is really an SVG', () => {
    expect(resolveRenderKind('text', '<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>', undefined)).toBe('svg')
  })

  it('keeps markdown as markdown', () => {
    expect(resolveRenderKind('markdown', '# hi', undefined)).toBe('markdown')
  })

  it('keeps plain text as text', () => {
    expect(resolveRenderKind('text', 'hello', undefined)).toBe('text')
  })

  it('does not upgrade to svg when the svg is unusable', () => {
    // 只有被完全剥离的内容不该被当作 svg 渲染。
    expect(resolveRenderKind('text', '<svg', undefined)).toBe('text')
  })
})

describe('escapeHTML', () => {
  it('escapes every HTML-significant character', () => {
    expect(escapeHTML('<a href="x">&\'</a>')).toBe(
      '&lt;a href=&quot;x&quot;&gt;&amp;&#39;&lt;/a&gt;',
    )
  })

  it('escapes the ampersand first so entities are not double-decoded', () => {
    expect(escapeHTML('&lt;')).toBe('&amp;lt;')
  })
})

describe('truncateText', () => {
  it('returns the original when it fits', () => {
    expect(truncateText('short', 10)).toBe('short')
  })

  it('appends an ellipsis when truncating', () => {
    const out = truncateText('abcdefghij', 5)
    expect(out).toBe('abcde…')
    expect(out.length).toBe(6)
  })

  it('uses a 600-character default', () => {
    const long = 'x'.repeat(1000)
    expect(truncateText(long).length).toBe(601)
  })

  it('returns an empty string for empty input', () => {
    expect(truncateText('')).toBe('')
    expect(truncateText(undefined)).toBe('')
  })
})
