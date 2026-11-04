package modeldegradation

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// 本文件是这个功能唯一的高风险面：用户填写任意 URL，服务端去请求它。
//
// 这是 SSRF 的教科书场景 —— 攻击者可以让服务器去访问
// 云元数据服务（169.254.169.254）、内网管理台（10.x / 192.168.x）、
// 本机回环（127.0.0.1:6379 的 Redis）等等。
//
// 防护不是「校验一次字符串」就够的，因为可以绕过：
//   - http://127.0.0.1          直白
//   - http://2130706433         十进制 IP
//   - http://0x7f.1             十六进制/混合进制
//   - http://[::1]              IPv6 回环
//   - http://evil.com  A 记录指向 127.0.0.1   DNS rebinding 的第一跳
//   - http://127.0.0.1.nip.io   同上，通配 DNS
//
// 因此本文件的策略是「解析后按 IP 判定」，而不是匹配字符串。
// 校验分两处执行：
//  1. 请求前（ValidateBaseURL）：格式 + 解析 + IP 段检查
//  2. 拨号时（NewSafeDialer）：再查一次实际要连的 IP
//     第 2 步是关键 —— 防的就是 DNS rebinding：
//     第 1 步解析到公网 IP 通过校验，真实拨号时 DNS 已被换成内网 IP。

// 被禁止的 IP 段。全部按「解析后的 IP」判断，不做字符串匹配。
var blockedNets = func() []*net.IPNet {
	cidrs := []string{
		"0.0.0.0/8",          // 本网络
		"10.0.0.0/8",         // 私有
		"100.64.0.0/10",      // CGNAT
		"127.0.0.0/8",        // 回环
		"169.254.0.0/16",     // 链路本地，含云元数据 169.254.169.254
		"172.16.0.0/12",      // 私有
		"192.0.0.0/24",       // IETF 协议分配
		"192.0.2.0/24",       // TEST-NET-1
		"192.168.0.0/16",     // 私有
		"198.18.0.0/15",      // 基准测试
		"198.51.100.0/24",    // TEST-NET-2
		"203.0.113.0/24",     // TEST-NET-3
		"224.0.0.0/4",        // 组播
		"240.0.0.0/4",        // 保留
		"255.255.255.255/32", // 广播
		"::1/128",            // IPv6 回环
		"::/128",             // 未指定
		"fc00::/7",           // IPv6 唯一本地
		"fe80::/10",          // IPv6 链路本地
		"ff00::/8",           // IPv6 组播
		"64:ff9b::/96",       // NAT64，可映射到内网 IPv4
		"2002::/16",          // 6to4，可映射到内网 IPv4
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

// ErrBaseURLBlockedScheme 表示协议不是 HTTPS。
var ErrBaseURLBlockedScheme = errors.New("modeldegradation: 上游地址必须是 HTTPS")

// ErrBaseURLBlockedHost 表示目标主机落在禁止的内网/保留段。
var ErrBaseURLBlockedHost = errors.New("modeldegradation: 上游地址不允许指向内网或保留地址")

// ErrBaseURLInvalid 表示地址本身格式不合法。
var ErrBaseURLInvalid = errors.New("modeldegradation: 上游地址格式不合法")

// IsBlockedIP 判断某个 IP 是否落在禁止段。
//
// 单独导出是为了让「拨号时复查」与「请求前校验」共用同一份判断，
// 避免两处规则漂移 —— 那种漂移会造成「校验说可以、拨号说不可以」
// 或者更糟的「校验拦了、拨号放行」。
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	// IPv4 映射的 IPv6 地址（::ffff:127.0.0.1）必须先归一化，
	// 否则会被当成普通 IPv6 地址而绕过 IPv4 段的检查。
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateBaseURL 校验用户填写的上游地址。
//
// 返回归一化后的 URL（去掉末尾斜杠），以及一个可直接使用的 *url.URL。
// resolver 允许注入（测试用）；为 nil 时用系统解析。
//
// 只允许 HTTPS：明文 HTTP 会让 api_key 在网络上是裸的，而用户填的是
// 真实凭据 —— 这个风险不能由「方便测试」来换。
func ValidateBaseURL(ctx context.Context, raw string, resolver *net.Resolver) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, ErrBaseURLInvalid
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, ErrBaseURLInvalid
	}

	if !strings.EqualFold(parsed.Scheme, "https") {
		return nil, ErrBaseURLBlockedScheme
	}
	if parsed.Host == "" {
		return nil, ErrBaseURLInvalid
	}
	// 不接受 URL 里带用户信息：https://user:pass@host 这类形态会让
	// 凭据出现在 base_url 里，而我们承诺只保存脱敏展示形态。
	if parsed.User != nil {
		return nil, ErrBaseURLInvalid
	}

	host := parsed.Hostname()
	if host == "" {
		return nil, ErrBaseURLInvalid
	}

	// 字面 IP 直接判，不用查 DNS。
	if ip := net.ParseIP(host); ip != nil {
		if IsBlockedIP(ip) {
			return nil, ErrBaseURLBlockedHost
		}
		return parsed, nil
	}

	// 域名：解析后逐个 IP 检查。任何一个落在禁止段就拒绝整个地址 ——
	// 允许多 A 记录里的一个内网地址，等于给了 DNS rebinding 一个入口。
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if ctx == nil {
		ctx = context.Background()
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	addrs, err := resolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return nil, fmt.Errorf("%w: 无法解析 %s", ErrBaseURLInvalid, host)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%w: %s 无解析结果", ErrBaseURLInvalid, host)
	}
	for _, addr := range addrs {
		if IsBlockedIP(addr.IP) {
			return nil, ErrBaseURLBlockedHost
		}
	}

	return parsed, nil
}

// NewSafeDialer 返回一个在拨号时复查目标 IP 的拨号器。
//
// 这一步不能省：ValidateBaseURL 与真实拨号之间存在时间差，DNS 可以在这
// 期间被换成内网地址（DNS rebinding）。拨号时拿到的是「实际要连的 IP」，
// 此时再查一次才是可靠的。
func NewSafeDialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		// Control 在 socket 连接建立前被调用，addr 是即将连接的地址。
		// 在这里拒绝比在 RoundTripper 里拦更可靠。
		Control: func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil {
				// 走到这里说明地址不是 IP，理论上不会发生；
				// 保守起见直接拒绝。
				return ErrBaseURLBlockedHost
			}
			if IsBlockedIP(ip) {
				return ErrBaseURLBlockedHost
			}
			return nil
		},
	}
}

// SanitizeBaseURLForDisplay 生成写进数据库的脱敏展示形态。
//
// 形如 https://api.example.com/*** —— 保留 scheme 与 host，
// 路径统一替换成 ***。理由：
//   - host 是历史墙「按主机隐藏」的维度，必须保留
//   - 路径可能带用户私有信息（租户 ID、token 前缀），不该落库
//
// 不做「保留前若干字符」那种脱敏：路径前缀同样可能泄漏信息，
// 而且会让同一上游的不同用户记录看起来不一致。
func SanitizeBaseURLForDisplay(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	host := parsed.Host
	if host == "" {
		return ""
	}
	return "https://" + host + "/***"
}

// HostOf 提取小写 host（不含端口），用于历史墙的主机维度。
func HostOf(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
