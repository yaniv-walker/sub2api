/**
 * SVG 产出的渲染前处理与 iframe 隔离策略。
 *
 * 为什么需要这一层：模型经常不老实 —— 它会把 SVG 塞进 Markdown 代码围栏里，
 * 或者在 SVG 后面跟一大段解释文字。直接丢给浏览器的话，前者渲染成一段代码文本，
 * 后者会把半张图后面的中文当成 SVG 文本节点渲染到画布里。
 * 所以送进 iframe 之前要先把「真正的 SVG 片段」切出来。
 */

/** SVG 里允许保留的标签（与 sanitize.ts 共用同一份白名单）。 */
const SVG_START = /<svg[\s>]/i

/**
 * 从模型输出里切出可渲染的 SVG 片段。
 *
 * 三步，顺序不能换：
 *   1. 去掉 Markdown 代码围栏（```svg / ``` / ~~~ 等）：
 *      围栏是**包裹**，不先摘掉的话第 2 步切出来的片段里仍带 ``` 尾巴；
 *   2. 从第一个 `<svg` 切到 `</svg>`（或 `</canvas>`）：
 *      模型偶尔会把 `</svg>` 写成 `</canvas>`，这是真实出现过的输出错误，
 *      不处理就等于整张图丢弃；
 *   3. 去掉 SVG 之后的所有内容（解释文字、代码块尾巴）。
 *
 * 找不到 `<svg` 时返回空串 —— 调用方据此回落到纯文本。
 */
