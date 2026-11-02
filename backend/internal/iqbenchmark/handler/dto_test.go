//go:build unit

package handler

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// forbiddenPublicJSONFields 是公开 DTO 绝不允许出现的 JSON 字段名。
//
// 这份清单就是需求里「白名单」的反向断言：一旦有人图省事把内部实体
// 直接拷进公开 DTO，测试立刻失败。新增字段时必须显式来过这里，
// 等于强制一次「这是否真的可以公开」的评审。
var forbiddenPublicJSONFields = []string{
	// 内部账号与运行期身份
	"internal_user_id", "internal_api_key_id", "account_id", "user_id", "api_key_id",
	// 分组与任务内部结构
	"group_id", "group_name", "task_id", "run_id", "case_ids", "case_snapshot",
	// 存储与原始响应
	"raw_response_ref", "blob_key", "content_sha256", "request_params", "params", "system_prompt",
	// 运维与安全诊断信息
	"unsafe_flags", "error_code", "notes", "lease_owner", "lease_expires_at",
	"status_reason", "scored_by", "scoring", "llm_judge_enabled",
}

// jsonFieldNames 返回结构体上所有 json tag 的字段名（忽略 `-` 与空名）。
func jsonFieldNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// TestPublicArtifactDTOHasNoInternalFields 断言公开 DTO 不含任何内部字段。
func TestPublicArtifactDTOHasNoInternalFields(t *testing.T) {
	names := jsonFieldNames(reflect.TypeOf(PublicArtifactDTO{}))
	require.NotEmpty(t, names, "PublicArtifactDTO 必须至少有一个字段")

	for _, forbidden := range forbiddenPublicJSONFields {
		require.NotContains(t, names, forbidden,
			"PublicArtifactDTO 不得暴露 %q：这是内部字段，公开接口只允许白名单字段", forbidden)
	}
}

// TestPublicArtifactDTOWhitelistIsExact 固定公开作品 DTO 的字段集合。
//
// 用精确集合而不是「不包含黑名单」：任何新增字段都必须显式改本测试，
// 不会因为「不小心加了个字段但恰好不在黑名单里」而静默泄漏。
func TestPublicArtifactDTOWhitelistIsExact(t *testing.T) {
	allowed := []string{
		"id", "case_id", "case_name", "case_kind", "model", "render_kind",
		"image_url", "content", "truncated", "status", "error_summary",
		"score", "duration_ms", "is_unsafe", "source", "created_at",
		"cost", "input_tokens", "output_tokens",
	}
	actual := jsonFieldNames(reflect.TypeOf(PublicArtifactDTO{}))
	require.ElementsMatch(t, allowed, actual,
		"公开作品字段集合被固定；如需新增，请先更新 design.md 第 9.2 节与本断言")
}

// TestPublicArtifactDTONeverExposesTriggerUser 断言自测产出不泄露触发者身份。
//
// source 是「这条作品是怎么来的」（可以公开，用于渲染「用户自测」角标）；
// triggered_by_user_id 是「谁测的」（属于隐私，一旦下发就能把作品与具体用户关联）。
// 两者只差一个字段名，最容易在加 source 时顺手把 user_id 也带出去。
func TestPublicArtifactDTONeverExposesTriggerUser(t *testing.T) {
	names := jsonFieldNames(reflect.TypeOf(PublicArtifactDTO{}))
	require.Contains(t, names, "source", "source 是公开的（用于角标）")

	for _, forbidden := range []string{
		"triggered_by_user_id", "user_id", "triggered_by",
		"moderation_status", "api_key_id",
	} {
		require.NotContains(t, names, forbidden,
			"自测产出的 %q 属于内部/隐私字段，绝不可公开下发", forbidden)
	}
}

// TestPublicArtifactSourceFallsBackToScheduled 断言未知来源兜底为 scheduled。
//
// 兜底方向必须是 scheduled 而不是 user_test：把官方检测误标成「用户自测」
// 会削弱可信度；反过来标错只是少了一个角标。
func TestPublicArtifactSourceFallsBackToScheduled(t *testing.T) {
	require.Equal(t, iqbenchmark.SourceScheduled, publicArtifactSource(""))
	require.Equal(t, iqbenchmark.SourceScheduled, publicArtifactSource("bogus"))
	// all 是查询专用值，落库状态不合法，公开侧同样兜底。
	require.Equal(t, iqbenchmark.SourceScheduled, publicArtifactSource(iqbenchmark.SourceAll))
	require.Equal(t, iqbenchmark.SourceScheduled, publicArtifactSource(iqbenchmark.SourceScheduled))
	require.Equal(t, iqbenchmark.SourceUserTest, publicArtifactSource(iqbenchmark.SourceUserTest))
}

