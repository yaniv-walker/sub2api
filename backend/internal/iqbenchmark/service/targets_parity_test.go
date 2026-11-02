package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	upstreamsvc "github.com/Wei-Shaw/sub2api/internal/service"
)

// 本文件是「口径漂移」的防线。
//
// platformDefaultModelIDs / modelListingSource 复刻自上游私有函数
// internal/handler/gateway_handler.go（见 targets.go 的注释）。
// 上游一旦改动平台默认集，这里必须在**编译期或测试期**失败，而不是让
// 管理员的下拉悄悄与 /v1/models 不一致。
//
// 对上游**已导出**的符号，这里直接调用上游函数做对照断言；
// 对无法导出的部分（switch 分发本身），用精选平台的绝对值 + 空值约束兜底。

// TestPlatformDefaultModelIDsMatchesUpstreamExports 对每个可直接调用的导出函数逐一对照。
func TestPlatformDefaultModelIDsMatchesUpstreamExports(t *testing.T) {
	tests := []struct {
		platform string
		upstream func() []string
	}{
		{upstreamsvc.PlatformOpenAI, openai.DefaultModelIDs},
		{upstreamsvc.PlatformAnthropic, claude.DefaultModelIDs},
		{upstreamsvc.PlatformGrok, xai.DefaultModelIDs},
		{upstreamsvc.PlatformOpenCodeGo, upstreamsvc.DefaultOpenCodeGoModelIDs},
	}

	for _, tc := range tests {
		t.Run(tc.platform, func(t *testing.T) {
			want := tc.upstream()
			got := platformDefaultModelIDs(tc.platform)
			require.NotEmpty(t, want, "upstream default set must not be empty for %s", tc.platform)
			assert.Equal(t, want, got,
				"platformDefaultModelIDs(%q) drifted from upstream", tc.platform)
		})
	}
}

// TestPlatformDefaultModelIDsGeminiAndAntigravity 对照需要提取 ID 的导出集合。
func TestPlatformDefaultModelIDsGeminiAndAntigravity(t *testing.T) {
	wantGemini := make([]string, 0, len(geminicli.DefaultModels))
	for _, m := range geminicli.DefaultModels {
		wantGemini = append(wantGemini, m.ID)
	}
	require.NotEmpty(t, wantGemini)
	assert.Equal(t, wantGemini, platformDefaultModelIDs(upstreamsvc.PlatformGemini))

	models := antigravity.DefaultModels()
	wantAntigravity := make([]string, 0, len(models))
	for _, m := range models {
		wantAntigravity = append(wantAntigravity, m.ID)
	}
	require.NotEmpty(t, wantAntigravity)
	assert.Equal(t, wantAntigravity, platformDefaultModelIDs(upstreamsvc.PlatformAntigravity))
}

// TestPlatformDefaultModelIDsTypeSafe 对照上游字面量分支。
func TestPlatformDefaultModelIDsTypeSafe(t *testing.T) {
	assert.Equal(t, []string{"jev-latest"}, platformDefaultModelIDs(upstreamsvc.PlatformTypeSafe))
}

// TestPlatformDefaultModelIDsDefaultBranchIsClaude 断言未知平台的兜底分支是 Claude 集合，
// 与上游 default 分支一致（上游 default 返回 claude.DefaultModels 的 ID 列表）。
func TestPlatformDefaultModelIDsDefaultBranchIsClaude(t *testing.T) {
	want := make([]string, 0, len(claude.DefaultModels))
	for _, m := range claude.DefaultModels {
		want = append(want, m.ID)
	}
	require.NotEmpty(t, want)
	assert.Equal(t, want, platformDefaultModelIDs("some-unknown-platform"))
}

// TestPlatformDefaultModelIDsCompositeExcludesTypeSafe 断言 composite 分支跳过 TypeSafe。
//
// 这是上游明确注释的行为：jev-latest 只能经 /v1/systemone 使用，
// 静态回退永不对 LLM 客户端广告它。
func TestPlatformDefaultModelIDsCompositeExcludesTypeSafe(t *testing.T) {
	got := platformDefaultModelIDs(upstreamsvc.PlatformComposite)
	require.NotEmpty(t, got, "composite default set must not be empty")

	assert.NotContains(t, got, "jev-latest",
		"composite fallback must skip TypeSafe (upstream behaviour)")

	// 去重：composite 是多平台并集，绝不能有重复项。
	seen := make(map[string]struct{}, len(got))
	for _, id := range got {
		_, dup := seen[id]
		assert.False(t, dup, "duplicate model id %q in composite default set", id)
		seen[id] = struct{}{}
	}

	// 应当包含各具体平台的默认集（至少含 Claude 与 OpenAI 家族的成员）。
	expected := map[string]bool{
		upstreamsvc.PlatformAnthropic: false,
		upstreamsvc.PlatformOpenAI:    false,
	}
	for _, id := range got {
		for platform := range expected {
			if expected[platform] {
				continue
			}
			for _, candidate := range platformDefaultModelIDs(platform) {
				if candidate == id {
					expected[platform] = true
					break
				}
			}
		}
	}
	for platform, found := range expected {
		assert.True(t, found, "composite set should include models from %s", platform)
	}

	// 与 domain 的具体平台清单方向一致：composite 的组成必须来自已登记平台。
	assert.NotEmpty(t, domain.CompositePrecedencePlatformIDs())
}

// TestModelListingSourceParity 逐条复刻上游的四条分支。
//
// 上游原文（gateway_handler.go:1457）：
//
//	if platform == OpenAI && availableModels != nil { return availableModels }
//	if len(availableModels) == 0 { return fallbackModels }
//	if platform == Anthropic { return mergeModelIDs(availableModels, fallbackModels) }
//	return availableModels
func TestModelListingSourceParity(t *testing.T) {
	fallback := []string{"f1", "f2"}

	// 分支 1：OpenAI 且非 nil（哪怕是空切片）→ 原样返回，不回退。
	assert.Equal(t, []string{}, modelListingSource(upstreamsvc.PlatformOpenAI, []string{}, fallback),
		"OpenAI with non-nil empty slice must NOT fall back")
	assert.Equal(t, []string{"a"}, modelListingSource(upstreamsvc.PlatformOpenAI, []string{"a"}, fallback))

	// 分支 1 反例：OpenAI 且 nil → 落到分支 2 回退。
	assert.Equal(t, fallback, modelListingSource(upstreamsvc.PlatformOpenAI, nil, fallback))

	// 分支 2：非 OpenAI 且空 → 回退。
	assert.Equal(t, fallback, modelListingSource(upstreamsvc.PlatformGemini, nil, fallback))
	assert.Equal(t, fallback, modelListingSource(upstreamsvc.PlatformGemini, []string{}, fallback))

	// 分支 3：Anthropic → 合并可用集与默认集（去重保序）。
	got := modelListingSource(upstreamsvc.PlatformAnthropic, []string{"a", "b"}, []string{"b", "c"})
	assert.Equal(t, []string{"a", "b", "c"}, got,
		"Anthropic must merge available + fallback, deduped and order-preserving")

	// 分支 4：其余平台 → 原样用可用集。
	assert.Equal(t, []string{"x"}, modelListingSource(upstreamsvc.PlatformGemini, []string{"x"}, fallback))
}

// TestMergeModelIDsParity 复刻 mergeModelIDs 的 TrimSpace + 丢空 + 去重保序语义。
func TestMergeModelIDsParity(t *testing.T) {
	got := mergeModelIDs(
		[]string{" a ", "", "b", "a"},
		[]string{"b", "  ", "c "},
	)
	assert.Equal(t, []string{"a", "b", "c"}, got)
}
