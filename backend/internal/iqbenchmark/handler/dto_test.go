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
//
// 关于 group_id / group_name 的一次**有意放宽**（记录在此以免被当成疏漏）：
//   - group_id 从清单中移除：公开作品展要按「分组分墙」，墙体必须可寻址。
//     仅凭管理员自填的展示名无法稳定标识一堵墙（改名即失联）。
//   - group_name 从清单中移除：但公开侧下发的**不是**上游分组名，
//     而是管理员在白名单里另填的 expose_name。上游名（往往含渠道信息，
//     如「claude-code-高倍率」）永远不出后台。上游名仍由
//     TestPublicOverviewBucketDTOExposesNoGroupIdentity 之外的
//     配置层约束保证：ExposeNameOf 为空即返回空串，绝不回退。
//
// 这不是「守卫被删了」，而是守卫被这条注释换成了更具体的规则：
// 公开侧允许出现的是「管理员明确同意公开的分组标识」，
// 而不是「数据库里那一行分组的原样投影」。
//
// 仍然禁止的（未改动）：user_id / api_key_id / account_id 等身份字段。
// 「这条是不是我测的」改由服务端算好的布尔值 is_mine 表达，
// 客户端因此拿不到任何可用于反查归属的标识。
var forbiddenPublicJSONFields = []string{
	// 内部账号与运行期身份
	"internal_user_id", "internal_api_key_id", "account_id", "user_id", "api_key_id",
	// 任务内部结构
	"task_id", "run_id", "case_ids", "case_snapshot",
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
		// 判定四态与错误归一化：status 只说明「调用成不成」，
		// verdict 才是「答得对不对」。缺了它模型变笨时前端无从表达。
		"verdict", "render_verdict", "error_kind",
		// is_mine 是服务端算好的布尔值，不含任何归属标识。
		"is_mine",
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
		[]string{"id", "name", "kind", "expect_kind", "prompt", "allow_user_test"},
		names,
		"PublicCaseDTO 字段集合固定；新增字段需同步更新 design.md 第 9.2 节")
}

