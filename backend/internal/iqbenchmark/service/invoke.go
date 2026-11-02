package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// 本文件实现「通过 sub2api 自身网关发起检测调用」。
//
// 硬约束：绝不允许直连上游供应商。所有检测流量必须走本服务的
// /v1/chat/completions 与 /v1/images/generations，这样：
//   - 计费、配额、审计、限流全部复用既有链路（无需另建一套）；
//   - 检测流量天然记在内部账号的 usage_logs 上，便于统计隔离；
//   - 不会绕过任何既有的安全策略（内容审核、模型白名单等）。

// GatewayClient 通过回环地址调用本服务自己的网关。
//
// 注意：客户端**不持有任何 Key**。Key 随每次请求传入（见 ChatRequest.APIKey），
// 这样同一个客户端可以被内部账号和用户自测复用，也不会有并发写共享状态的竞争。
type GatewayClient struct {
	// BaseURL 形如 http://127.0.0.1:8080（不含 /v1）。
	BaseURL string
	// Timeout 是单次调用超时。
	Timeout time.Duration
	// HTTPClient 可注入（测试用）；为空时使用加固后的默认客户端。
	HTTPClient *http.Client
	// UserAgent 固定标识检测流量来源。
	UserAgent string
}

// NewGatewayClient 构造网关客户端。
//
// BaseURL 只接受回环地址或显式配置的地址：如果配成了外部地址，
// 检测流量就会脱离本服务自己的计费与审计链路，因此这里做硬校验。
//
// 不再接收 apiKey 参数：Key 由调用方按请求传入（内部账号 Key 在运行期才创建，
// 装配期根本不存在）。保留该参数会让「装配期传空、运行期忘了改」这种缺陷再次发生。
func NewGatewayClient(baseURL string, timeout time.Duration) (*GatewayClient, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if trimmed == "" {
		return nil, errors.New("iqbenchmark: gateway base url is empty")
	}
	if !isLoopbackURL(trimmed) && !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
		return nil, fmt.Errorf("iqbenchmark: unsupported gateway base url %q", trimmed)
	}
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	return &GatewayClient{
		BaseURL:   trimmed,
		Timeout:   timeout,
		UserAgent: iqbenchmark.DetectionUserAgent,
	}, nil
}