export function sliceSVG(raw: string | undefined): string {
  if (!raw) return ''
  let text = raw

  // 1. 去代码围栏。只处理成对出现的围栏，避免把内容里出现的单个 ``` 也削掉。
  text = text.replace(/```[a-zA-Z0-9+#-]*\r?\n([\s\S]*?)```/g, '$1')
  text = text.replace(/~~~[a-zA-Z0-9+#-]*\r?\n([\s\S]*?)~~~/g, '$1')

  // 2. 定位 SVG 起点。XML 声明与 DOCTYPE 在 iframe 里不需要。
  const start = text.search(SVG_START)
  if (start < 0) return ''
  text = text.slice(start)

  // 3. 定位终点。优先 </svg>，缺失时容忍 </canvas>，
  //    两者都没有则当作未闭合 —— 交给浏览器补齐（iframe 内是独立文档，
  //    未闭合标签不会泄漏到宿主页面）。
  //
  //    注意：无论原文用哪个结束标签，输出一律归一化成 </svg>。
  //    开标签是 <svg>，收标签写成 </canvas> 时浏览器会把 </canvas>
  //    当作未知标签丢弃、让 <svg> 保持未闭合 —— 那张图会渲染成
  //    撑满容器的一片空白。归一化是这条分支唯一有意义的处理。
  const lower = text.toLowerCase()
  const svgEnd = lower.lastIndexOf('</svg>')
  const canvasEnd = lower.lastIndexOf('</canvas>')
  const end = Math.max(svgEnd, canvasEnd)
  if (end >= 0) {
    text = text.slice(0, end) + '</svg>'
  }
  return text.trim()
}

/**
 * 把 SVG 包成一份完整 HTML 文档，用于 iframe srcdoc。
 *
 * 必须包成完整文档（含 `<!DOCTYPE html>` 与 `<body>`），原因：
 *   - srcdoc 里只有一段裸 `<svg>` 时，浏览器会按 HTML 解析，
 *     某些自闭合写法（`<path/>`）在 HTML 上下文里会被当成未闭合，
 *     整张图的结构会错乱；
 *   - 独立文档让 SVG 里的 CSS（`<style>`）不会影响宿主页面 ——
 *     iframe 本身就是最强的样式隔离。
 *
 * 关键安全设定：**CSP 里禁掉脚本**。
 * 外层还有 `sandbox=""`（连脚本引擎都不给），这里是第二层 ——
 * 万一将来有人把 sandbox 改成 allow-scripts，CSP 仍然拦住脚本执行。
 */
export function svgDocument(svg: string): string {
  return [
    '<!DOCTYPE html>',
    '<html><head><meta charset="utf-8">',
    // 禁脚本、禁网络：default-src 'none' 之后只放行内联样式。
    `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:">`,
    '<style>',
    'html,body{margin:0;padding:0;background:transparent;}',
    'svg{display:block;width:100%;height:100%;}',
    '</style>',
    '</head><body>',
    svg,
    '</body></html>',
  ].join('')
}

/**
 * 可见 iframe 的 sandbox 值。
 *
 * 空字符串 = **最严格**：不给脚本、不给同源、不给表单、不给弹窗。
 * 空字符串在很多代码库里会被误改成省略该属性 —— 省略 `sandbox`
 * 等于完全不隔离，是最危险的写法，所以这里显式导出成常量，
 * 让「必须带 sandbox 属性」这件事只能通过改常量来实现。
 */
export const VISIBLE_SANDBOX = ''

/**
 * 隐藏测量帧的 sandbox 值。
 *
 * 只给 `allow-same-origin`，**不给** `allow-scripts`：
 * 测量由父页面读取 `iframe.contentDocument` 完成，
 * 帧内不需要跑任何脚本。这一点是整个方案成立的关键 ——
 * 如果靠帧内 postMessage 上报高度，就必须开 allow-scripts，
 * 那等于为了量一个高度把脚本执行权交出去。
 */
export const MEASURE_SANDBOX = 'allow-same-origin'

/**
 * 从一份 SVG 文档里读出宽高比（w/h）。
 *
 * 抽成纯函数是为了可测：真实测量要读 iframe 的 contentDocument，
 * jsdom 里那条路径的行为和浏览器不一致，容易测出一个假结论。
 *
 * 取值优先级：width/height 属性 → viewBox。
 * 先看属性是因为属性代表**渲染尺寸**，viewBox 只是坐标系 ——
 * 一个 viewBox="0 0 24 24" 的图标配上 width="240" 时按属性算更贴实际。
 *
 * 量不出来时返回 0，由调用方回落到固定高度。刻意**不**猜一个比例：
 * 猜错会把图裁掉或留一大片空白，比给个中性高度更糟。
 */
/**
 * 解析 SVG 的尺寸属性。
 *
 * 只接受纯数字（可带小数）：**必须显式排掉百分比**。
 * `parseFloat('100%')` 返回 100，看起来能用，但那是 100% 而不是
 * 100 像素 —— 按它算比例会得出一个假的 1:1，把 3:1 的图渲染成正方形。
 * 设计工具导出的 SVG 里 `width="100%"` 极其常见，不排掉等于
 * 大部分导入的图都会算错。
 */
function parseLength(raw: string | null): number {
  if (!raw) return Number.NaN
  const trimmed = raw.trim()
  if (trimmed.endsWith('%')) return Number.NaN
  // 允许 px/pt 等单位后缀，但只取数值部分。
  const match = /^([+-]?\d*\.?\d+)/.exec(trimmed)
  if (!match) return Number.NaN
  return Number.parseFloat(match[1])
}

export function aspectFromDocument(doc: Document | null | undefined): number {
  if (!doc) return 0
  const svg = doc.querySelector('svg')
  if (!svg) return 0

  const attrW = parseLength(svg.getAttribute('width'))
  const attrH = parseLength(svg.getAttribute('height'))
  if (Number.isFinite(attrW) && Number.isFinite(attrH) && attrW > 0 && attrH > 0) {
    return attrW / attrH
  }

  const box = svg.getAttribute('viewBox')
  if (box) {
    const parts = box.split(/[\s,]+/).map((v) => Number.parseFloat(v))
    if (parts.length === 4) {
      const [, , w, h] = parts
      if (Number.isFinite(w) && Number.isFinite(h) && w > 0 && h > 0) {
        return w / h
      }
    }
  }
  return 0
}

/**
 * 由宽高比算出容器高度（px）。
 *
 * 两个方向都要夹，缺一边就会出极端：
 *   - 不夹宽高比：20:1 的横条图会被压到 16px 高（内容全看不见），
 *     1:20 的竖条图会被拉成上千像素（卡片里一大片空白）；
 *   - 只夹宽高比不夹结果高度：很瘦的图仍然会算出远超卡片的高度，
 *     虽然 CSS 的 max-h 会裁掉，但那等于把「算错」交给下游兜底。
 *
 * 因此结果高度最终落在 [120, fallbackHeight * maxAspect]。
 */
export function frameHeightFor(
  aspect: number,
  fallbackHeight: number,
  maxAspect: number,
): number {
  if (!(aspect > 0)) return fallbackHeight
  const clamped = Math.min(Math.max(aspect, 1 / maxAspect), maxAspect)
  // 以 4:3 为基准：宽高比越大，同样宽度下需要的高度越小。
  const raw = Math.round((fallbackHeight * 4) / 3 / clamped)
  const lowerBound = 120
  const upperBound = Math.round(fallbackHeight * maxAspect)
  return Math.min(Math.max(raw, lowerBound), Math.max(upperBound, lowerBound))
}
