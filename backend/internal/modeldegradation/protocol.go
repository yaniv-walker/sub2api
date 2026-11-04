package modeldegradation

import (
	"fmt"
	"net/url"
	"strings"
)

// 本文件把「协议」翻译成具体的 HTTP 请求形态。
//
// 三种协议的真实差异（全部来自参考实现的文案与响应，不是猜的）：
//
//	chat_completions  POST {base}/v1/chat/completions
//	                  Authorization: Bearer <key>
//	                  reasoning_effort 作为顶层字段发送
//	                  非流式；长输出（生成题）容易在等待期被判超时
//
//	responses         POST {base}/v1/responses
//	                  Authorization: Bearer <key>
//	                  reasoning.effort 嵌套发送
//
//	messages          POST {base}/v1/messages
//	                  x-api-key: <key>
//	                  不接受 reasoning_effort（发了也无效，反而可能 400）
//
// 版本段拼接规则（参考实现文案明确写了）：
//   - Base URL 里已经带版本段（/v1、/v4 之类）就不再补 /v1
//   - 拼出来的路径返回 404/405 时，自动改试「不带 /v1」的变体
//     因为有的用户填的是 https://api.example.com/v1，有的填 https://api.example.com

// Endpoint 描述一次请求的目标与鉴权方式。
type Endpoint struct {
	// Path 是 BASE 之后的路径，例如 /v1/chat/completions。
	Path string
	// AuthHeader / AuthPrefix 决定怎么带密钥。
	AuthHeader string
	AuthPrefix string
}

// versionSegmentPattern 判断路径里是否已有版本段（v1 / v2 / v4 / v1beta 等）。
//
// 只认「整段」形态：/v1/xxx、/v4、/v1beta。不用 Contains("/v1") 是因为
// /v10 或 /api/v1abc 也会命中，那会让我们错误地不再补版本段。
func hasVersionSegment(path string) bool {
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		return false
	}
	segments := strings.Split(trimmed, "/")
	for _, seg := range segments {
		if seg == "" {
			continue
		}
		if len(seg) >= 2 && (seg[0] == 'v' || seg[0] == 'V') {
			rest := seg[1:]
			allDigit := true
			for _, r := range rest {
				if r < '0' || r > '9' {
					allDigit = false
					break
				}
			}
			if allDigit {
				return true
			}
			// 允许 v1beta / v1alpha 这类后缀形态。
			if strings.HasPrefix(rest, "1beta") || strings.HasPrefix(rest, "1alpha") {
				return true
			}
		}
	}
	return false
}

// BuildEndpoints 返回该协议应尝试的候选路径（按优先级）。
//
// 返回多个候选是为了实现「404/405 自动回退」：第一个是标准形态，
// 第二个是不带版本段的形态。调用方按顺序试，第一个非 404/405 的结果即采用。
func BuildEndpoints(base *url.URL, protocol string) ([]Endpoint, error) {
	if base == nil {
		return nil, ErrBaseURLInvalid
	}

	basePath := strings.TrimRight(base.Path, "/")

	var tail string
	var authHeader, authPrefix string
	switch protocol {
	case ProtocolChatCompletions:
		tail = "/chat/completions"
		authHeader, authPrefix = "Authorization", "Bearer "
	case ProtocolResponses:
		tail = "/responses"
		authHeader, authPrefix = "Authorization", "Bearer "
	case ProtocolMessages:
		tail = "/messages"
		authHeader, authPrefix = "x-api-key", ""
	default:
		return nil, fmt.Errorf("modeldegradation: 未知协议 %q", protocol)
	}

	// 标准形态：已有版本段就直接拼 tail，否则补 /v1。
	var primary string
	if hasVersionSegment(basePath) {
		primary = basePath + tail
	} else {
		primary = basePath + "/v1" + tail
	}

	candidates := []Endpoint{{
		Path:       primary,
		AuthHeader: authHeader,
		AuthPrefix: authPrefix,
	}}

	// 回退形态：不带 /v1，直接 base + tail。
	// 只在主形态确实补了 /v1 时才需要 —— 若用户自己就带了版本段，
	// 回退没有意义（两种情况路径相同）。
	fallback := basePath + tail
	if fallback != primary {
		candidates = append(candidates, Endpoint{
			Path:       fallback,
			AuthHeader: authHeader,
			AuthPrefix: authPrefix,
		})
	}

	return candidates, nil
}

