package modeldegradation

import "strings"

// 本文件从模型回答里抽出 SVG 源码。
//
// 模型几乎不会只吐一段裸 SVG。真实形态有三种：
//   1. ```svg ... ``` 或 ```html ... ``` 包裹
//   2. 前面带一段解释（"好的，下面是我的作品："），后面直接跟 SVG
//   3. 混着 HTML 外壳（<!DOCTYPE html><html>...<svg>...</svg>...</html>）
//
// 抽取策略：先剥代码围栏，再定位 <svg ...> 到最后一个 </svg>。
// 用「最后一个」而不是「第一个」：外层 HTML 里如果先出现一个空 <svg>
// （图标占位），第一个会抽到空壳。实际作品总在后面。

// extractSVG 返回可直接渲染的 SVG 源码；没找到返回空串。
func extractSVG(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}

	body := stripFences(text)

	lower := strings.ToLower(body)
	start := strings.Index(lower, "<svg")
	if start < 0 {
		return ""
	}
	end := strings.LastIndex(lower, "</svg>")
	if end < 0 || end <= start {
		return ""
	}
	end += len("</svg>")

	svg := body[start:end]
	if strings.TrimSpace(svg) == "" {
		return ""
	}
	return svg
}

// stripFences 剥掉 Markdown 代码围栏。
//
// 只处理围栏本身，不改动围栏里的内容 —— 里面的 SVG 要原样保留，
// 任何「美化」都可能破坏动画属性。
func stripFences(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			// 整行只是围栏（可能带语言标记），丢弃。
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// LooksLikeSVG 判断文本里是否有可用 SVG。
func LooksLikeSVG(text string) bool {
	return extractSVG(text) != ""
}
