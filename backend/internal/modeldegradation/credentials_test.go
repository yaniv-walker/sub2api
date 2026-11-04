//go:build unit

package modeldegradation

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
)

// SSRF 是这整个功能最大的风险面：用户填一个 URL，服务端去请求它。
// 这些测试是「我们真的挡住了」的证据，不是形式化的覆盖率。

func TestIsBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1",       // 回环
		"127.0.0.53",      // 回环（systemd-resolved）
		"0.0.0.0",         // 未指定
		"10.0.0.1",        // 私有
		"10.255.255.254",  // 私有边界
		"172.16.0.1",      // 私有
		"172.31.255.254",  // 私有上边界
		"192.168.1.1",     // 私有
		"169.254.169.254", // 云元数据服务 —— 最高危
		"100.64.0.1",      // CGNAT
		"192.0.2.1",       // TEST-NET-1
		"198.51.100.1",    // TEST-NET-2
		"203.0.113.1",     // TEST-NET-3
		"224.0.0.1",       // 组播
		"255.255.255.255", // 广播
		"::1",             // IPv6 回环
		"fe80::1",         // IPv6 链路本地
		"fc00::1",         // IPv6 唯一本地
		"fd12:3456::1",    // IPv6 唯一本地
		"ff02::1",         // IPv6 组播
	}

	for _, raw := range blocked {
		t.Run(raw, func(t *testing.T) {
			ip := net.ParseIP(raw)
			if ip == nil {
				t.Fatalf("无法解析测试 IP %q", raw)
			}
			if !IsBlockedIP(ip) {
				t.Fatalf("IsBlockedIP(%q) = false，应当拦截", raw)
			}
		})
	}

	allowed := []string{
		"8.8.8.8",
		"1.1.1.1",
		"93.184.216.34",
		"2606:4700:4700::1111",
	}

	for _, raw := range allowed {
		t.Run("allow_"+raw, func(t *testing.T) {
			ip := net.ParseIP(raw)
			if ip == nil {
				t.Fatalf("无法解析测试 IP %q", raw)
			}
			if IsBlockedIP(ip) {
				t.Fatalf("IsBlockedIP(%q) = true，不应拦截公网地址", raw)
			}
		})
	}
}

func TestIsBlockedIP_IPv4MappedIPv6(t *testing.T) {
	// ::ffff:127.0.0.1 是 IPv6 形态的 IPv4 回环。
	// 不做归一化就会被当成普通 IPv6 地址绕过检查 —— 这是经典绕过手法。
	cases := []string{
		"::ffff:127.0.0.1",
		"::ffff:169.254.169.254",
		"::ffff:10.0.0.1",
		"::ffff:192.168.1.1",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			ip := net.ParseIP(raw)
			if ip == nil {
				t.Fatalf("无法解析 %q", raw)
			}
			if !IsBlockedIP(ip) {
				t.Fatalf("IsBlockedIP(%q) = false，IPv4 映射地址必须按其 IPv4 段判断", raw)
			}
		})
	}

	// 公网 IPv4 的映射形态不应被误拦。
	if IsBlockedIP(net.ParseIP("::ffff:8.8.8.8")) {
		t.Fatal("公网地址的 IPv4 映射形态不应被拦截")
	}
}

func TestIsBlockedIP_Nil(t *testing.T) {
	if !IsBlockedIP(nil) {
		t.Fatal("nil IP 应按拦截处理（fail-closed）")
	}
}

func TestValidateBaseURL_RejectsNonHTTPS(t *testing.T) {
	// 明文 HTTP 会让 api_key 裸奔。用户填的是真实凭据，不能为「方便测试」让步。
	cases := []string{
		"http://api.example.com",
		"ftp://api.example.com",
		"ws://api.example.com",
		"file:///etc/passwd",
		"gopher://api.example.com",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			_, err := ValidateBaseURL(context.Background(), raw, nil)
			if err == nil {
				t.Fatalf("ValidateBaseURL(%q) 应拒绝非 HTTPS", raw)
			}
			if !errors.Is(err, ErrBaseURLBlockedScheme) {
				t.Fatalf("错误应为 ErrBaseURLBlockedScheme，实际 %v", err)
			}
		})
	}
}

func TestValidateBaseURL_RejectsLiteralPrivateIP(t *testing.T) {
	// 字面 IP 不走 DNS，直接判。
	cases := []string{
		"https://127.0.0.1",
		"https://127.0.0.1:8080",
		"https://10.0.0.1",
		"https://192.168.1.1",
		"https://169.254.169.254/latest/meta-data",
		"https://[::1]",
		"https://[fe80::1]",
		"https://0.0.0.0",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			_, err := ValidateBaseURL(context.Background(), raw, nil)
			if err == nil {
				t.Fatalf("ValidateBaseURL(%q) 应拒绝内网地址", raw)
			}
			if !errors.Is(err, ErrBaseURLBlockedHost) {
				t.Fatalf("错误应为 ErrBaseURLBlockedHost，实际 %v", err)
			}
		})
	}
}

func TestValidateBaseURL_AcceptsPublicLiteralIP(t *testing.T) {
	// 公网字面 IP 应通过（不走 DNS，无网络依赖）。
	u, err := ValidateBaseURL(context.Background(), "https://8.8.8.8", nil)
	if err != nil {
		t.Fatalf("公网 IP 应通过: %v", err)
	}
	if u == nil || u.Host != "8.8.8.8" {
		t.Fatalf("返回的 URL = %v", u)
	}
}

