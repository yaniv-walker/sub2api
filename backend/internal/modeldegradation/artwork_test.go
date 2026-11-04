//go:build unit

package modeldegradation

import (
	"strings"
	"testing"
)

// 生成题的「作品」是用户唯一能直观看到的产出，
// 抽不出来就等于这个功能废了一半。

func TestExtractSVG_Plain(t *testing.T) {
	in := `<svg viewBox="0 0 100 100"><circle cx="50" cy="50" r="40"/></svg>`
	got := extractSVG(in)
	if got != in {
		t.Fatalf("extractSVG = %q, want %q", got, in)
	}
}

func TestExtractSVG_FencedBlock(t *testing.T) {
	in := "好的，下面是我的作品：\n\n```svg\n<svg viewBox=\"0 0 10 10\"><rect/></svg>\n```\n\n希望你喜欢。"
	got := extractSVG(in)
	want := `<svg viewBox="0 0 10 10"><rect/></svg>`
	if got != want {
		t.Fatalf("extractSVG = %q, want %q", got, want)
	}
}

func TestExtractSVG_WithSurroundingProse(t *testing.T) {
	in := "我来画一只鹈鹕骑自行车。\n<svg viewBox=\"0 0 200 200\"><g><circle/></g></svg>\n画面中还有校验码。"
	got := extractSVG(in)
	if !strings.HasPrefix(got, "<svg") {
		t.Fatalf("应以 <svg 开头，实际 %q", got)
	}
	if !strings.HasSuffix(got, "</svg>") {
		t.Fatalf("应以 </svg> 结尾，实际 %q", got)
	}
}

func TestExtractSVG_InsideHTMLWrapper(t *testing.T) {
	// 模型常把 SVG 包在完整 HTML 文档里。要能从 HTML 里剥出来。
	in := `<!DOCTYPE html>
<html><head><style>body{margin:0}</style></head>
<body><svg width="200" height="200"><circle r="50"/></svg></body></html>`
	got := extractSVG(in)
	if !strings.Contains(got, "<circle r=\"50\"/>") {
		t.Fatalf("extractSVG = %q", got)
	}
	if strings.Contains(got, "<html>") {
		t.Fatalf("不应把 HTML 外壳带进来: %q", got)
	}
}

func TestExtractSVG_PrefersLastCloseTag(t *testing.T) {
	// 有的回答先给一个空壳图标占位，再给真正的作品。
	// 用第一个 </svg> 会截出半截，必须用最后一个。
	in := `<svg></svg>` + "\n" + `<svg viewBox="0 0 5 5"><path d="M0 0"/></svg>`
	got := extractSVG(in)
	if !strings.Contains(got, `<path d="M0 0"/>`) {
		t.Fatalf("应包含真正的作品内容，实际 %q", got)
	}
}

func TestExtractSVG_NoSVG(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"抱歉，我无法生成图片。",
		"<html><body>纯 HTML 没有 SVG</body></html>",
		"<svg 缺少闭合标签",
		"只有 </svg> 闭合标签",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if got := extractSVG(in); got != "" {
				t.Fatalf("extractSVG(%q) = %q, want 空串", in, got)
			}
		})
	}
}

func TestStripFences(t *testing.T) {
	in := "```svg\n<svg></svg>\n```"
	got := stripFences(in)
	if strings.Contains(got, "```") {
		t.Fatalf("围栏应被剥掉，实际 %q", got)
	}
	if !strings.Contains(got, "<svg>") {
		t.Fatalf("围栏内容必须保留，实际 %q", got)
	}
}

func TestStripFences_PreservesContentExactly(t *testing.T) {
	// 围栏里的属性一个字节都不能改 —— 动画属性对格式敏感。
	svg := `<svg><animate attributeName="opacity" values="0;1;0"/></svg>`
	got := stripFences("```html\n" + svg + "\n```")
	if !strings.Contains(got, svg) {
		t.Fatalf("SVG 内容被改动了: %q", got)
	}
}

func TestLooksLikeSVG(t *testing.T) {
	if !LooksLikeSVG(`<svg></svg>`) {
		t.Fatal("应识别为 SVG")
	}
	if LooksLikeSVG("纯文本") {
		t.Fatal("不应识别为 SVG")
	}
}

func TestFillRenderTemplate_ReplacesPlaceholders(t *testing.T) {
	template := "请画一幅{scene}的作品，并在右下角显示校验码 {code}。"
	prompt, code := FillRenderTemplate(template)

	if strings.Contains(prompt, "{scene}") {
		t.Fatalf("scene 占位符未被替换: %q", prompt)
	}
	if strings.Contains(prompt, "{code}") {
		t.Fatalf("code 占位符未被替换: %q", prompt)
	}
	if len(code) != 8 {
		t.Fatalf("校验码应为 8 位，实际 %q（长度 %d）", code, len(code))
	}
	if !strings.Contains(prompt, code) {
		t.Fatalf("prompt 里应包含校验码 %q，实际 %q", code, prompt)
	}
}

func TestFillRenderTemplate_CodeIsHex(t *testing.T) {
	// 校验码必须是十六进制形态，因为要求模型「用 SVG text 元素显示」，
	// 非 ASCII 字符在不同字体下的渲染差异会让核对变难。
	for i := 0; i < 20; i++ {
		_, code := FillRenderTemplate("{code}")
		for _, r := range code {
			isDigit := r >= '0' && r <= '9'
			isUpper := r >= 'A' && r <= 'F'
			if !isDigit && !isUpper {
				t.Fatalf("校验码含非十六进制字符: %q", code)
			}
		}
	}
}

func TestFillRenderTemplate_CodeIsRandom(t *testing.T) {
	// 校验码必须真的不可预测 —— 否则可以预先准备带固定码的图来充数。
	seen := make(map[string]bool, 50)
	for i := 0; i < 50; i++ {
		_, code := FillRenderTemplate("{code}")
		seen[code] = true
	}
	// 50 次里出现重复的概率极低（16^8 空间）。
	if len(seen) < 45 {
		t.Fatalf("校验码重复过多，只产生了 %d 个不同值", len(seen))
	}
}

func TestFillRenderTemplate_SceneComesFromPool(t *testing.T) {
	_, _ = FillRenderTemplate("{scene}")
	prompt, _ := FillRenderTemplate("场景：{scene}")
	found := false
	for _, scene := range renderScenes {
		if strings.Contains(prompt, scene) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("prompt 里的场景不在池中: %q", prompt)
	}
}

func TestFillRenderTemplate_NoPlaceholders(t *testing.T) {
	// 模板里没有占位符时不应崩溃，也不应改动原文。
	prompt, code := FillRenderTemplate("画一只猫。")
	if prompt != "画一只猫。" {
		t.Fatalf("无占位符时不应改动原文，实际 %q", prompt)
	}
	if len(code) != 8 {
		t.Fatalf("即使不用也仍应生成校验码，实际 %q", code)
	}
}

func TestRandomScene_NonEmpty(t *testing.T) {
	for i := 0; i < 20; i++ {
		if s := randomScene(); s == "" {
			t.Fatal("randomScene 不应返回空串")
		}
	}
}
