//go:build unit

package modeldegradation

import (
	"net/url"
	"testing"
)

// 协议适配的每一处差异都会直接导致线上失败：
// 少一个 /v1 就 404，认证头用错就 401，effort 发错协议就 400。
// 这些都不是「可能有问题」，是必然有问题。因此逐项钉死。

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) 失败: %v", raw, err)
	}
	return u
}

func TestBuildEndpoints_PrimaryPaths(t *testing.T) {
	cases := []struct {
		base     string
		protocol string
		wantPath string
		wantAuth string
		wantPref string
	}{
		{
			base:     "https://api.example.com",
			protocol: ProtocolChatCompletions,
			wantPath: "/v1/chat/completions",
			wantAuth: "Authorization",
			wantPref: "Bearer ",
		},
		{
			base:     "https://api.example.com",
			protocol: ProtocolResponses,
			wantPath: "/v1/responses",
			wantAuth: "Authorization",
			wantPref: "Bearer ",
		},
		{
			base:     "https://api.example.com",
			protocol: ProtocolMessages,
			wantPath: "/v1/messages",
			wantAuth: "x-api-key",
			wantPref: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.protocol, func(t *testing.T) {
			eps, err := BuildEndpoints(mustURL(t, tc.base), tc.protocol)
			if err != nil {
				t.Fatalf("BuildEndpoints 失败: %v", err)
			}
			if len(eps) == 0 {
				t.Fatal("至少应有一个候选端点")
			}
			if eps[0].Path != tc.wantPath {
				t.Fatalf("主路径 = %q, want %q", eps[0].Path, tc.wantPath)
			}
			if eps[0].AuthHeader != tc.wantAuth {
				t.Fatalf("认证头 = %q, want %q", eps[0].AuthHeader, tc.wantAuth)
			}
			if eps[0].AuthPrefix != tc.wantPref {
				t.Fatalf("认证前缀 = %q, want %q", eps[0].AuthPrefix, tc.wantPref)
			}
		})
	}
}

func TestBuildEndpoints_ExistingVersionSegment(t *testing.T) {
	// 用户填的 base 已经带了版本段时，不能再补 /v1。
	// 否则会拼出 /v1/v1/chat/completions —— 必然 404。
	cases := []struct {
		base     string
		protocol string
		wantPath string
	}{
		{
			base:     "https://api.example.com/v1",
			protocol: ProtocolChatCompletions,
			wantPath: "/v1/chat/completions",
		},
		{
			base:     "https://api.example.com/v4",
			protocol: ProtocolChatCompletions,
			wantPath: "/v4/chat/completions",
		},
		{
			base:     "https://api.example.com/v1beta",
			protocol: ProtocolMessages,
			wantPath: "/v1beta/messages",
		},
		{
			base:     "https://api.example.com/api/v1",
			protocol: ProtocolResponses,
			wantPath: "/api/v1/responses",
		},
		{
			base:     "https://api.example.com/proxy/v2",
			protocol: ProtocolChatCompletions,
			wantPath: "/proxy/v2/chat/completions",
		},
	}

	for _, tc := range cases {
		t.Run(tc.base, func(t *testing.T) {
			eps, err := BuildEndpoints(mustURL(t, tc.base), tc.protocol)
			if err != nil {
				t.Fatalf("BuildEndpoints 失败: %v", err)
			}
			if eps[0].Path != tc.wantPath {
				t.Fatalf("路径 = %q, want %q", eps[0].Path, tc.wantPath)
			}
		})
	}
}

func TestBuildEndpoints_FallbackCandidate(t *testing.T) {
	// 补了 /v1 的情况下必须准备回退候选，因为有的用户填的 base
	// 已经隐含了 /v1 却没写出来（网关把 /v1 放在服务端）。
	eps, err := BuildEndpoints(mustURL(t, "https://api.example.com"), ProtocolChatCompletions)
	if err != nil {
		t.Fatalf("BuildEndpoints 失败: %v", err)
	}
	if len(eps) < 2 {
		t.Fatalf("应有回退候选，实际只有 %d 个", len(eps))
	}
	if eps[1].Path != "/chat/completions" {
		t.Fatalf("回退路径 = %q, want /chat/completions", eps[1].Path)
	}

	// 已经有版本段时，回退路径与主路径相同，不应重复尝试。
	eps2, err := BuildEndpoints(mustURL(t, "https://api.example.com/v1"), ProtocolChatCompletions)
	if err != nil {
		t.Fatalf("BuildEndpoints 失败: %v", err)
	}
	if len(eps2) != 1 {
		t.Fatalf("已有版本段时不应有重复回退候选，实际 %d 个", len(eps2))
	}
}