// TestPublicCaseDTOHasNoPromptInternals 断言公开题目 DTO 只有 5 个字段。
//
// system_prompt、params、scoring 一旦泄漏，等于把提示词工程细节
// 与评分标准交给被测方（模型厂商可直接针对评分标准优化）。
func TestPublicCaseDTOHasNoPromptInternals(t *testing.T) {
	names := jsonFieldNames(reflect.TypeOf(PublicCaseDTO{}))
	require.ElementsMatch(t,
		[]string{"id", "name", "kind", "expect_kind", "prompt"},
		names,
		"PublicCaseDTO 字段集合固定为 5 个；新增字段需同步更新 design.md 第 9.2 节")
}

// TestPublicOverviewBucketDTOExposesNoGroupIdentity 断言趋势桶不暴露分组身份。
func TestPublicOverviewBucketDTOExposesNoGroupIdentity(t *testing.T) {
	names := jsonFieldNames(reflect.TypeOf(PublicOverviewBucketDTO{}))
	require.NotContains(t, names, "group_id")
	require.NotContains(t, names, "group_name")
	require.Contains(t, names, "model")
}

// TestAllPublicDTOsAvoidForbiddenFields 对所有公开 DTO 统一跑一遍黑名单。
//
// 单为 PublicArtifactDTO 写断言容易漏掉后加的结构体，这里一次性覆盖
// 本包内全部 Public 前缀 DTO。
func TestAllPublicDTOsAvoidForbiddenFields(t *testing.T) {
	types := map[string]reflect.Type{
		"PublicArtifactDTO":       reflect.TypeOf(PublicArtifactDTO{}),
		"PublicCaseDTO":           reflect.TypeOf(PublicCaseDTO{}),
		"PublicOverviewDTO":       reflect.TypeOf(PublicOverviewDTO{}),
		"PublicOverviewBucketDTO": reflect.TypeOf(PublicOverviewBucketDTO{}),
	}
	for typeName, typ := range types {
		names := jsonFieldNames(typ)
		for _, forbidden := range forbiddenPublicJSONFields {
			require.NotContains(t, names, forbidden, "%s 不得暴露 %q", typeName, forbidden)
		}
	}
}

// TestToPublicArtifactHidesCostAndTokensByDefault 断言默认不暴露成本与 token。
//
// 默认必须是「不暴露」：成本属于运营信息，只有管理员显式打开
// expose_cost_to_user 或 expose_tokens_to_user 后才可公开。
func TestToPublicArtifactHidesCostAndTokensByDefault(t *testing.T) {
	artifact := &iqbenchmark.Artifact{
		ID:           7,
		Model:        "gpt-4o",
		RenderKind:   iqbenchmark.RenderKindText,
		Content:      "hello",
		Status:       iqbenchmark.ArtifactStatusSuccess,
		Cost:         0.42,
		InputTokens:  1234,
		OutputTokens: 5678,
	}

	dto := toPublicArtifact(artifact, PublicArtifactOptions{})
	require.Nil(t, dto.Cost, "默认不得暴露成本")
	require.Nil(t, dto.InputTokens, "默认不得暴露输入 token")
	require.Nil(t, dto.OutputTokens, "默认不得暴露输出 token")

	opts := PublicArtifactOptions{ExposeCost: true, ExposeTokens: true}
	shown := toPublicArtifact(artifact, opts)
	require.NotNil(t, shown.Cost)
	require.Equal(t, 0.42, *shown.Cost)
	require.NotNil(t, shown.InputTokens)
	require.Equal(t, 1234, *shown.InputTokens)
	require.NotNil(t, shown.OutputTokens)
	require.Equal(t, 5678, *shown.OutputTokens)
}

// TestToPublicArtifactBlockedHasNoContent 断言被拦截的作品不返回正文与图片。
//
// 即使数据库里保留了原始内容（供管理员排查），公开接口也不能吐出去：
// 否则一次 XSS 尝试就会原样渲染给所有登录用户。
func TestToPublicArtifactBlockedHasNoContent(t *testing.T) {
	artifact := &iqbenchmark.Artifact{
		ID:         1,
		Status:     iqbenchmark.ArtifactStatusBlocked,
		RenderKind: iqbenchmark.RenderKindImageURL,
		Content:    "<svg onload=alert(1)></svg>",
		ImageURL:   "https://example.com/a.png",
	}

	dto := toPublicArtifact(artifact, PublicArtifactOptions{})
	require.Empty(t, dto.Content, "blocked 作品不得返回正文")
	require.Empty(t, dto.ImageURL, "blocked 作品不得返回图片地址")
	require.True(t, dto.IsUnsafe, "blocked 作品应标记为 unsafe")
}

