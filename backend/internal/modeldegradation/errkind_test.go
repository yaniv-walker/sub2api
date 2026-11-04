//go:build unit

package modeldegradation

import (
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// TestClassifyByStatus_CoversAllKinds 锁住「状态码 → 错误类型」的映射。
//
// 这条链路曾经整体失效：executor 把 err 传成 nil 去调
// iqbenchmark.ClassifyErrorKind，而那个函数一进门就判 err == nil
// 并直接返回 generic —— 于是 401/403/404/429/402/5xx 全部被压成
// 「上游请求失败，请稍后重试或联系接口服务商」，用户永远看不到
// 「请检查 API Key」这种真正能解决问题的提示。
//
// 后果不是崩溃，而是「提示永远没用」—— 这种缺陷不会报错，
// 只会让人反复怀疑自己填的地址和 Key，所以必须用测试钉死。
func TestClassifyByStatus_CoversAllKinds(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   string
	}{
		{"401 钥匙不对", http.StatusUnauthorized, iqbenchmark.ErrorKindAuthFailed},
		{"403 钥匙对但不许", http.StatusForbidden, iqbenchmark.ErrorKindForbidden},
		{"404 路径或模型不存在", http.StatusNotFound, iqbenchmark.ErrorKindNotFound},
		{"408 网关超时", http.StatusRequestTimeout, iqbenchmark.ErrorKindGatewayTimeout},
		{"429 太快了", http.StatusTooManyRequests, iqbenchmark.ErrorKindRateLimited},
		{"402 没钱了", http.StatusPaymentRequired, iqbenchmark.ErrorKindQuotaExceeded},
		{"500 上游炸了", http.StatusInternalServerError, iqbenchmark.ErrorKindServerError},
		{"502 上游网关坏", http.StatusBadGateway, iqbenchmark.ErrorKindServerError},
		{"503 上游不可用", http.StatusServiceUnavailable, iqbenchmark.ErrorKindServerError},
		{"504 网关等待超时", http.StatusGatewayTimeout, iqbenchmark.ErrorKindGatewayTimeout},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyByStatus(tc.status)
			if got != tc.want {
				t.Fatalf("status %d → %q, want %q", tc.status, got, tc.want)
			}
			// 不允许退化成 generic —— 那正是修之前的错误行为。
			if got == iqbenchmark.ErrorKindGeneric {
				t.Fatalf("status %d 退化成了 generic，说明状态码分支没走到", tc.status)
			}
		})
	}
}

// TestClassifyByStatus_UnknownFallsBackToGeneric 保证陌生状态码有兜底。
func TestClassifyByStatus_UnknownFallsBackToGeneric(t *testing.T) {
	if got := classifyByStatus(418); got != iqbenchmark.ErrorKindGeneric {
		t.Fatalf("未知状态码应兜底为 generic，实际 %q", got)
	}
}

// TestUserFacingError_EveryKindHasDistinctText 保证 12 类文案不重样。
//
// 如果两类错误给出同一句话，用户就无法从提示里区分该改 Key
// 还是该等一等 —— 提示等于没写。
func TestUserFacingError_EveryKindHasDistinctText(t *testing.T) {
	seen := make(map[string]string)
	for _, kind := range iqbenchmark.ErrorKinds {
		text := userFacingError(kind)
		if text == "" {
			t.Fatalf("%q 没有对应文案", kind)
		}
		if prev, dup := seen[text]; dup {
			t.Fatalf("%q 与 %q 文案重复: %q", kind, prev, text)
		}
		seen[text] = kind
	}
	if len(seen) != len(iqbenchmark.ErrorKinds) {
		t.Fatalf("文案数 %d 与错误类型数 %d 不一致", len(seen), len(iqbenchmark.ErrorKinds))
	}
}

// TestUserFacingError_TimeoutDoesNotBlameModel 是「超时 ≠ 降智」的文案层证据。
//
// 超时是上游慢，不是模型变傻。文案里不能出现任何暗示「质量问题」
// 的词，否则用户会把网络问题当成模型降智来投诉。
func TestUserFacingError_TimeoutDoesNotBlameModel(t *testing.T) {
	text := userFacingError(iqbenchmark.ErrorKindTimeout)
	for _, banned := range []string{"降智", "变笨", "质量", "答错"} {
		// 用 strings.Contains 而不是包内的 contains ——
		// 后者的签名是 contains([]string, string)，做的是集合成员判断。
		if strings.Contains(text, banned) {
			t.Fatalf("超时提示里出现了归因于模型的词 %q: %q", banned, text)
		}
	}
}