func TestHasVersionSegment(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"", false},
		{"/", false},
		{"/v1", true},
		{"/v1/", true},
		{"/v4", true},
		{"/api/v1", true},
		{"/v1beta", true},
		{"/v1alpha", true},
		// 关键：/v10 是版本段，但 /api/v1abc 不是我们认识的形态。
		{"/v10", true},
		{"/version", false},
		{"/api", false},
		{"/v", false},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := hasVersionSegment(tc.path); got != tc.want {
				t.Fatalf("hasVersionSegment(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestRequestBody_ProtocolDifferences(t *testing.T) {
	t.Run("chat_completions 用 messages 且 effort 在顶层", func(t *testing.T) {
		body := RequestBody(ProtocolChatCompletions, "gpt-4", "题目", EffortHigh)

		if body["model"] != "gpt-4" {
			t.Fatalf("model = %v", body["model"])
		}
		if body["stream"] != false {
			t.Fatal("必须非流式：流式拿不到完整结果无法判分")
		}
		if _, ok := body["messages"]; !ok {
			t.Fatal("chat_completions 必须用 messages")
		}
		if body["reasoning_effort"] != EffortHigh {
			t.Fatalf("reasoning_effort = %v, want %q", body["reasoning_effort"], EffortHigh)
		}
	})

	t.Run("responses 用 input 且 effort 嵌套在 reasoning 里", func(t *testing.T) {
		body := RequestBody(ProtocolResponses, "o3", "题目", EffortMedium)

		if _, ok := body["input"]; !ok {
			t.Fatal("responses 必须用 input")
		}
		reasoning, ok := body["reasoning"].(map[string]any)
		if !ok {
			t.Fatalf("reasoning 应为 map，实际 %T", body["reasoning"])
		}
		if reasoning["effort"] != EffortMedium {
			t.Fatalf("reasoning.effort = %v, want %q", reasoning["effort"], EffortMedium)
		}
		// 不应同时出现顶层的 reasoning_effort —— 那是另一个协议的形态。
		if _, ok := body["reasoning_effort"]; ok {
			t.Fatal("responses 不应有顶层 reasoning_effort")
		}
	})

	t.Run("messages 不接受 effort", func(t *testing.T) {
		// Anthropic 收到这个字段会 400，那个 400 会被归一化成 server_error，
		// 让用户以为上游挂了。因此必须完全不发。
		body := RequestBody(ProtocolMessages, "claude-3", "题目", EffortMax)

		if _, ok := body["reasoning_effort"]; ok {
			t.Fatal("messages 不应发送 reasoning_effort")
		}
		if _, ok := body["reasoning"]; ok {
			t.Fatal("messages 不应发送 reasoning")
		}
		// Anthropic 要求 max_tokens。
		if _, ok := body["max_tokens"]; !ok {
			t.Fatal("messages 必须带 max_tokens")
		}
	})

	t.Run("空 effort 时不发任何思考字段", func(t *testing.T) {
		for _, p := range Protocols {
			body := RequestBody(p, "m", "题目", "")
			if _, ok := body["reasoning_effort"]; ok {
				t.Fatalf("%s: 空 effort 不应发送 reasoning_effort", p)
			}
			if _, ok := body["reasoning"]; ok {
				t.Fatalf("%s: 空 effort 不应发送 reasoning", p)
			}
		}
	})
}

func TestExtractText_ChatCompletions(t *testing.T) {
	payload := map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"role":    "assistant",
					"content": "最少取出 21 颗。",
				},
			},
		},
	}
	if got := ExtractText(ProtocolChatCompletions, payload); got != "最少取出 21 颗。" {
		t.Fatalf("ExtractText = %q", got)
	}
}

func TestExtractText_ChatCompletionsContentBlocks(t *testing.T) {
	// 有的上游把 content 返回成分块数组而不是字符串。
	payload := map[string]any{
		"choices": []any{
			map[string]any{
				"message": map[string]any{
					"content": []any{
						map[string]any{"type": "text", "text": "第一段"},
						map[string]any{"type": "text", "text": "第二段"},
					},
				},
			},
		},
	}
	if got := ExtractText(ProtocolChatCompletions, payload); got != "第一段第二段" {
		t.Fatalf("ExtractText = %q", got)
	}
}

