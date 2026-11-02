package sanitize

import (
	"regexp"
	"strings"
)

// RenderKind 是判定得到的渲染形态。
const (
	RenderImageURL = "image_url"
	RenderSVG      = "svg"
	RenderMarkdown = "markdown"
	RenderText     = "text"
)

// MinArtifactCoverage 是「产物覆盖率」阈值。
//
// 含义：去掉 Markdown 说明文字、代码围栏标记与空白后，有效产物占全文的比例。
// 低于该阈值说明模型主要在「说话」而不是「产出」，应判为文本而非产物。
//
// 该阈值取自 MIT 许可的 joeseesun/llm-case-benchmark
// （public/output-classifier.js 的 MIN_ARTIFACT_COVERAGE = 0.72）。
const MinArtifactCoverage = 0.72

var (
	// 代码围栏：```lang ... ```
	reFencedBlock = regexp.MustCompile("(?s)```[a-zA-Z0-9_+-]*\\s*\\n(.*?)```")
	// 任意围栏（含未闭合）
	reAnyFence = regexp.MustCompile("(?s)```")
	// SVG 根元素
	reSVGRoot = regexp.MustCompile(`(?is)<svg[\s>]`)
	// HTML 根元素（含常见容器）
	reHTMLRoot = regexp.MustCompile(`(?i)<(?:html|body|div|p|table|ul|ol|h[1-6])\b`)
	// Markdown 结构标记
	reMarkdownHeading = regexp.MustCompile(`(?m)^#{1,6}\s+\S`)
	reMarkdownList    = regexp.MustCompile(`(?m)^\s*(?:[-*+]|\d+\.)\s+\S`)
	reMarkdownTable   = regexp.MustCompile(`(?m)^\s*\|.+\|`)
)

// ClassifyInput 是形态判定所需的输入。
type ClassifyInput struct {
	// Text 是模型返回的正文（已抽取纯文本）。
	Text string
	// ExpectKind 是题目的期望输出形态，用于在模糊时做优先级裁决。
	ExpectKind string
}

// Classify 判定模型输出的渲染形态。
//
// 判定顺序（先强后弱）：
//  1. 取「主导产物」：若存在代码围栏，优先用最大的围栏内容；
//     fenced 内容占全文比例需 ≥ MinArtifactCoverage 才算主导产物；
//  2. 主导产物是完整 SVG（含 </svg>）→ svg；
//  3. 主导产物是可恢复 SVG（有 <svg 但未闭合）→ svg（补闭合由前端容错渲染）；
//  4. 主导产物是 HTML → markdown（经 DOMPurify 后渲染，避免直接执行）；
//  5. 无围栏但全文本身是完整 SVG → svg；
//  6. 否则含 Markdown 结构 → markdown；
//  7. 都否则 → text。
//
// 返回的 kind 与「是否走了围栏/是否可恢复」一起交给上层决定是否记录额外标记。
func Classify(in ClassifyInput) string {
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return RenderText
	}

	// 第 1 步：找最大围栏内容。
	dominant, hasFence := dominantFencedBlock(text)
	if hasFence {
		if coverage(text, dominant) >= MinArtifactCoverage {
			if kind := classifyFragment(dominant); kind != "" {
				return kind
			}
		}
		// 有围栏但围栏内容不是主导产物：说明模型主要在解释而非产出，
		// 此时**不能**回落到「全文即产物」判定 —— 全文里恰好出现 <svg 字样
		// 会让散文被误判成作品。改为按 Markdown/文本处理。
		if reMarkdownHeading.MatchString(text) || reMarkdownList.MatchString(text) || reMarkdownTable.MatchString(text) {
			return RenderMarkdown
		}
		return RenderText
	}

	// 第 5 步：全文自身就是产物（常见于模型直接输出裸 SVG，无围栏）。
	if kind := classifyFragment(text); kind != "" {
		return kind
	}

	// 第 6 步：Markdown 结构。
	if reMarkdownHeading.MatchString(text) || reMarkdownList.MatchString(text) || reMarkdownTable.MatchString(text) {
		return RenderMarkdown
	}

	// 第 7 步：纯文本。
	return RenderText
}

// dominantFencedBlock 返回最长的代码围栏内容，以及是否存在围栏。
func dominantFencedBlock(text string) (string, bool) {
	matches := reFencedBlock.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		// 有围栏标记但都没闭合：把第一个围栏之后的内容当作产物。
		if !reAnyFence.MatchString(text) {
			return "", false
		}
		idx := strings.Index(text, "```")
		rest := text[idx+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		}
		return rest, true
	}
	best := ""
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		if len(m[1]) > len(best) {
			best = m[1]
		}
	}
	return best, true
}

