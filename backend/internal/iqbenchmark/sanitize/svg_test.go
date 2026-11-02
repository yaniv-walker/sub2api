//go:build unit

package sanitize

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ---- SVG / HTML 危险构造 ----

func TestScanSVGBlocksScriptTag(t *testing.T) {
	res := ScanSVG(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	require.True(t, res.Blocked, "含 <script> 必须整条判为 blocked")
	require.Contains(t, res.Flags, FlagScript)
	// 阻断类不剥离：保留原文供管理员审阅，但公开侧不会下发。
	require.Contains(t, res.Content, "<script>")
}

func TestScanSVGStripsEventHandlerAttributes(t *testing.T) {
	res := ScanSVG(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><rect onclick="x()" width="10" height="10"/></svg>`)
	require.False(t, res.Blocked, "事件属性属可剥离类，不应整条阻断")
	require.Contains(t, res.Flags, FlagEventHandler)
	require.NotContains(t, res.Content, "onload")
	require.NotContains(t, res.Content, "onclick")
	require.True(t, res.Changed)
	// 主体必须保留：剥离不该把作品变成空串。
	require.Contains(t, res.Content, "<rect")
	require.Contains(t, res.Content, `width="10"`)
}

func TestScanSVGBlocksForeignObject(t *testing.T) {
	res := ScanSVG(`<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><body xmlns="http://www.w3.org/1999/xhtml"><script>alert(1)</script></body></foreignObject></svg>`)
	require.True(t, res.Blocked)
	require.Contains(t, res.Flags, FlagForeignObject)
}

func TestScanSVGBlocksJavascriptURL(t *testing.T) {
	res := ScanSVG(`<svg xmlns="http://www.w3.org/2000/svg"><a href="javascript:alert(1)"><text>x</text></a></svg>`)
	require.True(t, res.Blocked)
	require.Contains(t, res.Flags, FlagJavascriptURL)
}

func TestScanSVGBlocksEntityDeclaration(t *testing.T) {
	// XXE / 实体炸弹的典型载荷。
	res := ScanSVG(`<?xml version="1.0"?><!ENTITY lol "lol"><svg>&lol;</svg>`)
	require.True(t, res.Blocked)
	require.Contains(t, res.Flags, FlagEntityDecl)
}

func TestScanSVGBlocksEmbedObjectIframe(t *testing.T) {
	for _, payload := range []string{
		`<svg><iframe src="https://evil.example"></iframe></svg>`,
		`<svg><object data="x"></object></svg>`,
		`<svg><embed src="x"/></svg>`,
	} {
		res := ScanSVG(payload)
		require.True(t, res.Blocked, payload)
		require.Contains(t, res.Flags, FlagEmbedObjectFrame, payload)
	}
}

func TestScanSVGStripsDoctype(t *testing.T) {
	res := ScanSVG("<!DOCTYPE svg PUBLIC \"-//W3C//DTD SVG 1.1//EN\" \"http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd\">\n<svg><rect/></svg>")
	require.False(t, res.Blocked)
	require.Contains(t, res.Flags, FlagDoctype)
	require.NotContains(t, res.Content, "<!DOCTYPE")
	require.Contains(t, res.Content, "<svg>")
}

func TestScanSVGStripsCSSImport(t *testing.T) {
	res := ScanSVG(`<svg><style>@import url("https://evil.example/x.css"); .a{fill:red}</style><rect class="a"/></svg>`)
	require.False(t, res.Blocked)
	require.Contains(t, res.Flags, FlagCSSImport)
	require.NotContains(t, res.Content, "@import")
	// 同一 style 块内的正常规则必须保留。
	require.Contains(t, res.Content, "fill:red")
}

func TestScanSVGStripsDataURI(t *testing.T) {
	res := ScanSVG(`<svg><image href="data:image/svg+xml;base64,PHN2Zz48L3N2Zz4="/></svg>`)
	require.False(t, res.Blocked)
	require.Contains(t, res.Flags, FlagDataURI)
	require.NotContains(t, res.Content, "data:image/svg+xml")
}

func TestScanSVGStripsExternalRefs(t *testing.T) {
	res := ScanSVG(`<svg xmlns:xlink="http://www.w3.org/1999/xlink"><image xlink:href="https://tracker.example/pixel.png"/><rect/></svg>`)
	require.False(t, res.Blocked)
	require.Contains(t, res.Flags, FlagExternalRef)
	require.NotContains(t, res.Content, "tracker.example")
	require.Contains(t, res.Content, "<rect/>")
}

// 内联锚点引用是 SVG 的正常写法（渐变、裁剪、滤镜），绝不能当成外链剥离。
func TestScanSVGKeepsInternalAnchorRefs(t *testing.T) {
	res := ScanSVG(`<svg><defs><linearGradient id="g"><stop offset="0"/></linearGradient></defs><rect fill="url(#g)" href="#g"/></svg>`)
	require.False(t, res.Blocked)
	require.NotContains(t, res.Flags, FlagExternalRef)
	require.False(t, res.Changed)
	require.Contains(t, res.Content, "url(#g)")
}

func TestScanSVGLeavesCleanContentUntouched(t *testing.T) {
	clean := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle cx="50" cy="50" r="40" fill="blue"/></svg>`
	res := ScanSVG(clean)
	require.False(t, res.Blocked)
	require.Empty(t, res.Flags)
	require.False(t, res.Changed)
	require.Equal(t, clean, res.Content)
}

func TestScanTextOnlyScansWithoutStripping(t *testing.T) {
	input := `<svg onload="alert(1)"><rect/></svg>`
	res := ScanText(input)
	require.Contains(t, res.Flags, FlagEventHandler)
	// 纯文本路径不做剥离：内容原样保留，靠 <pre> 渲染避免解析。
	require.Equal(t, input, res.Content)
	require.False(t, res.Changed)
}

func TestScanHTMLBlocksScript(t *testing.T) {
	res := ScanHTML(`<div><script>fetch('/steal')</script></div>`)
	require.True(t, res.Blocked)
	require.Contains(t, res.Flags, FlagScript)
}

func TestScanEmptyInput(t *testing.T) {
	for _, input := range []string{"", "   ", "\n\t"} {
		res := ScanSVG(input)
		require.False(t, res.Blocked)
		require.Empty(t, res.Flags)
	}
}

func TestHasBlockFlagDistinguishesClasses(t *testing.T) {
	require.True(t, HasBlockFlag([]string{FlagScript}))
	require.True(t, HasBlockFlag([]string{FlagEventHandler, FlagEntityDecl}))
	require.False(t, HasBlockFlag([]string{FlagEventHandler, FlagDoctype}))
	require.False(t, HasBlockFlag(nil))
	require.True(t, IsStripFlag(FlagEventHandler))
	require.False(t, IsStripFlag(FlagScript))
}

// SafeSummary 绝不能把原始错误文本带出去（可能含供应商/账号信息）。
func TestSafeSummaryIsSanitized(t *testing.T) {
	require.Equal(t, "生成超时，未在限定时间内完成", SafeSummary(ErrCodeTimeout))
	require.Equal(t, "检测未成功完成", SafeSummary("some_internal_code"))
	require.Equal(t, "", SafeSummary(""))
	// 未识别的码不得原样返回。
	require.NotContains(t, SafeSummary("openai_429_org_abc"), "openai")
	require.NotContains(t, SafeSummary("openai_429_org_abc"), "org_abc")
}
