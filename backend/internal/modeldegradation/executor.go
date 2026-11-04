package modeldegradation

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// 本文件负责「把一道题送到上游并拿回文本」。
//
// 全部出站请求都经过这里，因此安全与会话细节只在这一处需要保证：
//   - 只连 https，且经 NewSafeDialer 拨号（防 DNS rebinding）
//   - 不跟随跨 host 重定向（避免 302 到内网）
//   - api_key 只放在鉴权头里，不进 URL、不进日志
//   - 404/405 时回退试「不带 /v1」的路径（用户填 base 的方式不一致）

// CallResult 是一次上游调用的结果。
type CallResult struct {
	// Text 是模型返回的文本（已按协议抽取）。空串表示没拿到内容。
	Text string
	// StatusCode 是最终响应的 HTTP 状态码；0 表示连接层就失败了。
	StatusCode int
	// ErrorKind 是 12 类归一化错误之一；空串表示成功。
	ErrorKind string
	// ErrorMessage 是给用户看的可读说明（已脱敏，不含 Key）。
	ErrorMessage string
	// DurationMS 是本次调用耗时。
	DurationMS int64
}

// UpstreamCaller 封装一次上游调用所需的全部输入。
type UpstreamCaller struct {
	// BaseURL 已经过 ValidateBaseURL，是可信的解析结果。
	BaseURL *url.URL
	// APIKey 是用户凭据，只在内存里活到这次调用结束。
	APIKey string

	Protocol string
	Model    string
	Effort   string

	// Timeout 是单题超时。生成题用 render_timeout_seconds，
	// 逻辑题用较短固定值（见 service 层）。
	Timeout time.Duration

	// HTTPClient 可注入（测试用）。为 nil 时由本结构自建带安全拨号的客户端。
	HTTPClient *http.Client
}

// httpClient 返回用于出站请求的客户端。
//
// 关键点：
//   - Transport 用 NewSafeDialer，拨号时复查 IP
//   - CheckRedirect 拒绝任何跨 host 跳转：合法上游不会把 API 请求
//     重定向到别的主机；会的话要么配置错，要么是 SSRF 探测
//   - 不用 http.DefaultClient，避免继承全局代理设置把请求带偏
func (c *UpstreamCaller) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	dialer := NewSafeDialer(30 * time.Second)
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		// 只允许 HTTPS 连接；http scheme 在 ValidateBaseURL 已挡，
		// 这里再关一层是防止重定向带到 http。
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 0, // 由 context 统一控制总超时
		MaxIdleConns:          4,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   c.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// 不允许跳转：上游 API 端点不该 3xx 到别处。
			return http.ErrUseLastResponse
		},
	}
}

// Call 执行一次上游调用。
//
// 按 BuildEndpoints 的候选顺序尝试：主形态返回 404/405 时试回退形态，
// 其余状态码（成功或其它错误）直接采用，不再尝试。
func (c *UpstreamCaller) Call(ctx context.Context, prompt string) CallResult {
	started := time.Now()

	endpoints, err := BuildEndpoints(c.BaseURL, c.Protocol)
	if err != nil {
		return CallResult{
			ErrorKind:    iqbenchmark.ErrorKindGeneric,
			ErrorMessage: "上游地址或协议配置有误",
			DurationMS:   time.Since(started).Milliseconds(),
		}
	}

	var last CallResult
	for i, ep := range endpoints {
		res := c.callOnce(ctx, ep, prompt)
		last = res

		// 只有「路径可能不对」的 404/405 才值得换下一个候选。
		// 其它任何结果（成功 / 401 / 500 / 超时）都直接返回 ——
		// 换路径不会改变这些错误的性质，只会白白多打一次上游。
		retryable := res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusMethodNotAllowed
		if !retryable || i == len(endpoints)-1 {
			return res
		}
		// 上下文已取消时不再重试。
		if ctx.Err() != nil {
			return res
		}
	}
	return last
}