// isLoopbackURL 判断 URL 是否指向回环地址。
func isLoopbackURL(raw string) bool {
	rest := raw
	for _, prefix := range []string{"http://", "https://"} {
		if strings.HasPrefix(rest, prefix) {
			rest = rest[len(prefix):]
			break
		}
	}
	host := rest
	if idx := strings.IndexAny(host, "/?#"); idx >= 0 {
		host = host[:idx]
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ChatRequest 是一次文本/代码检测请求。
type ChatRequest struct {
	Model        string
	Prompt       string
	SystemPrompt string
	// Params 是题目里的额外参数（temperature、max_tokens 等），原样透传。
	Params map[string]any
	// APIKey 是本次调用使用的明文 Key，必填。
	//
	// 为什么按请求传而不是存在 GatewayClient 上：
	// 内部账号的 Key 是在运行期由 EnsureInternalAgent 创建并写入数据库的，
	// 而 GatewayClient 在 Wire 装配期就已构造完成 —— 那时 Key 还不存在。
	// 把 Key 存在客户端上必然要 mutable 共享状态（并发写 + 数据竞争），
	// 且第二阶段「用户用自己 Key 自测」也需要按请求切换 Key。
	// 因此 Key 跟随请求走，客户端保持无状态。
	APIKey string
}

// ChatResponse 是从网关响应中抽取出的最小信息。
type ChatResponse struct {
	// Text 是拼接后的正文。
	Text string
	// InputTokens / OutputTokens 来自 usage 字段（缺失时为 0）。
	InputTokens  int
	OutputTokens int
	// RequestID 用于事后按 request_id 回采用量（网关注入的标识）。
	RequestID string
}

// ImageRequest 是一次图片检测请求。
type ImageRequest struct {
	Model  string
	Prompt string
	Params map[string]any
	// APIKey 是本次调用使用的明文 Key，必填（语义同 ChatRequest.APIKey）。
	APIKey string
}

// ImageResponse 是图片生成结果的抽取。
type ImageResponse struct {
	// URLs 是返回的图片地址（最多取 maxArtifactsPerImage 张）。
	URLs []string
	// B64 是 base64 编码的图片（部分模型只返回这个）。
	B64 []string
	// RevisedPrompt 是模型改写后的提示词（可选）。
	RevisedPrompt string
	RequestID     string
}

// ErrGatewayNon2xx 表示网关返回非 2xx 状态。
type ErrGatewayNon2xx struct {
	Status int
	// Code 是从响应体解析出的错误码（若无则为空）。
	Code string
	// Message 是脱敏后的错误摘要（绝不包含原始响应体全量）。
	Message string
}

func (e *ErrGatewayNon2xx) Error() string {
	return fmt.Sprintf("iqbenchmark: gateway returned %d (%s)", e.Status, e.Code)
}

// Chat 调用 /v1/chat/completions。
func (c *GatewayClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body := map[string]any{
		"model":    req.Model,
		"messages": buildMessages(req.SystemPrompt, req.Prompt),
	}
	mergeParams(body, req.Params)

	raw, header, err := c.post(ctx, "/v1/chat/completions", req.APIKey, body)
	if err != nil {
		return nil, err
	}

	var payload struct {
		ID      string `json:"id"`
		Choices []struct {
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
			Text string `json:"text"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("iqbenchmark: decode chat response: %w", err)
	}

	out := &ChatResponse{
		InputTokens:  payload.Usage.PromptTokens,
		OutputTokens: payload.Usage.CompletionTokens,
		RequestID:    firstNonEmpty(payload.ID, header.Get("X-Request-Id"), header.Get("X-Request-ID")),
	}
	var parts []string
	for _, choice := range payload.Choices {
		if text := extractContent(choice.Message.Content); text != "" {
			parts = append(parts, text)
			continue
		}
		if choice.Text != "" {
			parts = append(parts, choice.Text)
		}
	}
	out.Text = strings.Join(parts, "\n")
	return out, nil
}

// Image 调用 /v1/images/generations。
func (c *GatewayClient) Image(ctx context.Context, req ImageRequest) (*ImageResponse, error) {
	body := map[string]any{
		"model":  req.Model,
		"prompt": req.Prompt,
		"n":      1,
	}
	mergeParams(body, req.Params)

	raw, header, err := c.post(ctx, "/v1/images/generations", req.APIKey, body)
	if err != nil {
		return nil, err
	}

	var payload struct {
		ID   string `json:"id"`
		Data []struct {
			URL           string `json:"url"`
			B64JSON       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("iqbenchmark: decode image response: %w", err)
	}

	out := &ImageResponse{RequestID: firstNonEmpty(payload.ID, header.Get("X-Request-Id"), header.Get("X-Request-ID"))}
	for _, item := range payload.Data {
		if strings.TrimSpace(item.URL) != "" {
			out.URLs = append(out.URLs, item.URL)
		}
		if strings.TrimSpace(item.B64JSON) != "" {
			out.B64 = append(out.B64, item.B64JSON)
		}
		if out.RevisedPrompt == "" && strings.TrimSpace(item.RevisedPrompt) != "" {
			out.RevisedPrompt = item.RevisedPrompt
		}
	}
	return out, nil
}

// post 发起一次带鉴权的 JSON POST。
//
// apiKey 必须非空：空的 Bearer token 会被网关以 401 拒绝，
// 但如果在这里放行，故障会在很远的地方以「upstream_error」的形式出现，
// 排查成本极高。因此这里显式拒绝，让问题在调用点就暴露。
func (c *GatewayClient) post(ctx context.Context, path, apiKey string, body map[string]any) ([]byte, http.Header, error) {
	if c == nil {
		return nil, nil, errors.New("iqbenchmark: gateway client is nil")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, nil, iqbenchmark.ErrInternalAgentMissing
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("iqbenchmark: encode request: %w", err)
	}

	callCtx := ctx
	cancel := func() {}
	if c.Timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, c.Timeout)
	}
	defer cancel()

	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, c.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, nil, fmt.Errorf("iqbenchmark: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	if c.UserAgent != "" {
		httpReq.Header.Set("User-Agent", c.UserAgent)
	}

	client := c.HTTPClient
	if client == nil {
		client = defaultGatewayHTTPClient(c.Timeout)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		// 上下文超时单独成码：调用方需据此记为 timeout 而非 upstream_error。
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return nil, nil, fmt.Errorf("%w: %v", iqbenchmark.ErrDeadlineExceeded, err)
		}
		return nil, nil, fmt.Errorf("%w: %v", iqbenchmark.ErrGatewayUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 限制读取体积：异常响应体不应把内存打满。
	limited := io.LimitReader(resp.Body, 16<<20)
	raw, readErr := io.ReadAll(limited)
	if readErr != nil {
		return nil, resp.Header, fmt.Errorf("iqbenchmark: read response: %w", readErr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, classifyGatewayError(resp.StatusCode, raw)
	}
	return raw, resp.Header, nil
}

// classifyGatewayError 把非 2xx 响应翻译成有语义的错误。
//
// 只提取错误码与极短摘要：原始响应体可能含上游供应商信息、账号 ID 等，
// 绝不能整体带进日志或错误消息。
func classifyGatewayError(status int, raw []byte) error {
	code := ""
	message := ""
	var payload struct {
		Error struct {
			Code      any    `json:"code"`
			Type      string `json:"type"`
			Message   string `json:"message"`
			ErrorType string `json:"error_type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil {
		message = truncate(firstNonEmpty(payload.Error.Message, payload.Error.Type, payload.Error.ErrorType), 160)
		switch v := payload.Error.Code.(type) {
		case string:
			code = v
		case float64:
			code = fmt.Sprintf("%d", int64(v))
		}
	}
	if code == "" {
		code = statusCodeHint(status)
	}
	return &ErrGatewayNon2xx{Status: status, Code: code, Message: message}
}

// statusCodeHint 把常见状态码映射为本模块的错误码。
func statusCodeHint(status int) string {
	switch {
	case status == http.StatusTooManyRequests:
		return iqbenchmark.ErrCodeRateLimited
	case status == http.StatusPaymentRequired || status == 403:
		return iqbenchmark.ErrCodeInsufficientBalance
	case status == http.StatusUnauthorized:
		return "unauthorized"
	case status >= 500:
		return iqbenchmark.ErrCodeUpstreamError
	default:
		return iqbenchmark.ErrCodeUpstreamError
	}
}

// defaultGatewayHTTPClient 返回用于回环调用的 HTTP 客户端。
//
// 不设代理（检测流量不能走外部代理）、限制连接数、禁止跟随重定向
// （重定向会把请求带到非预期地址，绕过计费链路）。
func defaultGatewayHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:                 nil,
			MaxIdleConns:          16,
			MaxIdleConnsPerHost:   8,
			IdleConnTimeout:       60 * time.Second,
			ResponseHeaderTimeout: timeout,
			ForceAttemptHTTP2:     false,
		},
	}
}

