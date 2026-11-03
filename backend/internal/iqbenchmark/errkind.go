package iqbenchmark

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
)

// 本文件是「上游错误形态 → 12 类归一化」的纯函数层。
//
// 与 error_code 的分工必须说清，否则很容易被后来的人合并掉：
//
//	error_code（types.go 里的 ErrCode*，13 个）
//	    语义：「我们自己的失败原因」。它是内部码，驱动 QuotaExhausted 熔断，
//	    决定后续题目是否还要继续跑。改它等于改限流/熔断语义。
//	error_kind（本文件，12 类）
//	    语义：「上游是怎么坏的」。只用于展示、统计与归类，
//	    不参与任何控制流。新增或改错它最多是显示不准，不会让熔断失灵。
//
// 两条铁律：
//   - 本文件不得 import service 包，不得触碰任何 Store/Gateway；
//   - ErrCodeToErrorKind 的返回值必须落在 ErrorKinds 清单内，
//     单测会逐一穷举 ErrCode* 断言这一点。

// ClassifyErrorKind 把一次调用错误归一化成 12 类之一。
//
// 判定顺序刻意从"最确定"到"最不确定"：
// 先看上下文与哨兵错误（我们自己造的，语义确定），
// 再看 HTTP 状态码（上游给的，语义较确定），
// 最后才落到 generic（什么都看不出来）。
//
// 之所以不按"错误字符串包含 xxx"来判：那既不稳也危险 ——
// 上游换个措辞就全错，而且会把上游文本带进判定逻辑里。
func ClassifyErrorKind(err error, statusCode int) string {
	if err == nil {
		return ErrorKindGeneric
	}

	// ---- 第一层：我们自己的哨兵错误，语义最确定 ----

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrDeadlineExceeded) {
		// 本地等待超时与上游 504 要分开：前者是我们放弃了，
		// 后者是上游自己承认网关超时。排查方向完全不同。
		return ErrorKindTimeout
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return ErrorKindTimeout
	}

	if errors.Is(err, ErrGatewayUnreachable) {
		// 连不上网关：请求根本没到上游，不属于"上游返回了什么"。
		return ErrorKindGeneric
	}

	// ---- 第二层：HTTP 状态码 ----

	if kind, ok := errorKindByStatus(statusCode); ok {
		return kind
	}

	// ---- 第三层：兜底 ----

	return ErrorKindGeneric
}

// errorKindByStatus 是状态码到归类的表。
//
// 用表而不是 switch 的长 case，是为了让「12 类覆盖了哪些状态码」
// 一眼可数。单测遍历这张表跑，就不会出现"改了分支忘了改测试"。
func errorKindByStatus(code int) (string, bool) {
	switch code {
	case 0:
		// 没有状态码 = 没拿到响应，交由上层用哨兵错误判。
		return "", false
	case http.StatusUnauthorized:
		return ErrorKindAuthFailed, true
	case http.StatusForbidden:
		// 403 与 401 分开：401 是"钥匙不对"，403 是"钥匙对但不许"。
		// 二者的处置完全不同（换 Key vs 换号/提权）。
		return ErrorKindForbidden, true
	case http.StatusNotFound:
		// 404 在我们的语境里几乎总是"模型不存在"或"路径不对"。
		return ErrorKindNotFound, true
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		// 408 与 504 都归网关超时：都是"上游这一跳没在时限内答上来"。
		return ErrorKindGatewayTimeout, true
	case http.StatusTooManyRequests:
		return ErrorKindRateLimited, true
	case http.StatusPaymentRequired:
		// 与 429 分开：429 是太快了稍后再试，402 是没钱了再试也没用。
		return ErrorKindQuotaExceeded, true
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable:
		return ErrorKindServerError, true
	}
	if code >= 500 {
		// 其余 5xx 一律按服务端错误，避免漏网落到 generic。
		return ErrorKindServerError, true
	}
	return "", false
}

// ErrorKindFromCode 把内部 error_code 反查成 12 类。
//
// 用途：历史产出行只存了 error_code，没有 error_kind。
// 读路径用这个函数兜底推导，避免为了补一个展示字段去写回填迁移。
//
// 两个函数（ClassifyErrorKind 与 ErrorKindFromCode）的分工：
// 前者在**执行时**用（有真实 err 和状态码），信息最全；
// 后者在**读取时**用（只有落库的 code），信息较少但足够展示。
func ErrorKindFromCode(code string) string {
	if kind, ok := errorKindByCode[code]; ok {
		return kind
	}
	if strings.TrimSpace(code) == "" {
		return ""
	}
	return ErrorKindGeneric
}

// errorKindByCode 是内部码到归类的映射。
//
// 注意几个刻意的一对多：多个内部码可能归到同一类，
// 因为它们是"同一个上游问题的不同观察角度"。
// 反过来也有一个码进多类的错觉 —— 没有，每个码只进一类，
// 否则统计会重复计数。
var errorKindByCode = map[string]string{
	ErrCodeTimeout:             ErrorKindTimeout,
	ErrCodeRateLimited:         ErrorKindRateLimited,
	ErrCodeInsufficientBalance: ErrorKindQuotaExceeded,
	ErrCodeGatewayUnreachable:  ErrorKindGeneric,
	ErrCodeUpstreamError:       ErrorKindServerError,
	ErrCodeCapabilityMissing:   ErrorKindGeneric,
	ErrCodeNoArtifact:          ErrorKindEmptyResponse,
	ErrCodeContentBlocked:      ErrorKindForbidden,
	ErrCodeUnsafeContent:       ErrorKindForbidden,
	ErrCodeGroupMissing:        ErrorKindNotFound,
	ErrCodeStorageDisabled:     ErrorKindGeneric,
	ErrCodeTruncated:           ErrorKindResponseIncomplete,
	ErrCodeNoCases:             ErrorKindGeneric,
}

// ErrorKindCoversCode 判断错误码是否已被映射覆盖。
//
// 供单测穷举使用：新增一个 ErrCode* 却忘了写映射时，
// 这个函数会让测试立刻失败，而不是等到线上展示成 generic。
func ErrorKindCoversCode(code string) bool {
	_, ok := errorKindByCode[code]
	return ok
}

// IsKnownErrorKind 判断归类值是否在 12 类清单内。
//
// 空串合法（表示"没有错误"），其余必须命中清单。
func IsKnownErrorKind(kind string) bool {
	if kind == "" {
		return true
	}
	for _, k := range ErrorKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// ErrorKindFromText 是最后的兜底：从正文特征推断"响应不完整"。
//
// 单列出来是因为 streamBroken / responseIncomplete 这两类
// 无法从状态码看出来 —— 上游返 200 但内容断在半截，
// 这是流式/截断场景特有的，必须看内容。
//
// 只在已有产出但内容可疑时调用，不参与控制流。
func ErrorKindFromText(text string, truncated bool) string {
	if truncated {
		return ErrorKindResponseIncomplete
	}
	if strings.TrimSpace(text) == "" {
		return ErrorKindEmptyResponse
	}
	return ""
}
