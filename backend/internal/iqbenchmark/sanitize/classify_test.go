//go:build unit

package sanitize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyCompleteSVGInFence(t *testing.T) {
	input := "这是鹈鹕骑自行车的 SVG：\n\n```svg\n<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 100 100\"><circle cx=\"50\" cy=\"50\" r=\"40\"/></svg>\n```\n\n希望你喜欢！"
	require.Equal(t, RenderSVG, Classify(ClassifyInput{Text: input, ExpectKind: "svg"}))
}

func TestClassifyBareSVGWithoutFence(t *testing.T) {
	input := `<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`
	require.Equal(t, RenderSVG, Classify(ClassifyInput{Text: input}))
}

// 模型常在 token 上限处截断 SVG：只要有毒开标签与视觉元素就判可恢复 SVG，
// 而不是当成纯文本丢弃。
func TestClassifyRecoverableTruncatedSVG(t *testing.T) {
	input := `<svg xmlns="http://www.w3.org/2000/svg"><path d="M10 10 L20 20"/>`
	require.Equal(t, RenderSVG, Classify(ClassifyInput{Text: input}))
}

// 只是「提到 <svg> 这个词」的散文不能被误判为产物。
func TestClassifyProseMentioningSVGTagIsNotArtifact(t *testing.T) {
	input := "要生成图，你可以使用 <svg> 标签来绘制矢量图形，这是最常见的做法之一。"
	require.NotEqual(t, RenderSVG, Classify(ClassifyInput{Text: input}))
}

func TestClassifyHTMLAsMarkdown(t *testing.T) {
	input := `<div><h1>标题</h1><p>正文</p></div>`
	require.Equal(t, RenderMarkdown, Classify(ClassifyInput{Text: input}))
}

func TestClassifyMarkdownByHeading(t *testing.T) {
	input := "## 分析\n\n这是分析内容。"
	require.Equal(t, RenderMarkdown, Classify(ClassifyInput{Text: input}))
}

func TestClassifyMarkdownByList(t *testing.T) {
	input := "要点：\n\n- 第一点\n- 第二点\n"
	require.Equal(t, RenderMarkdown, Classify(ClassifyInput{Text: input}))
}

func TestClassifyMarkdownByTable(t *testing.T) {
	input := "| 列1 | 列2 |\n| --- | --- |\n| a | b |\n"
	require.Equal(t, RenderMarkdown, Classify(ClassifyInput{Text: input}))
}

func TestClassifyPlainText(t *testing.T) {
	input := "这是一段没有任何结构的普通回答。"
	require.Equal(t, RenderText, Classify(ClassifyInput{Text: input}))
}

func TestClassifyEmpty(t *testing.T) {
	require.Equal(t, RenderText, Classify(ClassifyInput{Text: ""}))
	require.Equal(t, RenderText, Classify(ClassifyInput{Text: "   \n\t"}))
}

// 覆盖率闸门：围栏内容太短、大部分是说明文字时，不应把围栏内容当主导产物。
func TestClassifyRequiresDominantArtifactCoverage(t *testing.T) {
	// 大量说明 + 极短围栏 → 覆盖率低于阈值，判定回落到 Markdown（因为有结构标记）。
	longProse := strings.Repeat("这是一段很长的解释性文字，模型主要在说话而不是产出作品。", 20)
	input := longProse + "\n\n```svg\n<svg><rect/></svg>\n```\n"
	kind := Classify(ClassifyInput{Text: input})
	require.NotEqual(t, RenderSVG, kind, "围栏内容占比过低时不应判为 SVG 产物")
}

func TestCoverageComputation(t *testing.T) {
	require.InDelta(t, 1.0, coverage("<svg><rect/></svg>", "<svg><rect/></svg>"), 1e-9)
	require.InDelta(t, 0.0, coverage("", "anything"), 1e-9)
}

func TestExtractSVGFromBareOutput(t *testing.T) {
	input := `<svg xmlns="http://www.w3.org/2000/svg"><circle r="5"/></svg>`
	svg, ok := ExtractSVG(input)
	require.True(t, ok)
	require.Equal(t, input, svg)
}

func TestExtractSVGFromFence(t *testing.T) {
	input := "说明\n```svg\n<svg><circle r=\"5\"/></svg>\n```\n结尾"
	svg, ok := ExtractSVG(input)
	require.True(t, ok)
	require.Contains(t, svg, "<svg")
	require.Contains(t, svg, "</svg>")
}

func TestExtractSVGSlicesFromSurroundingProse(t *testing.T) {
	input := `好的，这是作品：<svg><rect width="5" height="5"/></svg>希望满意。`
	svg, ok := ExtractSVG(input)
	require.True(t, ok)
	require.True(t, strings.HasPrefix(svg, "<svg"))
	require.True(t, strings.HasSuffix(svg, "</svg>"))
}

func TestExtractSVGReturnsFalseWhenAbsent(t *testing.T) {
	_, ok := ExtractSVG("这里没有任何矢量图形。")
	require.False(t, ok)
}

func TestExtractSVGKeepsUnclosedTail(t *testing.T) {
	input := `作品：<svg><path d="M0 0 L5 5"/>`
	svg, ok := ExtractSVG(input)
	require.True(t, ok)
	require.True(t, strings.Contains(svg, "<path"))
}

func TestLooksLikeImageCase(t *testing.T) {
	require.True(t, LooksLikeImageCase("image_url"))
	require.True(t, LooksLikeImageCase("IMAGE_URL"))
	require.False(t, LooksLikeImageCase("svg"))
}
