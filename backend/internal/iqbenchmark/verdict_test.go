package iqbenchmark_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// 本文件穷举判定规则。
//
// 判定是这一版最容易悄悄出错的逻辑：错一个分支，通过率曲线就是错的，
// 而且错得"看起来合理"（数字还会动）。所以这里不只测正常路径，
// 更要把「说不清」的每种情况都钉死到具体取值。

// ---------- VerdictForCase ----------

func TestVerdictForCaseJudgesAgainstExpected(t *testing.T) {
	answerCase := &iqbenchmark.Case{Expected: "42"}

	t.Run("答案正确是 ok", func(t *testing.T) {
		require.Equal(t, iqbenchmark.VerdictOK,
			iqbenchmark.VerdictForCase(iqbenchmark.ArtifactStatusSuccess, answerCase, "42", true))
	})

	t.Run("答案错误是 degraded", func(t *testing.T) {
		require.Equal(t, iqbenchmark.VerdictDegraded,
			iqbenchmark.VerdictForCase(iqbenchmark.ArtifactStatusSuccess, answerCase, "43", true))
	})

	t.Run("空答案永远不算通过", func(t *testing.T) {
		// 空正文配空 expected 时，normalizeAnswer 两侧都是 ""，
		// 天真实现会判成相等。这里必须被 hasArtifact / 空正文分支拦下。
		require.Equal(t, iqbenchmark.VerdictDegraded,
			iqbenchmark.VerdictForCase(iqbenchmark.ArtifactStatusSuccess, answerCase, "   ", true))
	})

	t.Run("无 expected 是 generated 而不是 ok", func(t *testing.T) {
		genCase := &iqbenchmark.Case{Expected: ""}
		require.Equal(t, iqbenchmark.VerdictGenerated,
			iqbenchmark.VerdictForCase(iqbenchmark.ArtifactStatusSuccess, genCase, "随便什么", true))
	})

	t.Run("题目已删除（nil）也是 generated", func(t *testing.T) {
		require.Equal(t, iqbenchmark.VerdictGenerated,
			iqbenchmark.VerdictForCase(iqbenchmark.ArtifactStatusSuccess, nil, "随便什么", true))
	})
}

func TestVerdictForCaseNeverPassesFailedCall(t *testing.T) {
	answerCase := &iqbenchmark.Case{Expected: "42"}

	// 调用失败即使正文恰好等于 expected，也不能算 ok：
	// 那多半是错误响应体碰巧撞上，按通过记账会把降级洗成正常。
	for _, status := range []string{
		iqbenchmark.ArtifactStatusFailed,
		iqbenchmark.ArtifactStatusTimeout,
		iqbenchmark.ArtifactStatusBlocked,
	} {
		t.Run(status, func(t *testing.T) {
			require.Equal(t, iqbenchmark.VerdictDegraded,
				iqbenchmark.VerdictForCase(status, answerCase, "42", true))
		})
	}
}

func TestVerdictForCaseUnknownStatusIsDegradedNotUnknown(t *testing.T) {
	// unknown 的语义是「没有产出」。已经执行过但状态不认识，
	// 应该保守判降级，而不是伪装成「没测」。
	require.Equal(t, iqbenchmark.VerdictDegraded,
		iqbenchmark.VerdictForCase("some_future_status", &iqbenchmark.Case{Expected: "x"}, "x", true))
}

func TestVerdictForCaseNoArtifactIsUnknown(t *testing.T) {
	// 调用成功但没有正文：既不是通过也不是降级，是「没拿到东西」。
	require.Equal(t, iqbenchmark.VerdictUnknown,
		iqbenchmark.VerdictForCase(iqbenchmark.ArtifactStatusSuccess, &iqbenchmark.Case{Expected: "42"}, "", false))
}

// ---------- AnswersMatch 归一化 ----------

func TestAnswersMatchNormalization(t *testing.T) {
	cases := []struct {
		name     string
		got      string
		expected string
		want     bool
	}{
		{"完全相同", "42", "42", true},
		{"大小写不敏感", "Paris", "paris", true},
		{"首尾空白", "  42  ", "42", true},
		{"内部连续空白折叠", "a    b", "a b", true},
		{"全角数字", "４２", "42", true},
		{"全角空格", "a\u3000b", "a b", true},
		{"全角标点", "（a）", "(a)", true},
		{"换行与制表", "a\n\tb", "a b", true},
		{"确实不同", "43", "42", false},
		// 这两条是刻意设计的一部分：不做去标点归一化。
		// 去了标点会让 "3.14" 与 "314" 相等，制造假通过。
		{"小数点必须保留", "3.14", "314", false},
		{"标点差异不算相等", "不是。", "不是", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, iqbenchmark.AnswersMatch(tc.got, tc.expected))
		})
	}
}

// ---------- VerdictForRender ----------