// callOnce 对单个候选路径发起一次请求。
func (c *UpstreamCaller) callOnce(ctx context.Context, ep Endpoint, prompt string) CallResult {
	started := time.Now()

	// 拼出完整 URL：base 的 scheme+host + 候选路径。
	target := *c.BaseURL
	target.Path = ep.Path
	target.RawQuery = ""

	body := RequestBody(c.Protocol, c.Model, prompt, c.Effort)
	payload, err := json.Marshal(body)
	if err != nil {
		return CallResult{
			ErrorKind:    iqbenchmark.ErrorKindGeneric,
			ErrorMessage: "构造请求失败",
			DurationMS:   time.Since(started).Milliseconds(),
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(payload))
	if err != nil {
		return CallResult{
			ErrorKind:    iqbenchmark.ErrorKindGeneric,
			ErrorMessage: "构造请求失败",
			DurationMS:   time.Since(started).Milliseconds(),
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if ep.AuthPrefix != "" {
		req.Header.Set(ep.AuthHeader, ep.AuthPrefix+c.APIKey)
	} else {
		req.Header.Set(ep.AuthHeader, c.APIKey)
	}
	// Anthropic 需要显式的版本头。
	if c.Protocol == ProtocolMessages {
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	client := c.httpClient()
	resp, err := client.Do(req)
	if err != nil {
		kind := iqbenchmark.ClassifyErrorKind(err, 0)
		return CallResult{
			ErrorKind:    kind,
			ErrorMessage: userFacingError(kind),
			DurationMS:   time.Since(started).Milliseconds(),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	// 限制读取大小，避免上游返回巨大内容打爆内存。
	// 生成题一个 SVG 也就几十 KB，4MB 足够宽裕。
	const maxBodyBytes = 4 << 20
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if readErr != nil {
		kind := iqbenchmark.ClassifyErrorKind(readErr, resp.StatusCode)
		return CallResult{
			StatusCode:   resp.StatusCode,
			ErrorKind:    kind,
			ErrorMessage: userFacingError(kind),
			DurationMS:   time.Since(started).Milliseconds(),
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		kind := classifyByStatus(resp.StatusCode)
		return CallResult{
			StatusCode: resp.StatusCode,
			ErrorKind:  kind,
			// 带上上游原文的一小段有助于用户判断，但要脱敏（不含 Key）。
			ErrorMessage: userFacingError(kind) + snippetSuffix(raw),
			DurationMS:   time.Since(started).Milliseconds(),
		}
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return CallResult{
			StatusCode:   resp.StatusCode,
			ErrorKind:    iqbenchmark.ErrorKindResponseIncomplete,
			ErrorMessage: userFacingError(iqbenchmark.ErrorKindResponseIncomplete),
			DurationMS:   time.Since(started).Milliseconds(),
		}
	}

	text := ExtractText(c.Protocol, decoded)
	duration := time.Since(started).Milliseconds()

	if strings.TrimSpace(text) == "" {
		return CallResult{
			StatusCode:   resp.StatusCode,
			Text:         "",
			ErrorKind:    iqbenchmark.ErrorKindEmptyResponse,
			ErrorMessage: userFacingError(iqbenchmark.ErrorKindEmptyResponse),
			DurationMS:   duration,
		}
	}

	return CallResult{
		StatusCode: resp.StatusCode,
		Text:       text,
		DurationMS: duration,
	}
}

// errStatusOnly 是只为触发状态码分支而存在的哨兵错误。
//
// 它永远不会被返回给用户，也不代表任何真实故障 —— 见 classifyByStatus。
var errStatusOnly = errors.New("modeldegradation: 仅按状态码分类")

// classifyByStatus 按 HTTP 状态码归类上游错误。
//
// 为什么要这层包装：iqbenchmark.ClassifyErrorKind 的约定是
// 「err 是传输层错误，statusCode 只是附带上下文」，它一进来就判
// err == nil 并直接返回 generic。但本模块在拿到非 2xx 响应时
// 恰恰没有传输层错误（err 是 nil），于是 401/403/404/429/402/5xx
// 全都被压成 generic，用户看到的永远是「上游请求失败，请稍后重试」，
// 指不到真正原因 —— 这本身就是一种「看起来降智」的体验。
//
// 传一个无关哨兵错误，让它跳过 nil 短路走到状态码分支，
// 这样 12 类的映射规则仍然只在 iqbenchmark 里定义一份，不在此重复。
func classifyByStatus(statusCode int) string {
	if kind := iqbenchmark.ClassifyErrorKind(errStatusOnly, statusCode); kind != "" {
		return kind
	}
	return iqbenchmark.ErrorKindGeneric
}

// userFacingError 把错误类型翻译成中文提示。
//
// 文案对齐参考实现（modelDegradation.upstream.*），12 类全覆盖。
func userFacingError(kind string) string {
	switch kind {
	case iqbenchmark.ErrorKindAuthFailed:
		return "上游鉴权失败，请检查 API Key"
	case iqbenchmark.ErrorKindForbidden:
		return "上游拒绝访问，请检查 Key 权限或网关限制"
	case iqbenchmark.ErrorKindNotFound:
		return "接口或模型不存在，请检查地址和协议"
	case iqbenchmark.ErrorKindTimeout:
		return "上游响应超时，请稍后重试或检查接口服务状态"
	case iqbenchmark.ErrorKindRateLimited:
		return "上游请求限流，请稍后重试"
	case iqbenchmark.ErrorKindQuotaExceeded:
		return "上游额度不足，请检查接口账户余额或限额"
	case iqbenchmark.ErrorKindServerError:
		return "上游服务暂时不可用，请稍后重试"
	case iqbenchmark.ErrorKindGatewayTimeout:
		return "上游网关等待模型响应超时"
	case iqbenchmark.ErrorKindStreamBroken:
		return "上游连接中断，请稍后重试"
	case iqbenchmark.ErrorKindResponseIncomplete:
		return "上游响应中断，未收到完整结果"
	case iqbenchmark.ErrorKindEmptyResponse:
		return "上游未返回内容，请检查接口服务"
	default:
		return "上游请求失败，请稍后重试或联系接口服务商"
	}
}

// snippetSuffix 从上游错误响应里取一小段可读文本，附在提示后面。
//
// 只取 JSON 里的 message/error 字段，不原样贴整段 HTML ——
// 一是长度，二是 HTML 里可能有重定向地址之类的东西，贴出去反而诱导用户。
//
// 关键：返回值最终会落库、并出现在所有人可见的历史墙上。有些网关
// 会把收到的鉴权头原样回显在错误信息里（"bad key Bearer sk-xxx"），
// 直接贴出去等于把用户的 Key 公开。所以这里必须再过一遍脱敏。
func snippetSuffix(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err == nil {
		if msg, ok := payload["message"].(string); ok && msg != "" {
			return "：" + truncate(RedactSecrets(msg), 200)
		}
		if errObj, ok := payload["error"].(map[string]any); ok {
			if msg, ok := errObj["message"].(string); ok && msg != "" {
				return "：" + truncate(RedactSecrets(msg), 200)
			}
		}
	}
	// 非 JSON：给一句通用说明而不是贴原文。
	return ""
}

// RedactSecrets 用的三条规则。
//
// 预编译在包级：这些模式在每次上游报错时都会跑，而且
// MustCompile 放在函数里会让 panic 发生在请求路径上。
var (
	// Bearer 后面跟的整段 token。
	bearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._\-]+`)
	// xxx-api-key: value / api_key=value 这类键值对。
	apiKeyPattern = regexp.MustCompile(`(?i)\b([a-z0-9_\-]*(?:api[_\-]?key|authorization)["']?\s*[:=]\s*["']?)[A-Za-z0-9._\-]+`)
	// 裸长串的候选：≥20 位字母数字下划线连字符。
	// 这只是「候选」，真正的判定看 looksLikeSecret —— 见 RedactSecrets。
	secretTokenPattern = regexp.MustCompile(`\b[A-Za-z0-9_\-]{20,}\b`)
	// 纯十六进制长串（≥32 位）：会话 token / 随机 ID 的典型形态。
	hexTokenPattern = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)
	// 模型名的典型形态：全小写、用连字符和数字分段，如
	// claude-sonnet-4-20250514。这类不该被当成密钥抹掉。
	modelNamePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-._][a-z0-9]+)*$`)
)

// looksLikeSecret 判断一个「长串」是否真的像凭据。
//
// 为什么要这一步：单看长度会把模型名一起抹掉。
// claude-sonnet-4-20250514 有 24 个字符，但它显然不是密钥；
// 抹掉它会让用户看到「模型 *** 不存在」这种更没法排查的提示。
//
// 判定依据：
//   - 同时含大写和小写字母（模型名几乎全是小写）→ 是密钥
//   - 纯十六进制且 ≥32 位 → 是 token
//   - 其余长串（全小写 + 连字符分段）→ 当模型名放过
func looksLikeSecret(s string) bool {
	if hexTokenPattern.MatchString(s) {
		return true
	}
	if modelNamePattern.MatchString(s) {
		return false
	}
	hasUpper := strings.ToLower(s) != s
	hasLower := strings.ToUpper(s) != s
	return hasUpper && hasLower
}

// RedactSecrets 抹掉文本里看起来像凭据的片段。
//
// 为什么不能只靠「我们不发 Key」：上游可能把请求头回显回来，
// 错误信息又会被存库并公开展示。防御必须落在写入之前这一层。
//
// 处理两类：
//   - 带前缀的鉴权串（Bearer xxx / sk-xxx / x-api-key: xxx）
//   - 独立出现的长随机串（≥20 位、字母数字混排），这几乎是 Key 的特征
func RedactSecrets(s string) string {
	if s == "" {
		return s
	}
	// 前两步是「确定是凭据」的形态，直接抹。
	s = bearerPattern.ReplaceAllString(s, "Bearer ***")
	s = apiKeyPattern.ReplaceAllString(s, "${1}***")
	// 第三步是「长串里挑出真的像凭据的」，避免误伤模型名。
	s = secretTokenPattern.ReplaceAllStringFunc(s, func(tok string) string {
		if looksLikeSecret(tok) {
			return "***"
		}
		return tok
	})
	return s
}

// truncate 按字符数截断（不是字节数，避免切坏中文）。
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// ── 生成题随机化 ──────────────────────────────────────────────────────

// renderScenes 是生成题的随机场景池。
//
// 为什么要随机场景：如果每一轮都画同一个主题，模型很可能给出高度相似的
// 构图，横向比较就看不出「谁更会画」。换场景才能暴露只会画一种构图的情况。
var renderScenes = []string{
	"热带海岛",
	"雪山之巅",
	"夜晚的城市天际线",
	"森林里的小溪",
	"沙漠中的绿洲",
	"樱花盛开的庭院",
	"水下珊瑚礁",
	"极光下的冰原",
	"雨中的江南古镇",
	"太空站舷窗外",
}

// FillRenderTemplate 把 render 题模板里的 {scene} 与 {code} 替换掉。
//
// {code} 是 8 位十六进制校验码：要求模型把它画进画面，这样一眼能看出
// 这幅图是不是本次现画的 —— 拿旧图/模板图充数时校验码对不上。
// 用 crypto/rand 而不是 math/rand：校验码要真的不可预测，
// 否则人可以预先准备带固定码的图。
func FillRenderTemplate(template string) (prompt, code string) {
	code = randomHex8()
	scene := randomScene()
	prompt = strings.ReplaceAll(template, "{scene}", scene)
	prompt = strings.ReplaceAll(prompt, "{code}", code)
	return prompt, code
}

// randomHex8 生成 8 位大写十六进制字符串。
func randomHex8() string {
	buf := make([]byte, 4) // 4 字节 = 8 个十六进制字符
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败极罕见；退回时间戳保证仍有区分度。
		return strings.ToUpper(fmt.Sprintf("%08x", time.Now().UnixNano()&0xFFFFFFFF))
	}
	return strings.ToUpper(hex.EncodeToString(buf))
}

// randomScene 从场景池里随机取一个。
func randomScene() string {
	if len(renderScenes) == 0 {
		return "任意场景"
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(renderScenes))))
	if err != nil {
		return renderScenes[0]
	}
	return renderScenes[n.Int64()]
}

// ErrUpstreamCancelled 表示执行被用户取消。
var ErrUpstreamCancelled = errors.New("modeldegradation: 检测已被取消")

// ListModels 拉取上游可用模型列表。
//
// 走 GET {base}/v1/models（Anthropic 是 /v1/models 同样形态）。
// 与 Call 共用同一套安全拨号与重定向策略。
//
// 失败时返回空列表而不是错误：这个功能只是「帮用户填模型名」的便利，
// 拉不到就让他手输。把它做成硬失败会让整个发起流程卡住。
func (c *UpstreamCaller) ListModels(ctx context.Context) ([]string, error) {
	endpoints, err := BuildEndpoints(c.BaseURL, c.Protocol)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, ep := range endpoints {
		// 把 /chat/completions 之类的尾巴换成 /models。
		path := modelsPathFrom(ep.Path)

		target := *c.BaseURL
		target.Path = path
		target.RawQuery = ""

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Accept", "application/json")
		if ep.AuthPrefix != "" {
			req.Header.Set(ep.AuthHeader, ep.AuthPrefix+c.APIKey)
		} else {
			req.Header.Set(ep.AuthHeader, c.APIKey)
		}
		if c.Protocol == ProtocolMessages {
			req.Header.Set("anthropic-version", "2023-06-01")
		}

		client := c.httpClient()
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		const maxBodyBytes = 2 << 20
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		// 404/405 说明路径不对，换下一个候选。
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			lastErr = fmt.Errorf("modeldegradation: 模型列表路径未找到")
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			kind := iqbenchmark.ClassifyErrorKind(nil, resp.StatusCode)
			return nil, errors.New(userFacingError(kind))
		}

		models, parseErr := parseModelList(raw)
		if parseErr != nil {
			lastErr = parseErr
			continue
		}
		return models, nil
	}

	if lastErr == nil {
		lastErr = errors.New("modeldegradation: 获取模型列表失败")
	}
	return nil, lastErr
}

// modelsPathFrom 把端点路径改写成模型列表路径。
//
// /v1/chat/completions -> /v1/models
// /v1/responses        -> /v1/models
// /v1/messages         -> /v1/models
// 无版本段时保持同样的相对层级。
func modelsPathFrom(endpointPath string) string {
	for _, suffix := range []string{"/chat/completions", "/responses", "/messages"} {
		if strings.HasSuffix(endpointPath, suffix) {
			return strings.TrimSuffix(endpointPath, suffix) + "/models"
		}
	}
	return strings.TrimRight(endpointPath, "/") + "/models"
}

// parseModelList 解析模型列表响应。
//
// 两种形态都支持：
//   - OpenAI 风格 {"data":[{"id":"gpt-4"},...]}
//   - 简化形态 {"models":["a","b"]} 或顶层数组
func parseModelList(raw []byte) ([]string, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err == nil {
		// {"data":[{id:...}]}
		if data, ok := payload["data"].([]any); ok {
			out := make([]string, 0, len(data))
			for _, item := range data {
				if m, ok := item.(map[string]any); ok {
					if id, ok := m["id"].(string); ok && id != "" {
						out = append(out, id)
					}
				}
			}
			if len(out) > 0 {
				return out, nil
			}
			// data 存在但为空：是合法响应，返回空列表。
			return []string{}, nil
		}
		// {"models":["a","b"]}
		if models, ok := payload["models"].([]any); ok {
			out := make([]string, 0, len(models))
			for _, m := range models {
				if s, ok := m.(string); ok && s != "" {
					out = append(out, s)
				}
			}
			return out, nil
		}
	}

	// 顶层数组
	var arr []any
	if err := json.Unmarshal(raw, &arr); err == nil {
		out := make([]string, 0, len(arr))
		for _, m := range arr {
			if s, ok := m.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	}

	return nil, errors.New("modeldegradation: 无法解析模型列表响应")
}
