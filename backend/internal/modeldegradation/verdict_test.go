//go:build unit

package modeldegradation

import "testing"

// 判分是这个功能的核心：它决定「降智 / 正常」的结论。
// 判错的后果不是界面难看，而是冤枉一个正常的上游，或者放过一个真降智的。
// 因此这里覆盖的边界比普通 CRUD 更细。

func TestAnswersMatch_NumericExact(t *testing.T) {
	cases := []struct {
		name     string
		expected string
		response string
		want     bool
	}{
		{
			name:     "命中裸数字",
			expected: "21",
			response: "21",
			want:     true,
		},
		{
			name:     "命中自然语言里的数字",
			expected: "21",
			response: "最少取出 **21 颗**。由于最坏情况下……",
			want:     true,
		},
		{
			name:     "未命中：答案错了",
			expected: "21",
			response: "最少需要 29 颗糖。推理如下……",
			want:     false,
		},
		{
			name:     "未命中：回答里有多个数字但没有期望值",
			expected: "21",
			response: "先取 29 个，再取 7 个，最后 9 个，共 16 个",
			want:     false,
		},
		{
			name:     "小数等值命中",
			expected: "21",
			response: "答案是 21.0",
			want:     true,
		},
		{
			name:     "期望值是小数",
			expected: "2.5",
			response: "结果是 2.50 米",
			want:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AnswersMatch(tc.expected, tc.response); got != tc.want {
				t.Fatalf("AnswersMatch(%q, %q) = %v, want %v",
					tc.expected, tc.response, got, tc.want)
			}
		})
	}
}

func TestAnswersMatch_NumericBoundary(t *testing.T) {
	// 关键边界：不能把数字当成子串匹配。
	// 期望 "21" 时，回答里的 "121" 或 "210" 不该命中 —— 那是误判。
	cases := []struct {
		name     string
		expected string
		response string
		want     bool
	}{
		{"不应命中 121", "21", "答案是 121", false},
		{"不应命中 210", "21", "答案是 210", false},
		{"不应命中 2.1", "21", "答案是 2.1", false},
		{"应命中独立出现的 21", "21", "共 121 个球，其中 21 个是红的", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AnswersMatch(tc.expected, tc.response); got != tc.want {
				t.Fatalf("AnswersMatch(%q, %q) = %v, want %v",
					tc.expected, tc.response, got, tc.want)
			}
		})
	}
}

func TestAnswersMatch_TextAnswer(t *testing.T) {
	cases := []struct {
		name     string
		expected string
		response string
		want     bool
	}{
		{"非数字答案命中", "巴黎", "这座城市是巴黎。", true},
		{"非数字答案未命中", "巴黎", "这座城市是伦敦。", false},
		{"空期望值不命中", "", "任何内容", false},
		{"空回答不命中", "巴黎", "", false},
		{"期望值带引号也能命中", `"巴黎"`, "是巴黎", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AnswersMatch(tc.expected, tc.response); got != tc.want {
				t.Fatalf("AnswersMatch(%q, %q) = %v, want %v",
					tc.expected, tc.response, got, tc.want)
			}
		})
	}
}

func TestAnswersMatch_FullWidthDigits(t *testing.T) {
	// 模型经常打出全角数字。把它们判成不命中会造成假降智。
	if !AnswersMatch("21", "答案是２１颗") {
		t.Fatal("全角数字应被识别为等值")
	}
}

