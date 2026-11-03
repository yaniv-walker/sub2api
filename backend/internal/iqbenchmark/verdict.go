package iqbenchmark

import (
	"strings"
	"unicode"
)

// 本文件是「判定」的纯函数层：输入一次执行的客观结果，输出四态。
//
// 放在包根而不是 service 包，有两个理由：
//   - 它是领域规则，不是编排逻辑，不该依赖 Store / Gateway 之类的东西；
//   - 纯函数才能被穷举测试。判定是这一版最容易出错的地方
//     （错一个分支，整条通过率曲线就是错的），必须能一一定住。
//
// 设计要点：所有函数都是 fail-closed 的。任何「说不清」的情况
// 一律落到 degraded 或 unknown，绝不乐观地判成 ok。
// 一个假的「模型没变笨」比一个假的「模型变笨了」危害大得多，
// 因为前者会让人放心的继续用。

// VerdictForCase 计算一道题产出的判定。
//
// 入参是执行后的客观事实，不做任何 I/O：
//
//	status     调用结果（success / failed / timeout / blocked）
//	c          题目；用于取 Expected。可为 nil（题目已被删除）
//	text       返回正文（图片题为空）
//	hasArtifact 是否真的落了产出（有正文或图片 URL）
//
// 判定顺序（先排除、后判分，绝不跳步）：
//
//  1. 调用本身失败 / 超时 / 被拦截 → degraded。没有产出就谈不上答对。
//  2. 题目无 Expected → generated（有产出）或 unknown（无产出）。
//     这一步必须在判分之前：没有参考答案时"看起来对"不构成通过。
//  3. 有 Expected 但正文为空 → degraded。空答案永远不算通过。
//  4. 有 Expected 且等值比较通过 → ok，否则 degraded。
func VerdictForCase(status string, c *Case, text string, hasArtifact bool) string {
	switch status {
	case ArtifactStatusSuccess:
		// 继续往下判。
	case ArtifactStatusFailed, ArtifactStatusTimeout, ArtifactStatusBlocked:
		// 三种都没能产出可判内容，一律降级。
		//
		// 被拦截（blocked）尤其不能算通过：它压根没通过安全策略，
		// 谈不上"答得好"；也不算 unknown，因为确实执行过。
		return VerdictDegraded
	default:
		// 未知状态（例如未来新增）也归为 degraded，而不是 unknown：
		// unknown 的语义是"没有产出"，而这里显然是执行过了。
		return VerdictDegraded
	}

	if !hasArtifact {
		return VerdictUnknown
	}
	if c == nil || strings.TrimSpace(c.Expected) == "" {
		return VerdictGenerated
	}
	if strings.TrimSpace(text) == "" {
		return VerdictDegraded
	}
	if AnswersMatch(text, c.Expected) {
		return VerdictOK
	}
	return VerdictDegraded
}

// VerdictForRender 计算渲染形态的判定。
//
// 与答案判定分开，因为它们是两类不同的降级：
// 模型可能答对了但把该出的 SVG 吐成了纯文本，也可能图出得漂亮但答案是错的。
// 合并成一个字段就必然有一类信息被掩盖。
//
//	expectKind  题目期望的渲染形态
//	renderKind  实际判定的渲染形态（sanitize.Classify 的结果）
//	hasArtifact 是否真的落了产出
func VerdictForRender(status, expectKind, renderKind string, hasArtifact bool) string {
	if status != ArtifactStatusSuccess || !hasArtifact {
		// 没有产出时，"渲染得好不好"无从谈起。
		return VerdictUnknown
	}
	if strings.TrimSpace(renderKind) == "" {
		return VerdictUnknown
	}
	if strings.TrimSpace(expectKind) == "" {
		// 题目没规定形态：任何可识别形态都算合格。
		return RenderVerdictOK
	}
	if renderKind == expectKind {
		return RenderVerdictOK
	}
	return RenderVerdictDegraded
}

// AggregateRunVerdict 把一次 run 下所有产出的判定汇总成 run 级四态。
//
// 规则与参考实现对齐：
//
//	logicTotal == 0              → generated（纯生成模式：这一轮只出图不判分）
//	passed == logicTotal         → normal
//	passed == 0 且 total > 0     → degraded
//	其他                         → degraded（部分通过也是降级）
//
// 注意 total 与 logicTotal 的区别：total 是产出总数，logicTotal 是可判题目数。
// 只有 generated 的 run（全是图片题）total 大于 0 而 logicTotal 为 0，
// 这正是"纯生成模式"的判定依据，也是参考实现里
// verdict === "generated" && logic_total === 0 那条特例的来源。
func AggregateRunVerdict(total, logicTotal, logicPassed int) string {
	if total <= 0 {
		// 一条产出都没有：不是"通过"，也不是"降级"，是没测成。
		return RunVerdictUnknown
	}
	if logicTotal <= 0 {
		return RunVerdictGenerated
	}
	if logicPassed >= logicTotal {
		return RunVerdictNormal
	}
	return RunVerdictDegraded
}

// AnswersMatch 比较模型回答与参考答案。
//
// 刻意做成保守的归一化等值比较，不做语义相似：
//   - 语义相似需要 LLM 裁判，而 LLMJudgeEnabled 目前是恒 false 的开关，
//     真启用它会引入"裁判自己也会漂移"的二阶问题；
//   - 归一化等值虽然严，但它可重现、可解释，模型变了就是变了。
//
// 归一化只做三件不改变语义的事：
//   - 全角转半角（中文标点在模型输出里极常见）；
//   - 折叠连续空白并去首尾；
//   - 忽略大小写。
//
// 刻意**不**做的事：去标点。去标点会让 "3.14" 与 "314" 相等，
// 也会让 "不是" 与 "是" 在特定切分下混淆 —— 宁可判错，不可判成假通过。
func AnswersMatch(got, expected string) bool {
	return normalizeAnswer(got) == normalizeAnswer(expected)
}

// normalizeAnswer 是 AnswersMatch 用的归一化。
func normalizeAnswer(s string) string {
	s = foldFullWidth(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.ToLower(s)
}

// foldFullWidth 把全角 ASCII 与全角空格折成半角。
//
// 中文模型输出里 "１２３"、"（" 这类全角字符很常见，
// 不折算会让正确答案被判错 —— 那是假降级，同样有害。
//
// 用 []rune 而不是 strings.Builder：Builder.WriteRune 永远返回 nil error，
// 逐处丢弃返回值只会招来 errcheck 告警，反而掩盖真正需要检查的错误。
func foldFullWidth(s string) string {
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '\u3000': // 全角空格
			runes[i] = ' '
		case r >= '\uFF01' && r <= '\uFF5E':
			// 全角 ASCII 区间，与半角相差 0xFEE0。
			runes[i] = r - 0xFEE0
		case unicode.IsSpace(r) && r != ' ':
			// 把各种 Unicode 空白统一成半角空格，
			// 这样后面的 Fields 才能一并折叠。
			runes[i] = ' '
		}
	}
	return string(runes)
}
