//go:build unit

package modeldegradation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// 本文件用真实 httptest 服务器覆盖 executor 的出站调用路径。
//
// 为什么必须有：service 层的测试全用假 store，从不真的发 HTTP 请求。
// 也就是说协议分发、鉴权头、重试、错误归一化这些「真出网」的逻辑
// 之前一行都没有被测过 —— 而这恰好是最容易错、也最不应该错的部分：
// 鉴权头放错位置会有 401，路径拼错会有 404，错误分类错了会把
// 「超时」误报成「降智」。
//
// 注入 HTTPClient 是为了绕开 SSRF 拨号器 —— 拨号器只允许公网 https，
// 而 httptest 起的是回环 http。SSRF 校验本身在 credentials_test.go
// 单独覆盖，这里只测调用层。

// newTestCaller 构造一个指向 httptest 服务器的 caller。
func newTestCaller(t *testing.T, srv *httptest.Server, protocol string) *UpstreamCaller {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("解析测试地址失败: %v", err)
	}
	return &UpstreamCaller{
		BaseURL:    u,
		APIKey:     "sk-test-key",
		Protocol:   protocol,
		Model:      "test-model",
		Timeout:    5 * time.Second,
		HTTPClient: srv.Client(),
	}
}

// TestCall_ChatCompletionsHappyPath 覆盖最常见协议的完整往返。
func TestCall_ChatCompletionsHappyPath(t *testing.T) {
	var gotPath, gotAuth, gotCT string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"答案是 21"}}]}`))
	}))
	defer srv.Close()

	res := newTestCaller(t, srv, ProtocolChatCompletions).Call(context.Background(), "题目")

	if res.ErrorKind != "" {
		t.Fatalf("不应报错，实际 ErrorKind=%q msg=%q", res.ErrorKind, res.ErrorMessage)
	}
	if res.Text != "答案是 21" {
		t.Fatalf("Text = %q, want 答案是 21", res.Text)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200", res.StatusCode)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("路径 = %q, want /v1/chat/completions", gotPath)
	}
	if gotAuth != "Bearer sk-test-key" {
		t.Fatalf("Authorization = %q, want Bearer sk-test-key", gotAuth)
	}
	if gotCT != "application/json" {
		t.Fatalf("Content-Type = %q", gotCT)
	}
	if gotBody["model"] != "test-model" {
		t.Fatalf("请求体 model = %v", gotBody["model"])
	}
	if gotBody["stream"] != false {
		t.Fatalf("必须非流式，stream = %v", gotBody["stream"])
	}
}

// TestCall_ResponsesProtocol 确认 responses 走自己的路径与抽取。
func TestCall_ResponsesProtocol(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"答案是 21"}]}]}`))
	}))
	defer srv.Close()

	res := newTestCaller(t, srv, ProtocolResponses).Call(context.Background(), "题目")
	if res.Text != "答案是 21" {
		t.Fatalf("Text = %q, want 答案是 21", res.Text)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("路径 = %q, want /v1/responses", gotPath)
	}
}

// TestCall_MessagesProtocolUsesXApiKey 是 Anthropic 兼容层的核心差异。
//
// messages 协议必须用 x-api-key 而不是 Authorization，并且带版本头。
// 用错了上游直接 401，而 401 会被归一化成「鉴权失败」——
// 用户去看自己的 Key 却怎么都对，因为问题根本不在 Key 上。
func TestCall_MessagesProtocolUsesXApiKey(t *testing.T) {
	var gotPath, gotXKey, gotAuth, gotVer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotXKey = r.Header.Get("x-api-key")
		gotAuth = r.Header.Get("Authorization")
		gotVer = r.Header.Get("anthropic-version")
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"答案是 21"}]}`))
	}))
	defer srv.Close()

	res := newTestCaller(t, srv, ProtocolMessages).Call(context.Background(), "题目")
	if res.Text != "答案是 21" {
		t.Fatalf("Text = %q", res.Text)
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("路径 = %q, want /v1/messages", gotPath)
	}
	if gotXKey != "sk-test-key" {
		t.Fatalf("x-api-key = %q, want sk-test-key", gotXKey)
	}
	if gotAuth != "" {
		t.Fatalf("messages 协议不该带 Authorization，实际 %q", gotAuth)
	}
	if gotVer != "2023-06-01" {
		t.Fatalf("anthropic-version = %q, want 2023-06-01", gotVer)
	}
}

// TestCall_RetriesWithoutV1On404 覆盖「用户填 base 的方式不一致」。
//
// 有人填 https://api.example.com，有人填 https://api.example.com/v1。
// 主形态 404 时要自动试另一种，否则用户会误以为接口不存在。
func TestCall_RetriesWithoutV1On404(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		// 只接受不带 /v1 的路径，模拟 base_url 已经带了版本段。
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"答案是 21"}}]}`))
	}))
	defer srv.Close()

	res := newTestCaller(t, srv, ProtocolChatCompletions).Call(context.Background(), "题目")
	if res.Text != "答案是 21" {
		t.Fatalf("回退后应成功，Text=%q kind=%q", res.Text, res.ErrorKind)
	}
	if len(paths) < 2 {
		t.Fatalf("应至少尝试两次路径，实际 %v", paths)
	}
}

