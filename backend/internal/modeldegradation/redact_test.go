//go:build unit

package modeldegradation

import (
	"strings"
	"testing"
)

// TestRedactSecrets 是凭据外泄防线在「文本」这一层的直接证据。
//
// 为什么需要它：错误提示会带上游原文片段，而这段文案会被存进
// model_degradation_runs.error_message，再出现在所有人可见的历史墙上。
// 我们的代码从不把 Key 放进 URL 或日志，但只要上游把收到的鉴权头
// 原样回显在错误信息里，Key 就会顺着这条路径被公开出去。
// 所以脱敏必须落在「写库之前」这一层，而不是指望上游不犯错。
func TestRedactSecrets(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // 期望结果里必须出现
		deny []string
	}{
		{
			name: "Bearer 头被回显",
			in:   "bad key Bearer sk-test-key-123",
			want: "Bearer ***",
			deny: []string{"sk-test-key-123"},
		},
		{
			name: "小写 bearer",
			in:   "invalid authorization: bearer abcdefghijklmnop",
			want: "***",
			deny: []string{"abcdefghijklmnop"},
		},
		{
			name: "api_key 键值对",
			in:   `{"error":"invalid api_key=sk-live-9f8e7d6c5b4a"}`,
			want: "***",
			deny: []string{"sk-live-9f8e7d6c5b4a"},
		},
		{
			name: "x-api-key 带连字符",
			in:   `x-api-key: sk-ant-api03-XyZ0123456789abcdef`,
			want: "***",
			deny: []string{"sk-ant-api03-XyZ0123456789abcdef"},
		},
		{
			name: "裸奔的长随机串",
			in:   "token 9f8e7d6c5b4a39281706ffeeccddbbaa rejected",
			want: "***",
			deny: []string{"9f8e7d6c5b4a39281706ffeeccddbbaa"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactSecrets(tc.in)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("脱敏后应含 %q，实际 %q", tc.want, got)
			}
			for _, d := range tc.deny {
				if strings.Contains(got, d) {
					t.Fatalf("仍泄漏了 %q：%q", d, got)
				}
			}
		})
	}
}

// TestRedactSecrets_KeepsUsefulText 保证脱敏不把提示本身抹没了。
//
// 如果整段都被替换成 ***，用户就失去「上游到底说了什么」的信息，
// 那和直接不显示错误没区别 —— 脱敏要精准，不能一刀切。
func TestRedactSecrets_KeepsUsefulText(t *testing.T) {
	in := "上游返回 401: invalid api key Bearer sk-abc123def456"
	got := RedactSecrets(in)

	for _, keep := range []string{"401", "invalid api key"} {
		if !strings.Contains(got, keep) {
			t.Fatalf("不该抹掉有用信息 %q，实际 %q", keep, got)
		}
	}
	if strings.Contains(got, "sk-abc123def456") {
		t.Fatalf("Key 未脱敏: %q", got)
	}
}

// TestRedactSecrets_EmptyAndPlain 保证边界情况不 panic、不乱改。
func TestRedactSecrets_EmptyAndPlain(t *testing.T) {
	if got := RedactSecrets(""); got != "" {
		t.Fatalf("空串应原样返回，实际 %q", got)
	}
	plain := "模型不存在，请检查地址和协议"
	if got := RedactSecrets(plain); got != plain {
		t.Fatalf("普通中文不该被改动，%q -> %q", plain, got)
	}
}

// TestRedactSecrets_DoesNotEatNormalWords 防止把正常单词当 Key 抹掉。
//
// 规则里有一条「≥20 位字母数字」，如果边界写错，会把普通英文单词
// 或模型名一起抹掉，用户看到的提示就变成一串 ***，反而更难排查。
func TestRedactSecrets_DoesNotEatNormalWords(t *testing.T) {
	for _, keep := range []string{
		"gpt-4o",
		"claude-sonnet-4-20250514",
		"模型不存在",
		"接口或模型不存在，请检查地址和协议",
	} {
		if got := RedactSecrets(keep); strings.Contains(got, "***") {
			t.Fatalf("不该抹掉 %q，实际 %q", keep, got)
		}
	}
}
