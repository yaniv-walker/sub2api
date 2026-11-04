package modeldegradation

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// 本文件实现判定口径。全部规则都是从参考实现的真实响应里反推出来的，
// 不是自创：
//
//	运行级   verdict = normal | degraded | unknown | generated
//	生成级   render_verdict = ok | degraded | unknown | generated（独立于运行级）
//	单题级   item.verdict = ok | degraded | generated | unknown
//
// 通过率 = normal 数 ÷ 已完成数（cancelled 不计入分母）。

// 数字提取：回答里出现的所有整数或小数。
//
// 为什么用「回答里是否出现这个数字」而不是精确比对：
// 模型回答是自然语言，几乎不可能只吐出一个裸数字。真实响应里的形态是
// 「最少取出 **21 颗**。由于……」，所以要判断的是期望值有没有出现在
// 回答中。这个口径与参考实现一致，其 detail 文案就是
// 「未出现期望答案 21，回答中的数字：29, 7, 9, 16」。
var numberPattern = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

// ExtractNumbers 提取文本中的所有数字（保持出现顺序，含重复）。
func ExtractNumbers(text string) []string {
	matches := numberPattern.FindAllString(text, -1)
	if matches == nil {
		return []string{}
	}
	return matches
}

// NormalizeAnswer 归一化答案字符串用于比较。
//
// 处理三件事：
//   - 去掉首尾空白与常见的包裹标点（引号、括号、句号）
//   - 全角数字转半角（模型常打出全角）
//   - 数值形态统一：21、21.0、21.00 视为同一个数
//
// 不做的：不去掉中间空格、不转小写 —— 这两件事会让 "2 1" 和 "21"
// 混为一谈，那是误判而不是宽容。
func NormalizeAnswer(s string) string {
	trimmed := strings.TrimSpace(s)
	trimmed = foldFullWidth(trimmed)
	trimmed = strings.Trim(trimmed, `"'()[]{}。.,，、；;：:！!？?`+" \t\r\n")
	if trimmed == "" {
		return ""
	}
	// 纯数值形态统一成最短表示。
	if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
		// 用 %g 去掉无意义的尾随零；21.0 -> 21，21.50 -> 21.5
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	return trimmed
}

// foldFullWidth 把全角 ASCII 区（！到 ～）与全角空格转成半角。
func foldFullWidth(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\u3000': // 全角空格
			_, _ = b.WriteRune(' ')
		case r >= '\uFF01' && r <= '\uFF5E': // 全角 ASCII
			_, _ = b.WriteRune(r - 0xFEE0)
		default:
			_, _ = b.WriteRune(r)
		}
	}
	return b.String()
}

// AnswersMatch 判断模型回答里是否出现了期望答案。
//
// 判定口径（与参考实现一致）：
//   - 期望值是数字：回答中出现等值的数字即算命中。
//     "21" 命中 "最少取出 21 颗"，不命中 "29 个"。
//   - 期望值非数字：回答归一化后包含期望值即算命中。
//
// 空期望值一律不命中 —— 没有期望值就没有判分依据，这种情况应该在
// 入口处就被拦掉（logic 题必须带 expected），走到这里说明数据有问题。
func AnswersMatch(expected, response string) bool {
	want := NormalizeAnswer(expected)
	if want == "" {
		return false
	}
	got := NormalizeAnswer(response)
	if got == "" {
		return false
	}

	if wantNum, ok := parseNumber(want); ok {
		for _, raw := range ExtractNumbers(got) {
			if n, ok := parseNumber(NormalizeAnswer(raw)); ok && n == wantNum {
				return true
			}
		}
		return false
	}

	// 非数字答案：按子串判断。期望值可能带标点，双方都已归一化。
	return strings.Contains(got, want)
}

// parseNumber 把字符串解析成数字，失败返回 false。
func parseNumber(s string) (float64, bool) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// LogicItemResult 是逻辑题的判定结果。
type LogicItemResult struct {
	Verdict string
	Passed  bool
	Detail  string
}