func TestExtractText_Responses(t *testing.T) {
	t.Run("优先用 output_text 便捷字段", func(t *testing.T) {
		payload := map[string]any{
			"output_text": "便捷字段内容",
			"output": []any{
				map[string]any{
					"type": "message",
					"content": []any{
						map[string]any{"type": "output_text", "text": "深层内容"},
					},
				},
			},
		}
		if got := ExtractText(ProtocolResponses, payload); got != "便捷字段内容" {
			t.Fatalf("ExtractText = %q", got)
		}
	})

	t.Run("无便捷字段时取 output[].content[].text", func(t *testing.T) {
		payload := map[string]any{
			"output": []any{
				map[string]any{
					"type": "message",
					"content": []any{
						map[string]any{"type": "output_text", "text": "深层内容"},
					},
				},
			},
		}
		if got := ExtractText(ProtocolResponses, payload); got != "深层内容" {
			t.Fatalf("ExtractText = %q", got)
		}
	})

	t.Run("跳过 reasoning 项", func(t *testing.T) {
		// reasoning 项的 content 是思考过程，不是答案。
		// 混进答案里会污染判分（思考过程中可能提到候选数字）。
		payload := map[string]any{
			"output": []any{
				map[string]any{
					"type": "reasoning",
					"content": []any{
						map[string]any{"type": "reasoning_text", "text": "我在想 29 对不对"},
					},
				},
				map[string]any{
					"type": "message",
					"content": []any{
						map[string]any{"type": "output_text", "text": "答案是 21"},
					},
				},
			},
		}
		got := ExtractText(ProtocolResponses, payload)
		if got != "答案是 21" {
			t.Fatalf("ExtractText = %q，不应包含 reasoning 内容", got)
		}
	})
}

func TestExtractText_Messages(t *testing.T) {
	payload := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "答案是 "},
			map[string]any{"type": "text", "text": "21"},
		},
	}
	if got := ExtractText(ProtocolMessages, payload); got != "答案是 21" {
		t.Fatalf("ExtractText = %q", got)
	}
}

func TestExtractText_EmptyPayload(t *testing.T) {
	for _, p := range Protocols {
		if got := ExtractText(p, map[string]any{}); got != "" {
			t.Fatalf("%s: 空 payload 应返回空串，实际 %q", p, got)
		}
	}
}

func TestModelsPathFrom(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"/v1/chat/completions", "/v1/models"},
		{"/v1/responses", "/v1/models"},
		{"/v1/messages", "/v1/models"},
		{"/v1beta/messages", "/v1beta/models"},
		{"/api/v1/chat/completions", "/api/v1/models"},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := modelsPathFrom(tc.in); got != tc.want {
				t.Fatalf("modelsPathFrom(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseModelList(t *testing.T) {
	t.Run("OpenAI 风格 data 数组", func(t *testing.T) {
		raw := []byte(`{"data":[{"id":"gpt-4"},{"id":"gpt-4o"}]}`)
		got, err := parseModelList(raw)
		if err != nil {
			t.Fatalf("parseModelList 失败: %v", err)
		}
		if len(got) != 2 || got[0] != "gpt-4" || got[1] != "gpt-4o" {
			t.Fatalf("got = %v", got)
		}
	})

	t.Run("models 字符串数组", func(t *testing.T) {
		raw := []byte(`{"models":["claude-3","claude-3-5"]}`)
		got, err := parseModelList(raw)
		if err != nil {
			t.Fatalf("parseModelList 失败: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got = %v", got)
		}
	})

	t.Run("顶层数组", func(t *testing.T) {
		raw := []byte(`["a","b"]`)
		got, err := parseModelList(raw)
		if err != nil {
			t.Fatalf("parseModelList 失败: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("got = %v", got)
		}
	})

	t.Run("data 为空数组是合法响应", func(t *testing.T) {
		raw := []byte(`{"data":[]}`)
		got, err := parseModelList(raw)
		if err != nil {
			t.Fatalf("空 data 不应报错: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("got = %v", got)
		}
	})

	t.Run("无法解析的内容报错", func(t *testing.T) {
		raw := []byte(`<html>not json</html>`)
		if _, err := parseModelList(raw); err == nil {
			t.Fatal("非 JSON 应返回错误")
		}
	})
}

func TestSupportsReasoningEffort(t *testing.T) {
	cases := []struct {
		protocol string
		want     bool
	}{
		{ProtocolChatCompletions, true},
		{ProtocolResponses, true},
		{ProtocolMessages, false},
	}
	for _, tc := range cases {
		t.Run(tc.protocol, func(t *testing.T) {
			if got := SupportsReasoningEffort(tc.protocol); got != tc.want {
				t.Fatalf("SupportsReasoningEffort(%q) = %v, want %v", tc.protocol, got, tc.want)
			}
		})
	}
}

func TestBuildEndpoints_UnknownProtocol(t *testing.T) {
	if _, err := BuildEndpoints(mustURL(t, "https://api.example.com"), "bogus"); err == nil {
		t.Fatal("未知协议应返回错误")
	}
}