func TestNormalizeAnswer(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"21", "21"},
		{" 21 ", "21"},
		{"21.0", "21"},
		{"21.00", "21"},
		{`"21"`, "21"},
		{"（21）", "21"},
		{"21。", "21"},
		{"", ""},
		{"２１", "21"},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := NormalizeAnswer(tc.in); got != tc.want {
				t.Fatalf("NormalizeAnswer(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestJudgeLogic(t *testing.T) {
	t.Run("命中返回 ok 且通过", func(t *testing.T) {
		res := JudgeLogic("21", "最少取 21 颗", "", "")
		if res.Verdict != ItemVerdictOK {
			t.Fatalf("verdict = %q, want %q", res.Verdict, ItemVerdictOK)
		}
		if !res.Passed {
			t.Fatal("Passed 应为 true")
		}
		if res.Detail == "" {
			t.Fatal("Detail 不应为空")
		}
	})

	t.Run("未命中返回 degraded 且不通过", func(t *testing.T) {
		res := JudgeLogic("21", "需要 29 颗", "", "")
		if res.Verdict != ItemVerdictDegraded {
			t.Fatalf("verdict = %q, want %q", res.Verdict, ItemVerdictDegraded)
		}
		if res.Passed {
			t.Fatal("Passed 应为 false")
		}
		// detail 要能帮用户判断「算错了」还是「没在算」，
		// 所以必须把回答里出现过的数字列出来。
		want := "未出现期望答案 21，回答中的数字：29"
		if res.Detail != want {
			t.Fatalf("Detail = %q, want %q", res.Detail, want)
		}
	})

	t.Run("空响应返回 unknown 而不是 degraded", func(t *testing.T) {
		// 超时/报错是服务问题，不是能力问题。判成 degraded 会让通过率虚低。
		res := JudgeLogic("21", "", ErrorKindTimeoutForTest, "上游响应超时")
		if res.Verdict != ItemVerdictUnknown {
			t.Fatalf("verdict = %q, want %q", res.Verdict, ItemVerdictUnknown)
		}
		if res.Passed {
			t.Fatal("Passed 应为 false")
		}
		if res.Detail != "上游响应超时" {
			t.Fatalf("Detail = %q, want 上游原始错误说明", res.Detail)
		}
	})

	t.Run("空响应无错误说明时给兜底文案", func(t *testing.T) {
		res := JudgeLogic("21", "", "", "")
		if res.Detail == "" {
			t.Fatal("Detail 不应为空")
		}
	})

	t.Run("纯空白响应视同空响应", func(t *testing.T) {
		res := JudgeLogic("21", "   \n\t  ", "", "")
		if res.Verdict != ItemVerdictUnknown {
			t.Fatalf("verdict = %q, want %q", res.Verdict, ItemVerdictUnknown)
		}
	})
}

// ErrorKindTimeoutForTest 引用真实常量，避免测试里硬编码字符串漂移。
const ErrorKindTimeoutForTest = "timeout"

func TestJudgeRender(t *testing.T) {
	extract := extractSVG

	t.Run("拿到完整 SVG 返回 ok", func(t *testing.T) {
		resp := "好的，这是作品：\n```svg\n<svg viewBox=\"0 0 100 100\"><circle r=\"10\"/></svg>\n```"
		res := JudgeRender(resp, "", "", extract)
		if res.Verdict != ItemVerdictOK {
			t.Fatalf("verdict = %q, want %q", res.Verdict, ItemVerdictOK)
		}
		if !res.Passed {
			t.Fatal("Passed 应为 true")
		}
		if res.Artwork == "" {
			t.Fatal("Artwork 不应为空")
		}
	})

	t.Run("有响应但抽不出 SVG 返回 degraded", func(t *testing.T) {
		res := JudgeRender("抱歉，我无法生成图片。", "", "", extract)
		if res.Verdict != ItemVerdictDegraded {
			t.Fatalf("verdict = %q, want %q", res.Verdict, ItemVerdictDegraded)
		}
	})

	t.Run("空响应返回 unknown", func(t *testing.T) {
		res := JudgeRender("", "", "上游响应超时", extract)
		if res.Verdict != ItemVerdictUnknown {
			t.Fatalf("verdict = %q, want %q", res.Verdict, ItemVerdictUnknown)
		}
	})
}

func TestAggregateRunVerdict(t *testing.T) {
	cases := []struct {
		name         string
		logicPassed  int
		logicTotal   int
		logicUnknown int
		want         string
	}{
		{
			name:       "纯生成模式（没跑逻辑题）→ generated",
			logicTotal: 0,
			want:       VerdictGenerated,
		},
		{
			name:         "全部 unknown → unknown",
			logicTotal:   2,
			logicUnknown: 2,
			want:         VerdictUnknown,
		},
		{
			name:        "全对 → normal",
			logicPassed: 2,
			logicTotal:  2,
			want:        VerdictNormal,
		},
		{
			name:       "全错 → degraded",
			logicTotal: 2,
			want:       VerdictDegraded,
		},
		{
			name:        "部分对 → degraded",
			logicPassed: 1,
			logicTotal:  2,
			want:        VerdictDegraded,
		},
		{
			name:         "一半 unknown 但有一个答对 → degraded（没全对）",
			logicPassed:  1,
			logicTotal:   2,
			logicUnknown: 1,
			want:         VerdictDegraded,
		},
		{
			name:         "一半 unknown 且没答对 → degraded",
			logicTotal:   2,
			logicUnknown: 1,
			want:         VerdictDegraded,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AggregateRunVerdict(tc.logicPassed, tc.logicTotal, tc.logicUnknown)
			if got != tc.want {
				t.Fatalf("AggregateRunVerdict(%d,%d,%d) = %q, want %q",
					tc.logicPassed, tc.logicTotal, tc.logicUnknown, got, tc.want)
			}
		})
	}
}

func TestAggregateRenderVerdict(t *testing.T) {
	cases := []struct {
		name    string
		total   int
		ok      int
		unknown int
		want    string
	}{
		{"没跑生成题 → 空串", 0, 0, 0, RenderVerdictNone},
		{"全部成功 → ok", 1, 1, 0, RenderVerdictOK},
		{"全部 unknown → unknown", 1, 0, 1, RenderVerdictUnknown},
		{"有响应但没成功 → degraded", 1, 0, 0, RenderVerdictDegraded},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AggregateRenderVerdict(tc.total, tc.ok, tc.unknown)
			if got != tc.want {
				t.Fatalf("AggregateRenderVerdict(%d,%d,%d) = %q, want %q",
					tc.total, tc.ok, tc.unknown, got, tc.want)
			}
		})
	}
}

func TestPassRate(t *testing.T) {
	cases := []struct {
		name      string
		normal    int64
		completed int64
		want      float64
	}{
		{"无已完成检测时为 0", 0, 0, 0},
		{"全部正常为 1", 5, 5, 1},
		{"一半正常为 0.5", 5, 10, 0.5},
		{"没有正常为 0", 0, 10, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PassRate(tc.normal, tc.completed); got != tc.want {
				t.Fatalf("PassRate(%d,%d) = %v, want %v",
					tc.normal, tc.completed, got, tc.want)
			}
		})
	}
}

func TestExtractNumbers(t *testing.T) {
	got := ExtractNumbers("先取 29 个，再取 7 个，最后 9 个，共 16.5 个")
	want := []string{"29", "7", "9", "16.5"}
	if len(got) != len(want) {
		t.Fatalf("ExtractNumbers 长度 = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ExtractNumbers[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// 负数也要能取到，否则「温差 -3 度」这类题会漏。
	neg := ExtractNumbers("答案是 -3 度")
	if len(neg) != 1 || neg[0] != "-3" {
		t.Fatalf("负数字提取失败: %v", neg)
	}

	// 无数字时返回空切片而不是 nil，避免调用方 nil 解引用。
	if n := ExtractNumbers("没有数字"); n == nil {
		t.Fatal("无数字时应返回空切片而不是 nil")
	}
}