// TestPublicOverviewBucketDTOGroupIdentityIsExposeOnly 断言趋势桶只暴露
// 「管理员同意公开的分组标识」，而不是上游分组原貌。
//
// 这条断言取代了原来的 TestPublicOverviewBucketDTOExposesNoGroupIdentity
// （原断言要求 group_id / group_name 都不出现）。改判的理由：
// 公开作品展要按分组分墙，墙体必须可寻址、可命名。改判后的约束更具体：
//   - group_id 允许出现（它是白名单内的 ID，不是拓扑全集）
//   - group_name 允许出现，但**只承载 expose_name**
//   - 任何上游分组名都不得出现在这里（由 ExposeNameOf 空值不回退保证）
func TestPublicOverviewBucketDTOGroupIdentityIsExposeOnly(t *testing.T) {
	names := jsonFieldNames(reflect.TypeOf(PublicOverviewBucketDTO{}))
	require.Contains(t, names, "group_id", "分墙需要可寻址的分组标识")
	require.Contains(t, names, "group_name", "分墙需要展示名（仅 expose_name）")
	require.Contains(t, names, "model")
	require.Contains(t, names, "verdict", "四态判定必须能到达前端")

	// expose_name 为空时绝不回退成上游分组名 —— 这是「忘了填名字」
	// 与「泄露拓扑」之间唯一的一道墙。
	s := iqbenchmark.DefaultSettings()
	s.ExposeGroups = []iqbenchmark.ExposeGroup{{GroupID: 3, ExposeName: ""}}
	require.Empty(t, s.ExposeNameOf(3), "空展示名必须保持空，不得回退到上游分组名")
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

// ---------- 四态判定 / 错误归一化 / is_mine ----------

// TestPublicVerdictNeverDefaultsToOK 断言未判定的产出不会被误报成「答对了」。
//
// 这是整个判定链路上最容易出事的默认值：迁移 253 之前的行没有 verdict，
// 若兜底成 ok，「通过率」会在升级瞬间凭空上涨一截。
func TestPublicVerdictNeverDefaultsToOK(t *testing.T) {
	require.Equal(t, iqbenchmark.VerdictUnknown, publicVerdict(""))
	require.Equal(t, iqbenchmark.VerdictUnknown, publicVerdict("bogus"))
	require.Equal(t, iqbenchmark.VerdictOK, publicVerdict(iqbenchmark.VerdictOK))
	require.Equal(t, iqbenchmark.VerdictDegraded, publicVerdict(iqbenchmark.VerdictDegraded))
	require.Equal(t, iqbenchmark.VerdictGenerated, publicVerdict(iqbenchmark.VerdictGenerated))
}

// TestPublicRenderVerdictNeverDefaultsToOK 同理：渲染判定未知时不能说「渲染正常」。
func TestPublicRenderVerdictNeverDefaultsToOK(t *testing.T) {
	require.Equal(t, iqbenchmark.RenderVerdictUnknown, publicRenderVerdict(""))
	require.Equal(t, iqbenchmark.RenderVerdictUnknown, publicRenderVerdict("broken"))
	require.Equal(t, iqbenchmark.RenderVerdictOK, publicRenderVerdict(iqbenchmark.RenderVerdictOK))
}

// TestPublicErrorKindDerivesFromLegacyCode 覆盖旧行兼容路径。
//
// 迁移 253 之前的行只有 error_code；这里用映射表反推一个展示类别，
// 保证老数据也能显示「超时 / 限流」而不是空白。
func TestPublicErrorKindDerivesFromLegacyCode(t *testing.T) {
	legacy := &iqbenchmark.Artifact{ErrorCode: iqbenchmark.ErrCodeTimeout}
	require.Equal(t, iqbenchmark.ErrorKindTimeout, publicErrorKind(legacy),
		"没有 error_kind 时必须从 error_code 反推")

	modern := &iqbenchmark.Artifact{ErrorKind: iqbenchmark.ErrorKindRateLimited}
	require.Equal(t, iqbenchmark.ErrorKindRateLimited, publicErrorKind(modern),
		"有 error_kind 时优先用它，不再看 error_code")

	// 未知取值必须塌缩成 generic：原样下发等于把内部错误串直通公网。
	odd := &iqbenchmark.Artifact{ErrorKind: "vendor_specific_thing"}
	require.Equal(t, iqbenchmark.ErrorKindGeneric, publicErrorKind(odd))

	// 完全没错误信息时不产生 error_kind 键。
	require.Empty(t, publicErrorKind(&iqbenchmark.Artifact{}))
}

// TestPublicArtifactIsMineIsServerComputed 断言 is_mine 只由服务端比较得出。
//
// 三个要点：
//  1. 定时检测的产出（TriggeredByUserID=0）对任何人都不是「我的」；
//  2. 别人的自测不是「我的」；
//  3. 未登录（ViewerUserID=0）时不会因为 0==0 把所有定时产出判成我的。
func TestPublicArtifactIsMineIsServerComputed(t *testing.T) {
	mine := &iqbenchmark.Artifact{ID: 1, Status: iqbenchmark.ArtifactStatusSuccess, TriggeredByUserID: 42}
	other := &iqbenchmark.Artifact{ID: 2, Status: iqbenchmark.ArtifactStatusSuccess, TriggeredByUserID: 99}
	scheduled := &iqbenchmark.Artifact{ID: 3, Status: iqbenchmark.ArtifactStatusSuccess, TriggeredByUserID: 0}

	require.True(t, toPublicArtifact(mine, PublicArtifactOptions{ViewerUserID: 42}).IsMine)
	require.False(t, toPublicArtifact(other, PublicArtifactOptions{ViewerUserID: 42}).IsMine)
	require.False(t, toPublicArtifact(scheduled, PublicArtifactOptions{ViewerUserID: 42}).IsMine,
		"定时检测不属于任何用户")
	require.False(t, toPublicArtifact(scheduled, PublicArtifactOptions{ViewerUserID: 0}).IsMine,
		"未登录访客不得因 0==0 而认领所有定时产出")
}

// TestPublicArtifactCarriesVerdict 断言四态真的透传到 DTO。
func TestPublicArtifactCarriesVerdict(t *testing.T) {
	a := &iqbenchmark.Artifact{
		ID:            1,
		Status:        iqbenchmark.ArtifactStatusSuccess,
		RenderKind:    iqbenchmark.RenderKindText,
		Content:       "x",
		Verdict:       iqbenchmark.VerdictDegraded,
		RenderVerdict: iqbenchmark.RenderVerdictOK,
		ErrorKind:     iqbenchmark.ErrorKindTimeout,
	}
	dto := toPublicArtifact(a, PublicArtifactOptions{})
	require.Equal(t, iqbenchmark.VerdictDegraded, dto.Verdict)
	require.Equal(t, iqbenchmark.RenderVerdictOK, dto.RenderVerdict)
	require.Equal(t, iqbenchmark.ErrorKindTimeout, dto.ErrorKind)
}