func TestVerdictForRender(t *testing.T) {
	t.Run("形态一致是 ok", func(t *testing.T) {
		require.Equal(t, iqbenchmark.RenderVerdictOK,
			iqbenchmark.VerdictForRender(iqbenchmark.ArtifactStatusSuccess,
				iqbenchmark.RenderKindSVG, iqbenchmark.RenderKindSVG, true))
	})

	t.Run("形态不一致是 degraded", func(t *testing.T) {
		// 该出 SVG 却吐了纯文本：渲染失败，但和答案对错无关。
		require.Equal(t, iqbenchmark.RenderVerdictDegraded,
			iqbenchmark.VerdictForRender(iqbenchmark.ArtifactStatusSuccess,
				iqbenchmark.RenderKindSVG, iqbenchmark.RenderKindText, true))
	})

	t.Run("题目不限定形态时任何形态都合格", func(t *testing.T) {
		require.Equal(t, iqbenchmark.RenderVerdictOK,
			iqbenchmark.VerdictForRender(iqbenchmark.ArtifactStatusSuccess, "", iqbenchmark.RenderKindMarkdown, true))
	})

	t.Run("无产出时是 unknown", func(t *testing.T) {
		require.Equal(t, iqbenchmark.RenderVerdictUnknown,
			iqbenchmark.VerdictForRender(iqbenchmark.ArtifactStatusFailed,
				iqbenchmark.RenderKindSVG, iqbenchmark.RenderKindSVG, false))
	})

	t.Run("形态无法识别时是 unknown", func(t *testing.T) {
		require.Equal(t, iqbenchmark.RenderVerdictUnknown,
			iqbenchmark.VerdictForRender(iqbenchmark.ArtifactStatusSuccess,
				iqbenchmark.RenderKindSVG, "", true))
	})
}

// ---------- AggregateRunVerdict ----------

func TestAggregateRunVerdict(t *testing.T) {
	cases := []struct {
		name                      string
		total, logicTotal, passed int
		want                      string
	}{
		{"全部通过是 normal", 3, 3, 3, iqbenchmark.RunVerdictNormal},
		{"部分通过也是 degraded", 3, 3, 2, iqbenchmark.RunVerdictDegraded},
		{"全部不通过是 degraded", 3, 3, 0, iqbenchmark.RunVerdictDegraded},
		// 纯生成模式：有产出但没有一道题可判（图片题）。
		{"无可判题目是 generated", 3, 0, 0, iqbenchmark.RunVerdictGenerated},
		{"没有任何产出是 unknown", 0, 0, 0, iqbenchmark.RunVerdictUnknown},
		// 混合：2 张图 + 1 道判对的题 → 可判分母只有 1，全部通过。
		{"图中混入判对题是 normal", 3, 1, 1, iqbenchmark.RunVerdictNormal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, iqbenchmark.AggregateRunVerdict(tc.total, tc.logicTotal, tc.passed))
		})
	}
}

func TestAggregateRunVerdictGeneratedRequiresZeroLogicTotal(t *testing.T) {
	// 这条是参考实现里 `verdict === "generated" && logic_total === 0`
	// 那个特例对应的不变量：只要有一道可判题目，就不能是 generated。
	require.NotEqual(t, iqbenchmark.RunVerdictGenerated, iqbenchmark.AggregateRunVerdict(1, 1, 1))
	require.NotEqual(t, iqbenchmark.RunVerdictGenerated, iqbenchmark.AggregateRunVerdict(1, 1, 0))
}

// ---------- 12 类错误归一化 ----------

func TestClassifyErrorKindByStatusCode(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusUnauthorized, iqbenchmark.ErrorKindAuthFailed},
		{http.StatusForbidden, iqbenchmark.ErrorKindForbidden},
		{http.StatusNotFound, iqbenchmark.ErrorKindNotFound},
		{http.StatusRequestTimeout, iqbenchmark.ErrorKindGatewayTimeout},
		{http.StatusGatewayTimeout, iqbenchmark.ErrorKindGatewayTimeout},
		{http.StatusTooManyRequests, iqbenchmark.ErrorKindRateLimited},
		{http.StatusPaymentRequired, iqbenchmark.ErrorKindQuotaExceeded},
		{http.StatusInternalServerError, iqbenchmark.ErrorKindServerError},
		{http.StatusBadGateway, iqbenchmark.ErrorKindServerError},
		{http.StatusServiceUnavailable, iqbenchmark.ErrorKindServerError},
		// 其余 5xx 也要落到 server_error，不能漏到 generic。
		{599, iqbenchmark.ErrorKindServerError},
		// 2xx/3xx/4xx 里没有特定语义的，落 generic。
		{418, iqbenchmark.ErrorKindGeneric},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			got := iqbenchmark.ClassifyErrorKind(errors.New("boom"), tc.status)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestClassifyErrorKindSentinelsWinOverStatus(t *testing.T) {
	// 上下文超时优先于状态码：它是我们自己放弃的，排查方向完全不同。
	require.Equal(t, iqbenchmark.ErrorKindTimeout,
		iqbenchmark.ClassifyErrorKind(context.DeadlineExceeded, http.StatusGatewayTimeout))

	require.Equal(t, iqbenchmark.ErrorKindTimeout,
		iqbenchmark.ClassifyErrorKind(iqbenchmark.ErrDeadlineExceeded, 0))
}