// JudgeLogic 判定一道逻辑题。
//
// 三种结果：
//   - ok：命中期望答案
//   - degraded：上游有正常回答，但没命中 —— 这是「降智」的核心证据
//   - unknown：上游没给出可用内容（空响应/报错），不能据此判定降智
//
// unknown 与 degraded 必须分开：上游超时是服务问题，答错是能力问题。
// 把超时算成降智会让通过率虚低，而把答错算成 unknown 又会掩盖降智。
func JudgeLogic(expected, response, errorKind, errorMessage string) LogicItemResult {
	if strings.TrimSpace(response) == "" {
		detail := errorMessage
		if detail == "" {
			detail = "上游未返回内容，请检查接口服务"
		}
		return LogicItemResult{
			Verdict: ItemVerdictUnknown,
			Passed:  false,
			Detail:  detail,
		}
	}

	if AnswersMatch(expected, response) {
		return LogicItemResult{
			Verdict: ItemVerdictOK,
			Passed:  true,
			Detail:  "命中答案 " + NormalizeAnswer(expected),
		}
	}

	// 未命中：把回答里出现过的数字列出来，让用户自己判断是「算错了」
	// 还是「根本没在算」。这是参考实现 detail 的形态，很有用 ——
	// 只说「未通过」的话，用户无法区分降智与题目歧义。
	numbers := ExtractNumbers(response)
	const maxListed = 12
	listed := numbers
	suffix := ""
	if len(numbers) > maxListed {
		listed = numbers[:maxListed]
		suffix = " 等"
	}
	detail := fmt.Sprintf("未出现期望答案 %s", NormalizeAnswer(expected))
	if len(listed) > 0 {
		detail += "，回答中的数字：" + strings.Join(listed, ", ") + suffix
	}

	return LogicItemResult{
		Verdict: ItemVerdictDegraded,
		Passed:  false,
		Detail:  detail,
	}
}

// RenderItemResult 是生成题的判定结果。
type RenderItemResult struct {
	Verdict string
	Passed  bool
	Detail  string
	Artwork string
}

// JudgeRender 判定一道生成题。
//
// 生成题不判「对不对」，只判「画出来没有」：
//   - ok：拿到了结构完整、可渲染的 SVG
//   - generated：拿到了 SVG，但不做智商判定（纯生成模式的单题形态）
//   - degraded：有响应但抽不出可用 SVG（结构退化）
//   - unknown：上游没给内容（超时/报错）
//
// 这里不启用 render_verdict 的独立判定权重 —— 生成题通过与否由
// 调用方根据「本次是否同时选了逻辑题」决定聚合方式。单题只回答一件事：
// 这张图能不能用。
func JudgeRender(response, errorKind, errorMessage string, extract func(string) string) RenderItemResult {
	if strings.TrimSpace(response) == "" {
		detail := errorMessage
		if detail == "" {
			detail = "上游未返回内容，请检查接口服务"
		}
		return RenderItemResult{
			Verdict: ItemVerdictUnknown,
			Detail:  detail,
		}
	}

	artwork := ""
	if extract != nil {
		artwork = extract(response)
	}
	if strings.TrimSpace(artwork) == "" {
		return RenderItemResult{
			Verdict: ItemVerdictDegraded,
			Detail:  "响应中未找到完整的 SVG 作品",
		}
	}

	return RenderItemResult{
		Verdict: ItemVerdictOK,
		Passed:  true,
		Detail:  "已生成作品",
		Artwork: artwork,
	}
}

// AggregateRunVerdict 聚合出运行级判定。
//
// 规则（严格照抄参考实现的口径）：
//
//  1. 没有任何逻辑题（logicTotal==0）且跑过生成题 → generated
//     纯生成模式，逻辑题一道没跑，谈「正常/降智」没有依据。
//
//  2. 有逻辑题但一道都没拿到可用内容 → unknown
//     全部 unknown 意味着上游根本没响应，这不是降智证据。
//
//  3. 逻辑题全部命中 → normal
//
//  4. 其余 → degraded
//     包括「部分命中」与「全部答错」。只要有可用的回答且没全对，
//     就是至少一项不达标。
func AggregateRunVerdict(logicPassed, logicTotal, logicUnknown int) string {
	if logicTotal == 0 {
		// 纯生成模式。调用方保证确实跑了生成题才会走到这里；
		// 连生成题都没跑的运行在服务层就被拒绝，不会落库。
		return VerdictGenerated
	}
	if logicUnknown >= logicTotal {
		return VerdictUnknown
	}
	if logicPassed >= logicTotal {
		return VerdictNormal
	}
	return VerdictDegraded
}

// AggregateRenderVerdict 聚合出生成题判定。
//
// 返回空串表示本次没跑生成题（与「跑了但失败」的 unknown 区分开）。
func AggregateRenderVerdict(total, ok, unknown int) string {
	if total == 0 {
		return RenderVerdictNone
	}
	if unknown >= total {
		return RenderVerdictUnknown
	}
	if ok >= total {
		return RenderVerdictOK
	}
	// 有响应但没全部成功：结构退化。
	return RenderVerdictDegraded
}

// PassRate 计算通过率：normal 数 ÷ 已完成数。
//
// cancelled 不计入分母 —— 用户主动停掉的检测不该拉低通过率，
// 但它的单题结果仍然有效（前端会提示这一点）。
func PassRate(normalCount, completedCount int64) float64 {
	if completedCount <= 0 {
		return 0
	}
	return float64(normalCount) / float64(completedCount)
}