// RequestBody 构造该协议的请求体。
//
// model、prompt、effort 三者按协议差异放置：
//   - chat_completions: messages + reasoning_effort（顶层）
//   - responses:        input   + reasoning.effort（嵌套）
//   - messages:         messages + max_tokens（不接受 effort）
//
// 非流式：本功能需要完整结果才能判分，流式反而要自己拼装。
func RequestBody(protocol, model, prompt, effort string) map[string]any {
	body := map[string]any{
		"model":  model,
		"stream": false,
	}

	switch protocol {
	case ProtocolChatCompletions:
		body["messages"] = []map[string]any{
			{"role": "user", "content": prompt},
		}
		// 只在协议支持且用户确实指定时才发。多发一个不认识的字段
		// 会让某些上游直接 400，那个失败会被误归一化成 server_error。
		if effort != "" && SupportsReasoningEffort(protocol) {
			body["reasoning_effort"] = effort
		}

	case ProtocolResponses:
		body["input"] = []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": prompt},
				},
			},
		}
		if effort != "" && SupportsReasoningEffort(protocol) {
			body["reasoning"] = map[string]any{"effort": effort}
		}

	case ProtocolMessages:
		body["messages"] = []map[string]any{
			{"role": "user", "content": prompt},
		}
		// Anthropic 要求 max_tokens。生成题可能要画很久，给足上限；
		// 逻辑题不需要这么多，但统一值不会有害（只是上限）。
		body["max_tokens"] = 32000
		// messages 协议不接受 reasoning_effort，此处刻意不发。

	default:
		body["messages"] = []map[string]any{
			{"role": "user", "content": prompt},
		}
	}

	return body
}

// ExtractText 从各协议的响应 JSON 里取出模型文本。
//
// 三种协议的响应结构不同，但都遵循「可能在若干位置之一有文本」的形态。
// 这里按协议逐层取，取不到再看通用兜底 —— 宁可多试几个位置，
// 也不要因为结构小差异把「有回答」误判成「空响应」，那会把正常的上游
// 判成 unknown。
func ExtractText(protocol string, payload map[string]any) string {
	switch protocol {
	case ProtocolChatCompletions:
		if s := extractChatCompletions(payload); s != "" {
			return s
		}
	case ProtocolResponses:
		if s := extractResponses(payload); s != "" {
			return s
		}
	case ProtocolMessages:
		if s := extractMessages(payload); s != "" {
			return s
		}
	}
	// 通用兜底：有些上游返回结构不规范，顺着常见字段找一遍。
	return extractGeneric(payload)
}

// extractChatCompletions 取 choices[0].message.content。
func extractChatCompletions(payload map[string]any) string {
	choices, ok := payload["choices"].([]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	first, ok := choices[0].(map[string]any)
	if !ok {
		return ""
	}
	msg, ok := first["message"].(map[string]any)
	if !ok {
		return ""
	}
	return stringContent(msg["content"])
}

// extractResponses 取 output[].content[].text（output_text 是便捷字段）。
func extractResponses(payload map[string]any) string {
	// 优先用官方便捷字段。
	if s, ok := payload["output_text"].(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	output, ok := payload["output"].([]any)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, raw := range output {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		// 只取 message 类型的项；reasoning 等项的 content 不是答案。
		if t, ok := item["type"].(string); ok && t != "message" {
			continue
		}
		content, ok := item["content"].([]any)
		if !ok {
			continue
		}
		for _, c := range content {
			block, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if s, ok := block["text"].(string); ok && s != "" {
				_, _ = b.WriteString(s)
			}
		}
	}
	return b.String()
}

// extractMessages 取 content[] 里 type=text 的 text 字段。
func extractMessages(payload map[string]any) string {
	content, ok := payload["content"].([]any)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if t, ok := block["type"].(string); ok && t != "text" {
			continue
		}
		if s, ok := block["text"].(string); ok {
			_, _ = b.WriteString(s)
		}
	}
	return b.String()
}

// extractGeneric 是无协议信息时的兜底。
func extractGeneric(payload map[string]any) string {
	for _, key := range []string{"output_text", "text", "content", "response"} {
		if s := stringContent(payload[key]); strings.TrimSpace(s) != "" {
			return s
		}
	}
	if s := extractChatCompletions(payload); s != "" {
		return s
	}
	if s := extractResponses(payload); s != "" {
		return s
	}
	if s := extractMessages(payload); s != "" {
		return s
	}
	return ""
}

// stringContent 处理 content 既可能是字符串、也可能是分块数组的情况。
func stringContent(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var b strings.Builder
		for _, raw := range t {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if s, ok := block["text"].(string); ok {
				_, _ = b.WriteString(s)
			}
		}
		return b.String()
	}
	return ""
}