func TestClassifyErrorKindNetTimeout(t *testing.T) {
	timeoutErr := &fakeNetError{timeout: true}
	require.Equal(t, iqbenchmark.ErrorKindTimeout, iqbenchmark.ClassifyErrorKind(timeoutErr, 0))

	nonTimeout := &fakeNetError{timeout: false}
	require.Equal(t, iqbenchmark.ErrorKindGeneric, iqbenchmark.ClassifyErrorKind(nonTimeout, 0))
}

func TestClassifyErrorKindGatewayUnreachableIsGeneric(t *testing.T) {
	// 网关不可达说明请求没到上游，不属于「上游返回了什么」，
	// 所以归 generic 而不是 server_error。
	require.Equal(t, iqbenchmark.ErrorKindGeneric,
		iqbenchmark.ClassifyErrorKind(iqbenchmark.ErrGatewayUnreachable, 0))
}

func TestClassifyErrorKindNilIsGeneric(t *testing.T) {
	require.Equal(t, iqbenchmark.ErrorKindGeneric, iqbenchmark.ClassifyErrorKind(nil, 0))
}

// ---------- 错误码反查 ----------

func TestErrorKindCoversEveryErrorCode(t *testing.T) {
	// 穷举 13 个内部码，确保每个都有归类。
	// 新增 ErrCode* 却忘了写映射时，这条会立刻失败，
	// 而不是等到线上展示成 generic。
	codes := []string{
		iqbenchmark.ErrCodeNoCases,
		iqbenchmark.ErrCodeGroupMissing,
		iqbenchmark.ErrCodeUpstreamError,
		iqbenchmark.ErrCodeRateLimited,
		iqbenchmark.ErrCodeTimeout,
		iqbenchmark.ErrCodeContentBlocked,
		iqbenchmark.ErrCodeInsufficientBalance,
		iqbenchmark.ErrCodeUnsafeContent,
		iqbenchmark.ErrCodeNoArtifact,
		iqbenchmark.ErrCodeStorageDisabled,
		iqbenchmark.ErrCodeTruncated,
		iqbenchmark.ErrCodeGatewayUnreachable,
		iqbenchmark.ErrCodeCapabilityMissing,
	}
	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			require.True(t, iqbenchmark.ErrorKindCoversCode(code), "错误码 %q 必须有归类", code)
			kind := iqbenchmark.ErrorKindFromCode(code)
			require.True(t, iqbenchmark.IsKnownErrorKind(kind), "%q 的归类 %q 必须落在 12 类内", code, kind)
			require.NotEmpty(t, kind)
		})
	}
}

func TestErrorKindFromCodeUnknown(t *testing.T) {
	require.Empty(t, iqbenchmark.ErrorKindFromCode(""), "空码表示没有错误，不能编造归类")
	require.Equal(t, iqbenchmark.ErrorKindGeneric, iqbenchmark.ErrorKindFromCode("no_such_code"))
}

func TestErrorKindsListHasTwelveDistinctEntries(t *testing.T) {
	require.Len(t, iqbenchmark.ErrorKinds, 12, "参考实现是 12 类")
	seen := map[string]struct{}{}
	for _, kind := range iqbenchmark.ErrorKinds {
		require.NotEmpty(t, kind)
		_, dup := seen[kind]
		require.False(t, dup, "归类 %q 重复", kind)
		seen[kind] = struct{}{}
	}
}

func TestIsKnownErrorKind(t *testing.T) {
	require.True(t, iqbenchmark.IsKnownErrorKind(""), "空表示无错误，合法")
	for _, kind := range iqbenchmark.ErrorKinds {
		require.True(t, iqbenchmark.IsKnownErrorKind(kind))
	}
	require.False(t, iqbenchmark.IsKnownErrorKind("meltdown"))
}

// ---------- 内容特征兜底 ----------

func TestErrorKindFromText(t *testing.T) {
	require.Equal(t, iqbenchmark.ErrorKindResponseIncomplete,
		iqbenchmark.ErrorKindFromText("半截内容", true), "被截断优先于其它特征")
	require.Equal(t, iqbenchmark.ErrorKindEmptyResponse, iqbenchmark.ErrorKindFromText("   ", false))
	require.Empty(t, iqbenchmark.ErrorKindFromText("正常内容", false), "正常内容不该编造归类")
}

// fakeNetError 让测试能构造 net.Error 的超时与非超时两种形态。
type fakeNetError struct{ timeout bool }

func (e *fakeNetError) Error() string   { return "fake net error" }
func (e *fakeNetError) Timeout() bool   { return e.timeout }
func (e *fakeNetError) Temporary() bool { return false }

var _ net.Error = (*fakeNetError)(nil)