// TestCall_DoesNotRetryOnAuthFailure 保证不把 401 当路径问题重试。
//
// 401 换路径也还是 401，重试只是白打一次上游、还可能触发限流。
func TestCall_DoesNotRetryOnAuthFailure(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	res := newTestCaller(t, srv, ProtocolChatCompletions).Call(context.Background(), "题目")
	if calls != 1 {
		t.Fatalf("401 不该重试，实际调用 %d 次", calls)
	}
	if res.ErrorKind != iqbenchmark.ErrorKindAuthFailed {
		t.Fatalf("ErrorKind = %q, want %q", res.ErrorKind, iqbenchmark.ErrorKindAuthFailed)
	}
	if !strings.Contains(res.ErrorMessage, "API Key") {
		t.Fatalf("提示应指向 Key，实际 %q", res.ErrorMessage)
	}
}

// TestCall_EmptyResponseIsNotSuccess 是判分正确性的前提。
//
// 空响应必须归一化成 empty_response，绝不能当成「模型答了但答错」——
// 那会把「上游坏了」误判成「模型降智」。这是本功能最核心的判别边界。
func TestCall_EmptyResponseIsNotSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"   "}}]}`))
	}))
	defer srv.Close()

	res := newTestCaller(t, srv, ProtocolChatCompletions).Call(context.Background(), "题目")
	if res.Text != "" {
		t.Fatalf("空白内容应归一为空串，实际 %q", res.Text)
	}
	if res.ErrorKind != iqbenchmark.ErrorKindEmptyResponse {
		t.Fatalf("ErrorKind = %q, want %q", res.ErrorKind, iqbenchmark.ErrorKindEmptyResponse)
	}
}

// TestCall_TimeoutIsClassifiedAsTimeout 锁住「超时 ≠ 降智」。
func TestCall_TimeoutIsClassifiedAsTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"迟到的答案"}}]}`))
	}))
	defer srv.Close()

	caller := newTestCaller(t, srv, ProtocolChatCompletions)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	res := caller.Call(ctx, "题目")
	if res.ErrorKind == "" {
		t.Fatal("超时必须报错，不能算成功")
	}
	if res.Text != "" {
		t.Fatalf("超时不该带回文本，实际 %q", res.Text)
	}
	if !strings.Contains(res.ErrorMessage, "超时") && !strings.Contains(res.ErrorMessage, "中断") {
		t.Fatalf("超时提示不贴切: %q", res.ErrorMessage)
	}
}

// TestCall_MalformedJSONIsIncomplete 覆盖上游返回非 JSON（例如网关 HTML）。
func TestCall_MalformedJSONIsIncomplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html>502 Bad Gateway</html>`))
	}))
	defer srv.Close()

	res := newTestCaller(t, srv, ProtocolChatCompletions).Call(context.Background(), "题目")
	if res.ErrorKind != iqbenchmark.ErrorKindResponseIncomplete {
		t.Fatalf("ErrorKind = %q, want %q", res.ErrorKind, iqbenchmark.ErrorKindResponseIncomplete)
	}
}

// TestCall_NeverLeaksKeyInMessage 是凭据约束在外发层的防线。
//
// 错误提示会带上上游原文片段，必须确认 Key 没有混进去 ——
// 这段文本最终会存库、会在历史墙展示给所有人看。
func TestCall_NeverLeaksKeyInMessage(t *testing.T) {
	// 故意让上游在错误体里回显收到的 Key —— 有些网关真会这么干。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Authorization")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key ` + key + `"}}`))
	}))
	defer srv.Close()

	res := newTestCaller(t, srv, ProtocolChatCompletions).Call(context.Background(), "题目")
	if strings.Contains(res.ErrorMessage, "sk-test-key") {
		t.Fatalf("错误提示里泄漏了 Key: %q", res.ErrorMessage)
	}
	// "Bearer" 这个词本身不算秘密（就印在 RFC 里），但后面跟的 Key 必须是掩码。
	if strings.Contains(res.ErrorMessage, "Bearer sk-") {
		t.Fatalf("错误提示里泄漏了鉴权串: %q", res.ErrorMessage)
	}
}