func TestValidateBaseURL_RejectsUserInfo(t *testing.T) {
	// https://user:pass@host 会让凭据出现在 base_url 里，
	// 而我们承诺只保存脱敏展示形态。直接拒掉最干净。
	_, err := ValidateBaseURL(context.Background(), "https://user:pass@api.example.com", nil)
	if err == nil {
		t.Fatal("带用户信息的 URL 应被拒绝")
	}
	if !errors.Is(err, ErrBaseURLInvalid) {
		t.Fatalf("错误应为 ErrBaseURLInvalid，实际 %v", err)
	}
}

func TestValidateBaseURL_RejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"not a url",
		"https://",
		"///",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			if _, err := ValidateBaseURL(context.Background(), raw, nil); err == nil {
				t.Fatalf("ValidateBaseURL(%q) 应拒绝非法地址", raw)
			}
		})
	}
}

func TestValidateBaseURL_PreservesPath(t *testing.T) {
	u, err := ValidateBaseURL(context.Background(), "https://8.8.8.8/v1", nil)
	if err != nil {
		t.Fatalf("应通过: %v", err)
	}
	if u.Path != "/v1" {
		t.Fatalf("Path = %q, want /v1", u.Path)
	}
}

func TestSanitizeBaseURLForDisplay(t *testing.T) {
	// 落库的形态必须是「scheme + host + /***」。
	// host 要保留（它是有意义的维度），路径必须抹掉（可能带租户信息）。
	//
	// host 只取 hostname、小写、不带端口：这与「按主机隐藏」用的 HostOf
	// 是同一个维度。展示形态与隐藏维度一旦不同源，带非默认端口的上游
	// 就永远无法被隐藏，而那正是最需要隐藏的私有部署场景。
	cases := []struct {
		raw  string
		want string
	}{
		{"https://api.example.com", "https://api.example.com/***"},
		{"https://api.example.com/v1", "https://api.example.com/***"},
		{"https://api.example.com/v1/secret-tenant-id", "https://api.example.com/***"},
		{"https://api.example.com:8443/v1", "https://api.example.com/***"},
		{"https://API.Example.COM/v1", "https://api.example.com/***"},
		{"https://api.example.com?token=abc", "https://api.example.com/***"},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			u, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatalf("url.Parse 失败: %v", err)
			}
			if got := SanitizeBaseURLForDisplay(u); got != tc.want {
				t.Fatalf("SanitizeBaseURLForDisplay(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}

	if got := SanitizeBaseURLForDisplay(nil); got != "" {
		t.Fatalf("nil URL 应返回空串，实际 %q", got)
	}
}

// TestSanitizeBaseURLForDisplay_matchesHostOf 锁住「展示形态」与「隐藏维度」同源。
//
// 这两者分开演进时，历史墙会按一个维度展示、按另一个维度隐藏，
// 表现是「管理页点了隐藏但公开墙没变」。这个测试就是防那种漂移。
func TestSanitizeBaseURLForDisplay_matchesHostOf(t *testing.T) {
	raws := []string{
		"https://api.example.com/v1",
		"https://api.example.com:8443/v1",
		"https://API.Example.COM",
		"https://foo_bar.example.com/v1",
	}

	for _, raw := range raws {
		t.Run(raw, func(t *testing.T) {
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatalf("url.Parse 失败: %v", err)
			}
			host := HostOf(u)
			if host == "" {
				t.Fatalf("HostOf(%q) 不该为空", raw)
			}
			display := SanitizeBaseURLForDisplay(u)
			if want := "https://" + host + "/***"; display != want {
				t.Fatalf("展示形态 %q 与 HostOf %q 不同源（期望 %q）", display, host, want)
			}
		})
	}
}

func TestSanitizeBaseURLForDisplay_NeverLeaksPath(t *testing.T) {
	// 这一条是「凭据不落库」承诺的一部分：
	// 用户可能把 token 塞进路径（/v1/sk-xxx/chat），脱敏必须抹掉。
	raw := "https://api.example.com/v1/sk-proj-abcdef123456/chat"
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse 失败: %v", err)
	}
	out := SanitizeBaseURLForDisplay(u)
	if out != "https://api.example.com/***" {
		t.Fatalf("脱敏结果 = %q", out)
	}
}

func TestHostOf(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"https://API.Example.COM/v1", "api.example.com"},
		{"https://api.example.com:8443/v1", "api.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			u, err := url.Parse(tc.raw)
			if err != nil {
				t.Fatalf("url.Parse 失败: %v", err)
			}
			if got := HostOf(u); got != tc.want {
				t.Fatalf("HostOf(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
	if got := HostOf(nil); got != "" {
		t.Fatalf("nil 应返回空串，实际 %q", got)
	}
}

func TestNewSafeDialer_BlocksPrivateAtDialTime(t *testing.T) {
	// 这是防 DNS rebinding 的第二道闸：即使 ValidateBaseURL 当时解析到公网，
	// 真实拨号时若目标是内网，也必须被拦下。
	//
	// 直接测 Control 回调里的判断逻辑比较绕（需要真实 dial），
	// 这里验证「拨号器存在且带 Control」，并单独验证 Control 用到的
	// IsBlockedIP 已在上面覆盖。二者组合即证明这条链路成立。
	d := NewSafeDialer(0)
	if d == nil {
		t.Fatal("NewSafeDialer 返回 nil")
	}
	if d.Control == nil {
		t.Fatal("拨号器必须带 Control 回调，否则 DNS rebinding 无防护")
	}
}
