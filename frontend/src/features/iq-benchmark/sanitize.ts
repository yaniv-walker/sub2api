/**
 * 作品内容的安全渲染工具。
 *
 * 三层防护，缺一不可：
 *   1. **后端已做**：被判为危险的作品直接标 blocked 且不下发 content；
 *   2. **这里做**：即使 content 带了可疑构造，也在渲染前再剥离一次；
 *   3. **渲染层做**：SVG 只允许通过 <img> 的 data URI 展示（不注入 DOM），
 *      Markdown 经 DOMPurify 清洗，纯文本一律走 textContent。
 *
 * 后端与前端都做不是重复劳动：后端负责「不把危险内容存进响应」，
 * 前端负责「即使拿到了也不执行」。任一层被绕过，另一层仍然兜底。
 */
import DOMPurify from 'dompurify'
import type { IQRenderKind } from './types'

/** SVG 中允许保留的元素白名单（形状/路径/文本/渐变，无脚本无外链）。 */
const SVG_ALLOWED_TAGS = [
  'svg', 'g', 'path', 'rect', 'circle', 'ellipse', 'line', 'polyline', 'polygon',
  'text', 'tspan', 'defs', 'lineargradient', 'radialgradient', 'stop', 'title', 'desc',
  'clippath', 'use', 'symbol', 'marker', 'pattern',
]

/** SVG 中允许保留的属性（不含 on* 事件、不含 xlink:href 之类的外链）。 */
const SVG_ALLOWED_ATTR = [
  'xmlns', 'width', 'height', 'viewbox', 'fill', 'stroke', 'stroke-width',
  'stroke-linecap', 'stroke-linejoin', 'stroke-dasharray', 'opacity', 'transform',
  'x', 'y', 'x1', 'y1', 'x2', 'y2', 'cx', 'cy', 'r', 'rx', 'ry', 'd', 'points',
  'dx', 'dy', 'font-size', 'font-family', 'font-weight', 'text-anchor',
  'dominant-baseline', 'id', 'class', 'offset', 'stop-color', 'stop-opacity',
  'gradientunits', 'gradienttransform', 'preserveaspectratio',
]

/**
 * 清洗 SVG 文本。
 *
 * 返回的字符串**只用于构造 data URI 给 <img>**，不直接插入 DOM。
 * 即便如此仍然清洗：这样即便将来有人改成 v-html，也不会立刻变成漏洞。
 */
export function sanitizeSVG(raw: string | undefined): string {
  if (!raw) return ''
  const cleaned = DOMPurify.sanitize(raw, {
    USE_PROFILES: { svg: true, svgFilters: false },
    ALLOWED_TAGS: SVG_ALLOWED_TAGS,
    ALLOWED_ATTR: SVG_ALLOWED_ATTR,
    FORBID_TAGS: ['script', 'foreignObject', 'iframe', 'image', 'a'],
    FORBID_ATTR: ['href', 'xlink:href', 'src', 'onload', 'onerror'],
  })
  return cleaned.trim()
}

/** 判断清洗后的 SVG 是否仍然可用（有内容且确实以 svg 开头）。 */
export function isUsableSVG(raw: string | undefined): boolean {
  const cleaned = sanitizeSVG(raw)
  return /^<svg[\s>]/i.test(cleaned)
}

/**
 * 把 SVG 文本转成可以直接放进 <img src> 的 data URI。
 *
 * 走 <img> 而不是内联 SVG 是关键：<img> 里的 SVG 不会执行脚本，
 * 也无法发起同源请求，是浏览器层面的强制隔离。
 */
export function svgToDataURI(raw: string | undefined): string {
  const cleaned = sanitizeSVG(raw)
  if (!cleaned) return ''
  // encodeURIComponent 比 base64 更省事且对中文更友好。
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(cleaned)}`
}

/** 清洗 Markdown 渲染后的 HTML。 */
export function sanitizeHTML(raw: string | undefined): string {
  if (!raw) return ''
  return DOMPurify.sanitize(raw, {
    USE_PROFILES: { html: true },
    FORBID_TAGS: ['script', 'style', 'iframe', 'object', 'embed', 'form', 'input'],
    FORBID_ATTR: ['onload', 'onerror', 'onclick', 'style'],
  })
}

/**
 * 把 Markdown 文本转成可安全渲染的 HTML。
 *
 * 这里刻意只支持最小子集（代码块、行内代码、粗体、换行）而**不引入**
 * 完整的 markdown 库：检测产出主要是代码片段，完整解析器带来的
 * XSS 面远比它提供的排版价值大。
 */
export function markdownToSafeHTML(raw: string | undefined): string {
  if (!raw) return ''
  const escaped = escapeHTML(raw)
  const withCodeBlocks = escaped.replace(
    /```([a-zA-Z0-9+#-]*)\n([\s\S]*?)```/g,
    (_m, lang: string, code: string) =>
      `<pre class="iq-codeblock"${lang ? ` data-lang="${lang}"` : ''}><code>${code}</code></pre>`,
  )
  const withInlineCode = withCodeBlocks.replace(/`([^`\n]+)`/g, '<code>$1</code>')
  const withBold = withInlineCode.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>')
  const withBreaks = withBold.replace(/\n/g, '<br>')
  return sanitizeHTML(withBreaks)
}

/** 按渲染类型决定是否把内容当作 SVG 处理。 */
export function looksLikeSVG(raw: string | undefined): boolean {
  if (!raw) return false
  return /^\s*(?:<\?xml[^>]*>\s*)?<svg[\s>]/i.test(raw)
}

/**
 * 决定一件作品的最终呈现形态。
 *
 * 优先级：后端声明的 render_kind 优先，但内容实际形态作为兜底 ——
 * 模型经常在 text 类题目里直接吐 SVG，这时应该渲染成图而不是一堆标签。
 */
export function resolveRenderKind(
  declared: IQRenderKind,
  content: string | undefined,
  imageURL: string | undefined,
): IQRenderKind {
  if (declared === 'image_url' && imageURL) return 'image_url'
  if (looksLikeSVG(content) && isUsableSVG(content)) return 'svg'
  if (declared === 'markdown') return 'markdown'
  return declared === 'image_url' ? 'text' : declared
}

/** HTML 转义，用于纯文本插入。 */
export function escapeHTML(raw: string): string {
  return raw
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

/** 截断展示文本，避免超长内容把卡片撑爆。 */
export function truncateText(raw: string | undefined, limit = 600): string {
  if (!raw) return ''
  if (raw.length <= limit) return raw
  return `${raw.slice(0, limit)}…`
}