// TestCall_ServerErrorClassified 覆盖 5xx。
func TestCall_ServerErrorClassified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	res := newTestCaller(t, srv, ProtocolChatCompletions).Call(context.Background(), "题目")
	if res.ErrorKind == "" {
		t.Fatal("5xx 必须归类为错误")
	}
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("StatusCode = %d, want 500", res.StatusCode)
	}
}

// TestCall_RequestBodyCarriesEffortOnlyWhenSupported 覆盖思考等级的下发位置。
//
// chat_completions 放顶层 reasoning_effort；messages 必须完全不下发 ——
// Anthropic 收到未知字段会直接 400，而那会被误读成上游故障。
func TestCall_RequestBodyCarriesEffortOnlyWhenSupported(t *testing.T) {
	var chatBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &chatBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	caller := newTestCaller(t, srv, ProtocolChatCompletions)
	caller.Effort = EffortHigh
	_ = caller.Call(context.Background(), "题目")

	if chatBody["reasoning_effort"] != "high" {
		t.Fatalf("chat_completions 应在顶层带 reasoning_effort，实际 %v", chatBody["reasoning_effort"])
	}
}

// TestCall_ContextCancelledStopsRetry 保证取消后不再继续打上游。
func TestCall_ContextCancelledStopsRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 一开始就取消

	_ = newTestCaller(t, srv, ProtocolChatCompletions).Call(ctx, "题目")
	if calls > 1 {
		t.Fatalf("上下文已取消不该继续重试，实际调用 %d 次", calls)
	}
}

// TestFillRenderTemplateInjectsSceneAndCode 是防重放机制的单元级证据。
//
// 生成题必须每次带上随机的场景与校验码，否则模型可以背下答案。
func TestFillRenderTemplateInjectsSceneAndCode(t *testing.T) {
	prompt, code := FillRenderTemplate("场景：{scene}，校验码：{code}")
	if code == "" {
		t.Fatal("必须生成校验码")
	}
	if len(code) != 8 {
		t.Fatalf("校验码应为 8 位十六进制，实际 %q", code)
	}
	if strings.Contains(prompt, "{scene}") || strings.Contains(prompt, "{code}") {
		t.Fatalf("占位符未被替换: %q", prompt)
	}
	if !strings.Contains(prompt, code) {
		t.Fatalf("prompt 里应含校验码 %q，实际 %q", code, prompt)
	}
}

// TestFillRenderTemplateVariesPerCall 保证每次不重复。
func TestFillRenderTemplateVariesPerCall(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 8; i++ {
		_, code := FillRenderTemplate("{code}")
		if seen[code] {
			t.Fatalf("校验码重复了: %q（防重放失效）", code)
		}
		seen[code] = true
	}
}

// TestListModels_FallsBackWithoutV1 覆盖模型列表的路径回退。
func TestListModels_FallsBackWithoutV1(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m-a"},{"id":"m-b"}]}`))
	}))
	defer srv.Close()

	caller := newTestCaller(t, srv, ProtocolChatCompletions)
	models, err := caller.ListModels(context.Background())
	if err != nil {
		t.Fatalf("列模型失败: %v", err)
	}
	if len(models) != 2 || models[0] != "m-a" || models[1] != "m-b" {
		t.Fatalf("models = %v, want [m-a m-b]", models)
	}
}

// TestListModels_ErrorPropagates 保证拉不到模型时如实报错。
func TestListModels_ErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()

	caller := newTestCaller(t, srv, ProtocolChatCompletions)
	if _, err := caller.ListModels(context.Background()); err == nil {
		t.Fatal("401 时应返回错误")
	} else if errors.Is(err, context.Canceled) {
		t.Fatalf("不该是取消错误: %v", err)
	}
}