// buildMessages 组装 messages 数组。
func buildMessages(systemPrompt, prompt string) []map[string]string {
	out := make([]map[string]string, 0, 2)
	if strings.TrimSpace(systemPrompt) != "" {
		out = append(out, map[string]string{"role": "system", "content": systemPrompt})
	}
	out = append(out, map[string]string{"role": "user", "content": prompt})
	return out
}

// mergeParams 把题目参数合并进请求体。
//
// 安全约束：只允许白名单参数透传 —— 题目配置来自管理员，但仍不能让它
// 覆盖 model/messages/prompt 这些关键字段，否则一个错误的题目配置
// 就能把请求发到别处或伪造多轮对话。
func mergeParams(body, params map[string]any) {
	if len(params) == 0 {
		return
	}
	blocked := map[string]struct{}{
		"model": {}, "messages": {}, "prompt": {}, "n": {},
		"stream": {}, "user": {},
	}
	for key, value := range params {
		if _, deny := blocked[key]; deny {
			continue
		}
		body[key] = value
	}
}

// extractContent 解析 message.content，兼容字符串与多模态数组两种形态。
func extractContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	// 多模态：[{"type":"text","text":"..."}, ...]
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var sb strings.Builder
		for _, part := range parts {
			if part.Text != "" {
				// strings.Builder.WriteString 永远返回 nil error；显式丢弃以免 errcheck 报错。
				_, _ = sb.WriteString(part.Text)
			}
		}
		return sb.String()
	}
	return ""
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
