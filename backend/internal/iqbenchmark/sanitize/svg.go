// Package sanitize 负责把模型输出当作不可信内容处理。
//
// 两层防线：
//  1. 入库前静态扫描（本包）：识别并标记危险构造，阻断类直接判为 blocked，
//     可剥离类剥离后入库；
//  2. 前端渲染前消毒（DOMPurify）：必须再过一遍，本包不替代它。
//
// 危险构造清单与判定思路源自 MIT 许可的 delirious6423/pelican-bench
// （harness/pelican.py 的 _FLAGS 表）与 joeseesun/llm-case-benchmark
// （public/output-classifier.js 的形态判定）。版权声明见同目录
// THIRD_PARTY_NOTICES.md。
package sanitize

import (
	"regexp"
	"strings"
)

// 危险构造标记键名。这些键会写入 artifacts.unsafe_flags，
// 公开侧永不下发（只暴露 is_unsafe 布尔位）。
const (
	FlagScript           = "script"
	FlagEventHandler     = "event_handler"
	FlagForeignObject    = "foreign_object"
	FlagJavascriptURL    = "javascript_url"
	FlagEntityDecl       = "entity_decl"
	FlagDoctype          = "doctype"
	FlagEmbedObjectFrame = "embed_object_iframe"
	FlagCSSImport        = "css_import"
	FlagDataURI          = "data_uri"
	FlagExternalRef      = "external_ref"
)

// blockFlags 是命中即整条判为 blocked 的构造。
//
// 这些构造的共同点：无法通过局部剥离安全降级 —— 一旦出现，说明输出内容
// 本身试图执行代码或引入外部执行上下文，保留任何部分都可能被绕过。
var blockFlags = map[string]struct{}{
	FlagScript:           {},
	FlagForeignObject:    {},
	FlagJavascriptURL:    {},
	FlagEntityDecl:       {},
	FlagEmbedObjectFrame: {},
}

// stripFlags 是可以通过剥离局部片段保留主体的构造。
var stripFlags = map[string]struct{}{
	FlagEventHandler: {},
	FlagDoctype:      {},
	FlagCSSImport:    {},
	FlagDataURI:      {},
	FlagExternalRef:  {},
}

// 编译一次，全局复用（正则表来自 pelican-bench 的 _FLAGS，顺序保持不变便于比对）。
var (
	reScript           = regexp.MustCompile(`(?i)<script\b`)
	reEventHandler     = regexp.MustCompile(`(?i)\son[a-z]+\s*=`)
	reForeignObject    = regexp.MustCompile(`(?i)<foreignObject\b`)
	reJavascriptURL    = regexp.MustCompile(`(?i)javascript\s*:`)
	reEntityDecl       = regexp.MustCompile(`(?i)<!ENTITY`)
	reDoctype          = regexp.MustCompile(`(?i)<!DOCTYPE`)
	reEmbedObjectFrame = regexp.MustCompile(`(?i)<(iframe|object|embed)\b`)
	reCSSImport        = regexp.MustCompile(`(?i)@import`)
	reDataURI          = regexp.MustCompile(`(?i)(?:href|src)\s*=\s*["']\s*data:`)

	// 外链引用：href/xlink:href/src 指向非锚点资源。
	// 内联 `#id` 引用（渐变、裁剪路径等）是 SVG 的正常写法，必须放行。
	reExternalRef = regexp.MustCompile(`(?i)(?:href|xlink:href|src)\s*=\s*["']\s*(?:https?:|//|ftp:)`)

	// 剥离用的替换规则。
	reStripEventHandler = regexp.MustCompile(`(?i)\s+on[a-z]+\s*=\s*(?:"[^"]*"|'[^']*'|[^\s>]+)`)
	reStripDoctype      = regexp.MustCompile(`(?i)<!DOCTYPE[^>]*>`)
	reStripCSSImport    = regexp.MustCompile(`(?i)@import[^;]*;?`)
	reStripDataURI      = regexp.MustCompile(`(?i)((?:href|src)\s*=\s*)(?:"data:[^"]*"|'data:[^']*'|data:[^\s>]+)`)
	reStripExternalRef  = regexp.MustCompile(`(?i)\s+(?:href|xlink:href|src)\s*=\s*(?:"[^"]*\b(?:https?:|//|ftp:)[^"]*"|'[^']*\b(?:https?:|//|ftp:)[^']*')`)
)

// Result 是扫描结果。
type Result struct {
	// Content 是处理后的正文（剥离类构造已移除；阻断类构造保留原文不处理）。
	Content string
	// Flags 是命中的全部标记（去重且顺序稳定）。
	Flags []string
	// Blocked 表示命中阻断类构造，整条必须判为 blocked。
	Blocked bool
	// Changed 表示正文已被修改（与入参不同）。
	Changed bool
}

// ScanSVG 扫描并净化一段 SVG 文本。
//
// 行为：
//   - 命中阻断类构造（script / foreignObject / javascript: / <!ENTITY / iframe|object|embed）
//     → 返回 Blocked=true，Content 保持原文（供管理员审阅，公开侧不下发）；
//   - 命中可剥离类构造（事件属性 / DOCTYPE / @import / data: URI / 外链引用）
//     → 剥离后返回，Flags 记录命中项；
//   - 无命中 → Content 与入参一致，Changed=false。
func ScanSVG(input string) Result {
	return scan(input, true)
}