// classifyFragment 对一段候选产物做形态判定。
// 返回空串表示该片段不是可识别的产物形态。
func classifyFragment(fragment string) string {
	frag := strings.TrimSpace(fragment)
	if frag == "" {
		return ""
	}
	if isCompleteSVG(frag) || isRecoverableSVG(frag) {
		return RenderSVG
	}
	if reHTMLRoot.MatchString(frag) {
		// HTML 统一按 markdown 路径渲染：前端会先经 DOMPurify 再插入 DOM，
		// 不区分「原生 HTML」与「Markdown 里的 HTML」两条渲染路径。
		return RenderMarkdown
	}
	if reMarkdownHeading.MatchString(frag) || reMarkdownList.MatchString(frag) || reMarkdownTable.MatchString(frag) {
		return RenderMarkdown
	}
	return ""
}

// isCompleteSVG 判断是否为完整 SVG（有开标签也有闭标签）。
func isCompleteSVG(s string) bool {
	return reSVGRoot.MatchString(s) && strings.Contains(strings.ToLower(s), "</svg>")
}

// isRecoverableSVG 判断是否为可恢复 SVG（有开标签但未闭合）。
//
// 模型经常在 token 上限处截断 SVG；只要开标签存在且内容看起来是 SVG 片段，
// 前端可以用 XML 容错解析补闭合后展示，比整条丢弃有价值。
func isRecoverableSVG(s string) bool {
	if !reSVGRoot.MatchString(s) {
		return false
	}
	if strings.Contains(strings.ToLower(s), "</svg>") {
		return false
	}
	// 至少含一个 SVG 视觉元素，避免把「提到 <svg> 这个词」的散文误判为产物。
	for _, marker := range []string{"<path", "<rect", "<circle", "<ellipse", "<polygon", "<polyline", "<line", "<g ", "<text", "<image"} {
		if strings.Contains(strings.ToLower(s), marker) {
			return true
		}
	}
	return false
}

// coverage 计算「有效产物体积 / 全文体积」。
//
// 去除说明性文字的方式：只按字符数比较，不试图语义分段 —— 阈值本身
// 就是统计意义上的启发式，过度加工反而让判定不可解释。
func coverage(full, fragment string) float64 {
	full = strings.TrimSpace(full)
	if full == "" {
		return 0
	}
	// 用去掉围栏标记后的正文长度做分母，避免围栏符号抬高覆盖率。
	denominator := len([]rune(reAnyFence.ReplaceAllString(full, "")))
	if denominator == 0 {
		return 0
	}
	numerator := len([]rune(strings.TrimSpace(fragment)))
	return float64(numerator) / float64(denominator)
}

// ExtractSVG 从模型输出中抽出 SVG 文本（含未闭合情形）。
//
// 返回 (svg, true) 表示抽到；否则返回 ("", false)。
// 抽取后仍必须经过 ScanSVG 与前端消毒，本函数不做任何安全处理。
func ExtractSVG(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	// 优先整段：模型常见做法是直接输出裸 SVG（必须以 <svg 开头，
	// 否则只是散文中提到了这个标签）。
	if startsWithSVGTag(trimmed) {
		return trimmed, true
	}
	// 其次从围栏里抽。
	if dominant, ok := dominantFencedBlock(trimmed); ok {
		frag := strings.TrimSpace(dominant)
		if startsWithSVGTag(frag) || reSVGRoot.MatchString(frag) {
			return frag, true
		}
	}
	// 最后从任意位置截取 `<svg` 到 `</svg>`。
	lower := strings.ToLower(trimmed)
	start := strings.Index(lower, "<svg")
	if start < 0 {
		return "", false
	}
	end := strings.LastIndex(lower, "</svg>")
	if end < 0 || end < start {
		return trimmed[start:], true
	}
	return trimmed[start : end+len("</svg>")], true
}

// startsWithSVGTag 判断文本是否以 <svg 开头（允许前置 XML 声明与注释）。
func startsWithSVGTag(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	if strings.HasPrefix(lower, "<svg") {
		return true
	}
	// 允许 `<?xml ...?><svg` 与 `<!-- comment --><svg` 这类前缀。
	if strings.HasPrefix(lower, "<?xml") || strings.HasPrefix(lower, "<!--") {
		idx := strings.Index(lower, "<svg")
		if idx < 0 {
			return false
		}
		// 前缀里不能出现 XML 之外的文本元素（粗略但足够区分散文）。
		prefix := lower[:idx]
		return !strings.Contains(prefix, "<p") && !strings.Contains(prefix, "<div")
	}
	return false
}

// LooksLikeImageCase 判断题目期望的是图片产物。
func LooksLikeImageCase(expectKind string) bool {
	return strings.EqualFold(strings.TrimSpace(expectKind), RenderImageURL)
}