// TestToPublicArtifactSuccessKeepsContentByRenderKind 断言成功作品按渲染类型下发。
func TestToPublicArtifactSuccessKeepsContentByRenderKind(t *testing.T) {
	text := &iqbenchmark.Artifact{
		ID:         1,
		Status:     iqbenchmark.ArtifactStatusSuccess,
		RenderKind: iqbenchmark.RenderKindText,
		Content:    "answer",
		ImageURL:   "https://example.com/ignored.png",
	}
	dto := toPublicArtifact(text, PublicArtifactOptions{})
	require.Equal(t, "answer", dto.Content)
	require.Empty(t, dto.ImageURL, "文本作品不下发图片地址")

	image := &iqbenchmark.Artifact{
		ID:         2,
		Status:     iqbenchmark.ArtifactStatusSuccess,
		RenderKind: iqbenchmark.RenderKindImageURL,
		Content:    "ignored",
		ImageURL:   "https://example.com/a.png",
	}
	dto = toPublicArtifact(image, PublicArtifactOptions{})
	require.Equal(t, "https://example.com/a.png", dto.ImageURL)
	require.Empty(t, dto.Content, "图片作品不下发正文")
}

// TestPublicErrorSummaryNeverLeaksRawError 断言公开错误摘要不透传原始文本。
//
// 上游错误文本可能包含 base url、header、堆栈等运维信息，
// 公开侧必须走 sanitize.SafeSummary 的固定文案。
func TestPublicErrorSummaryNeverLeaksRawError(t *testing.T) {
	artifact := &iqbenchmark.Artifact{
		Status:       iqbenchmark.ArtifactStatusTimeout,
		ErrorCode:    iqbenchmark.ErrCodeTimeout,
		ErrorSummary: "Post \"https://api.internal.example/v1/chat\": context deadline exceeded",
	}
	summary := publicErrorSummary(artifact)

	require.NotContains(t, summary, "api.internal.example", "不得泄漏上游地址")
	require.NotContains(t, summary, "deadline exceeded", "不得透传原始英文错误")
	require.NotEmpty(t, summary, "已知错误码必须有可读的中文摘要")
}

// TestPublicErrorSummaryEmptyWhenNoErrorCode 断言无错误码时不编造摘要。
func TestPublicErrorSummaryEmptyWhenNoErrorCode(t *testing.T) {
	require.Empty(t, publicErrorSummary(nil))
	require.Empty(t, publicErrorSummary(&iqbenchmark.Artifact{ErrorSummary: "some raw text"}))
}

// TestScoringDisabledOmitsScore 断言评分开关关闭时不下发分数。
func TestScoringDisabledOmitsScore(t *testing.T) {
	score := 88.5
	artifact := &iqbenchmark.Artifact{
		ID:     1,
		Status: iqbenchmark.ArtifactStatusSuccess,
		Score:  &score,
	}

	off := toPublicArtifact(artifact, PublicArtifactOptions{ScoringEnabled: false})
	require.Nil(t, off.Score, "评分关闭时不得下发分数")

	on := toPublicArtifact(artifact, PublicArtifactOptions{ScoringEnabled: true})
	require.NotNil(t, on.Score)
	require.Equal(t, 88.5, *on.Score)
}

// TestToPublicArtifactUsesFrozenCaseSnapshot 断言题目名来自冻结快照。
//
// 题目后续改名或删除时，历史作品必须仍显示当时的题面，否则对比失真。
func TestToPublicArtifactUsesFrozenCaseSnapshot(t *testing.T) {
	artifact := &iqbenchmark.Artifact{
		ID:     1,
		Status: iqbenchmark.ArtifactStatusSuccess,
		CaseID: 9,
		CaseSnapshot: map[string]any{
			"name": "Pelican on a Bicycle",
			"kind": iqbenchmark.CaseKindText,
		},
	}
	dto := toPublicArtifact(artifact, PublicArtifactOptions{})
	require.Equal(t, "Pelican on a Bicycle", dto.CaseName)
	require.Equal(t, iqbenchmark.CaseKindText, dto.CaseKind)
	require.EqualValues(t, 9, dto.CaseID)
}

// TestToPublicArtifactHandlesNil 断言 nil 不会 panic。
func TestToPublicArtifactHandlesNil(t *testing.T) {
	require.NotPanics(t, func() {
		dto := toPublicArtifact(nil, PublicArtifactOptions{ExposeCost: true})
		require.EqualValues(t, 0, dto.ID)
	})
}

// TestTruncatedFlagPropagates 断言截断标记会传到公开侧。
//
// 前端据此显示「内容已截断」，避免用户误以为模型只输出了一半。
func TestTruncatedFlagPropagates(t *testing.T) {
	artifact := &iqbenchmark.Artifact{
		ID:               1,
		Status:           iqbenchmark.ArtifactStatusSuccess,
		RenderKind:       iqbenchmark.RenderKindText,
		Content:          "partial",
		ContentTruncated: true,
		CreatedAt:        time.Now(),
	}
	dto := toPublicArtifact(artifact, PublicArtifactOptions{})
	require.True(t, dto.Truncated)
}