// ScanHTML 扫描并净化一段 HTML 文本（规则集与 SVG 相同，另加外链引用检查）。
//
// HTML 与 SVG 共用同一套危险构造判定：两者的攻击面（脚本、事件属性、
// 内嵌对象、外部引用）本质一致，分别实现两套规则只会让其中一套先失修。
func ScanHTML(input string) Result {
	return scan(input, true)
}

// ScanText 只做扫描不做剥离，用于纯文本渲染路径（纯文本不会被解析，
// 但命中阻断类构造仍要打标，便于管理员发现模型在试图注入）。
func ScanText(input string) Result {
	return scan(input, false)
}

func scan(input string, allowStrip bool) Result {
	res := Result{Content: input}
	if strings.TrimSpace(input) == "" {
		return res
	}

	seen := make(map[string]struct{}, 8)
	addFlag := func(name string) {
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		res.Flags = append(res.Flags, name)
	}

	// 阻断类：只记录，不改正文。
	if reScript.MatchString(input) {
		addFlag(FlagScript)
		res.Blocked = true
	}
	if reForeignObject.MatchString(input) {
		addFlag(FlagForeignObject)
		res.Blocked = true
	}
	if reJavascriptURL.MatchString(input) {
		addFlag(FlagJavascriptURL)
		res.Blocked = true
	}
	if reEntityDecl.MatchString(input) {
		addFlag(FlagEntityDecl)
		res.Blocked = true
	}
	if reEmbedObjectFrame.MatchString(input) {
		addFlag(FlagEmbedObjectFrame)
		res.Blocked = true
	}

	// 可剥离类：命中后按需剥离。
	if reEventHandler.MatchString(input) {
		addFlag(FlagEventHandler)
	}
	if reDoctype.MatchString(input) {
		addFlag(FlagDoctype)
	}
	if reCSSImport.MatchString(input) {
		addFlag(FlagCSSImport)
	}
	if reDataURI.MatchString(input) {
		addFlag(FlagDataURI)
	}
	if reExternalRef.MatchString(input) {
		addFlag(FlagExternalRef)
	}

	if !allowStrip || res.Blocked {
		// 阻断类不让剥离：任何「净化后保留」的尝试都可能被构造绕过，
		// 直接整条判 blocked，由前端展示占位卡片。
		return res
	}

	out := input
	out = reStripEventHandler.ReplaceAllString(out, "")
	out = reStripDoctype.ReplaceAllString(out, "")
	out = reStripCSSImport.ReplaceAllString(out, "")
	out = reStripDataURI.ReplaceAllString(out, "$1\"\"")
	out = reStripExternalRef.ReplaceAllString(out, "")
	if out != input {
		res.Content = out
		res.Changed = true
	}
	return res
}

// HasBlockFlag 判断一组标记里是否含阻断类构造。
func HasBlockFlag(flags []string) bool {
	for _, f := range flags {
		if _, ok := blockFlags[f]; ok {
			return true
		}
	}
	return false
}

// IsStripFlag 判断某个标记是否为可剥离类。
func IsStripFlag(flag string) bool {
	_, ok := stripFlags[flag]
	return ok
}

// SafeSummary 把内部错误码翻译为公开侧可下发的脱敏摘要。
//
// 公开侧永不下发内部错误码与原始错误文本：它们可能包含上游供应商名称、
// 账号信息或内部路由细节。这里只给出用户能理解的状态描述。
func SafeSummary(errorCode string) string {
	switch errorCode {
	case ErrCodeTimeout:
		return "生成超时，未在限定时间内完成"
	case ErrCodeRateLimited:
		return "上游限流，本次未完成"
	case ErrCodeContentBlocked:
		return "内容未通过安全审核"
	case ErrCodeUnsafeContent:
		return "作品包含不安全内容，已隐藏"
	case ErrCodeNoArtifact:
		return "模型未产出可识别的内容"
	case ErrCodeInsufficientBalance:
		return "检测额度不足"
	case ErrCodeStorageDisabled:
		return "作品存储暂不可用"
	case ErrCodeTruncated:
		return "作品超出大小限制，已截断"
	case ErrCodeCapabilityMissing:
		return "该模型不支持本题目所需能力"
	case ErrCodeGroupMissing, ErrCodeGatewayUnreachable:
		return "检测通道暂不可用"
	case ErrCodeUpstreamError:
		return "上游服务异常"
	case "":
		return ""
	default:
		// 未知码不泄露原文，给通用描述。
		return "检测未成功完成"
	}
}

// 与 types.go 中的内部错误码保持一致的别名，避免本包反向依赖主包常量
// 时产生循环引用（本包只依赖字符串本身）。
const (
	ErrCodeTimeout             = "timeout"
	ErrCodeRateLimited         = "rate_limited"
	ErrCodeContentBlocked      = "content_blocked"
	ErrCodeUnsafeContent       = "unsafe_content"
	ErrCodeNoArtifact          = "no_artifact"
	ErrCodeInsufficientBalance = "insufficient_balance"
	ErrCodeStorageDisabled     = "storage_disabled"
	ErrCodeTruncated           = "truncated"
	ErrCodeCapabilityMissing   = "capability_missing"
	ErrCodeGroupMissing        = "group_missing"
	ErrCodeGatewayUnreachable  = "gateway_unreachable"
	ErrCodeUpstreamError       = "upstream_error"
)
