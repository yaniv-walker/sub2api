package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	upstreamsvc "github.com/Wei-Shaw/sub2api/internal/service"
)

// fakeGroupStore 是只实现 ListActive 的最小替身。
type fakeGroupStore struct {
	groups []upstreamsvc.Group
	err    error
}

func (f *fakeGroupStore) ListActive(context.Context) ([]upstreamsvc.Group, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.groups, nil
}

// fakeModelLister 记录调用次数，用于断言缓存刷新与「不重复查询」。
type fakeModelLister struct {
	models    map[int64][]string
	invalidAt []int64
	calls     int
}

func (f *fakeModelLister) GetAvailableModels(_ context.Context, groupID *int64, _ string) []string {
	f.calls++
	if groupID == nil {
		return nil
	}
	return f.models[*groupID]
}

func (f *fakeModelLister) InvalidateAvailableModelsCache(groupID *int64, _ string) {
	if groupID != nil {
		f.invalidAt = append(f.invalidAt, *groupID)
	}
}

func newTargetsFixture(models map[int64][]string, groups []upstreamsvc.Group) (*TargetsService, *fakeModelLister) {
	lister := &fakeModelLister{models: models}
	return NewTargetsService(&fakeGroupStore{groups: groups}, lister), lister
}

// TestListTargetsUsesRuntimeModels 覆盖主路径：分组模型直接来自运行态映射。
func TestListTargetsUsesRuntimeModels(t *testing.T) {
	groups := []upstreamsvc.Group{{
		ID: 7, Name: "主力", Platform: upstreamsvc.PlatformGemini,
		AllowImageGeneration: true, AccountCount: 3, ActiveAccountCount: 2,
	}}
	svc, _ := newTargetsFixture(map[int64][]string{7: {"gemini-2.5-pro"}}, groups)

	got, err := svc.ListTargets(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, got.Groups, 1)

	g := got.Groups[0]
	assert.Equal(t, int64(7), g.ID)
	assert.Equal(t, TargetModelSourceRuntime, g.ModelSource)
	assert.Nil(t, g.UnavailableReason)
	require.Len(t, g.Models, 1)
	assert.Equal(t, "gemini-2.5-pro", g.Models[0].ID)
	// 分组允许生图 ⇒ 模型同时具备 text + image。
	assert.Equal(t, []string{TargetCapabilityText, TargetCapabilityImage}, g.Models[0].Capabilities)
	assert.Equal(t, []string{TargetCapabilityText, TargetCapabilityImage}, g.Capabilities)
	assert.False(t, got.Truncated)
}

// TestListTargetsFallsBackToPlatformDefaults 是第 2 层口径的核心回归：
// 有账号但没配 model_mapping（GetAvailableModels 返回 nil）时，必须回退到平台默认集，
// 否则这类分组会显示空下拉 —— 而网关侧这些模型是真能调的。
func TestListTargetsFallsBackToPlatformDefaults(t *testing.T) {
	groups := []upstreamsvc.Group{{
		ID: 9, Name: "无映射", Platform: upstreamsvc.PlatformGemini,
		AccountCount: 2, ActiveAccountCount: 2,
	}}
	svc, _ := newTargetsFixture(map[int64][]string{9: nil}, groups)

	got, err := svc.ListTargets(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, got.Groups, 1)
	require.NotEmpty(t, got.Groups[0].Models, "must fall back to platform default models")
	assert.Equal(t, TargetModelSourcePlatformDefault, got.Groups[0].ModelSource)
	assert.Nil(t, got.Groups[0].UnavailableReason)
}

// TestListTargetsOpenAINilFallsBackButEmptyDoesNot 区分 nil 与 []string{}。
// OpenAI 分支「非 nil 即用」：空切片表示「过滤后确实为空」，不应回退。
func TestListTargetsOpenAINilFallsBackButEmptyDoesNot(t *testing.T) {
	groups := []upstreamsvc.Group{{
		ID: 1, Name: "oa-empty", Platform: upstreamsvc.PlatformOpenAI,
		AccountCount: 1, ActiveAccountCount: 1,
	}}
	svc, _ := newTargetsFixture(map[int64][]string{1: {}}, groups)

	got, err := svc.ListTargets(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, got.Groups, 1)
	assert.Empty(t, got.Groups[0].Models, "empty (non-nil) slice must not fall back for OpenAI")
	assert.Equal(t, TargetModelSourceNone, got.Groups[0].ModelSource)
	require.NotNil(t, got.Groups[0].UnavailableReason)
	assert.Equal(t, TargetReasonNoModelMapping, *got.Groups[0].UnavailableReason)
}

// TestListTargetsWarnsNoAccountDespiteFallback 覆盖「有平台默认集回退、但没有可调度账号」。
//
// 这是一种**必须保留模型**的场景：网关 /v1/models 同样会列出平台默认集，
// 所以下拉也应列出（否则管理员无法为「账号即将上线」的分组预先配任务），
// 但要用 unavailable_reason 提示「当前没有可调度账号」。
func TestListTargetsWarnsNoAccountDespiteFallback(t *testing.T) {
	groups := []upstreamsvc.Group{{
		ID: 2, Name: "空", Platform: upstreamsvc.PlatformOpenAI,
		AccountCount: 0, ActiveAccountCount: 0,
	}}
	svc, _ := newTargetsFixture(map[int64][]string{2: nil}, groups)

	got, err := svc.ListTargets(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, got.Groups, 1)
	// OpenAI 的 nil 会回退到平台默认集，因此模型非空。
	require.NotEmpty(t, got.Groups[0].Models, "platform default fallback still lists models")
	require.NotNil(t, got.Groups[0].UnavailableReason,
		"no schedulable account must still be surfaced as a warning")
	assert.Equal(t, TargetReasonNoSchedulableAccount, *got.Groups[0].UnavailableReason)
}

// TestListTargetsReasonsAllowlistFilteredEmpty 覆盖「白名单把非空集合过滤成空」。
func TestListTargetsReasonsAllowlistFilteredEmpty(t *testing.T) {
	groups := []upstreamsvc.Group{{
		ID: 3, Name: "白名单", Platform: upstreamsvc.PlatformGemini,
		AccountCount: 1, ActiveAccountCount: 1,
		ModelAllowlist: upstreamsvc.GroupModelAllowlist{
			Enabled: true,
			Models:  []string{"only-this-one"},
		},
	}}
	svc, _ := newTargetsFixture(map[int64][]string{3: {"other-a", "other-b"}}, groups)

	got, err := svc.ListTargets(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, got.Groups, 1)
	assert.Empty(t, got.Groups[0].Models)
	require.NotNil(t, got.Groups[0].UnavailableReason)
	assert.Equal(t, TargetReasonAllowlistFilteredEmpty, *got.Groups[0].UnavailableReason)
}

// TestListTargetsRefreshInvalidatesCache 断言 ?refresh=1 会精确失效上游缓存。
func TestListTargetsRefreshInvalidatesCache(t *testing.T) {
	groups := []upstreamsvc.Group{{
		ID: 5, Name: "g", Platform: upstreamsvc.PlatformGemini,
		AccountCount: 1, ActiveAccountCount: 1,
	}}
	svc, lister := newTargetsFixture(map[int64][]string{5: {"m1"}}, groups)

	_, err := svc.ListTargets(context.Background(), false)
	require.NoError(t, err)
	assert.Empty(t, lister.invalidAt, "plain list must not invalidate cache")

	_, err = svc.ListTargets(context.Background(), true)
	require.NoError(t, err)
	assert.Equal(t, []int64{5}, lister.invalidAt, "refresh must invalidate that exact group")
}

// TestListTargetsSkipsInvalidIDs 断言非法 ID 被跳过而不是产生脏行。
func TestListTargetsSkipsInvalidIDs(t *testing.T) {
	groups := []upstreamsvc.Group{
		{ID: 0, Name: "zero", Platform: upstreamsvc.PlatformGemini},
		{ID: 4, Name: "ok", Platform: upstreamsvc.PlatformGemini, AccountCount: 1, ActiveAccountCount: 1},
	}
	svc, _ := newTargetsFixture(map[int64][]string{4: {"m"}}, groups)

	got, err := svc.ListTargets(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, got.Groups, 1)
	assert.Equal(t, int64(4), got.Groups[0].ID)
}

// TestListTargetsNilDependenciesDoNotPanic 断言依赖缺失时返回空集合而非 panic。
// 管理员后台在装配异常时应看到「不可用」，不能白屏。
func TestListTargetsNilDependenciesDoNotPanic(t *testing.T) {
	svc := NewTargetsService(nil, nil)
	got, err := svc.ListTargets(context.Background(), false)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Empty(t, got.Groups)
}

// TestActiveGroupRejectsInactive 断言只有 ListActive 内的分组才可作为目标。
func TestActiveGroupRejectsInactive(t *testing.T) {
	svc := NewTargetsService(&fakeGroupStore{groups: []upstreamsvc.Group{
		{ID: 10, Name: "active", Platform: upstreamsvc.PlatformGemini},
	}}, nil)

	got, err := svc.ActiveGroup(context.Background(), 10)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(10), got.ID)

	_, err = svc.ActiveGroup(context.Background(), 11)
	assert.True(t, errors.Is(err, iqbenchmark.ErrTargetGroupUnavailable),
		"a group absent from ListActive must be rejected, got %v", err)
}

// TestValidateTarget 是二次校验的表格测试。
func TestValidateTarget(t *testing.T) {
	imgGroup := upstreamsvc.Group{
		ID: 20, Name: "img", Platform: upstreamsvc.PlatformGemini,
		AllowImageGeneration: true, AccountCount: 1, ActiveAccountCount: 1,
	}
	txtGroup := upstreamsvc.Group{
		ID: 21, Name: "txt", Platform: upstreamsvc.PlatformGemini,
		AllowImageGeneration: false, AccountCount: 1, ActiveAccountCount: 1,
	}
	svc := NewTargetsService(&fakeGroupStore{}, &fakeModelLister{
		models: map[int64][]string{
			20: {"gemini-2.5-pro"},
			21: {"gemini-2.5-pro"},
		},
	})

	tests := []struct {
		name    string
		group   *upstreamsvc.Group
		model   string
		kinds   []string
		wantErr error
	}{
		{
			name: "ok-text", group: &txtGroup, model: "gemini-2.5-pro",
			kinds: []string{iqbenchmark.CaseKindText}, wantErr: nil,
		},
		{
			name: "ok-image-on-image-group", group: &imgGroup, model: "gemini-2.5-pro",
			kinds: []string{iqbenchmark.CaseKindImage}, wantErr: nil,
		},
		{
			name: "nil-group", group: nil, model: "m",
			kinds: nil, wantErr: iqbenchmark.ErrTargetGroupUnavailable,
		},
		{
			name: "empty-model", group: &txtGroup, model: "   ",
			kinds: []string{iqbenchmark.CaseKindText}, wantErr: iqbenchmark.ErrTargetModelUnavailable,
		},
		{
			name: "model-not-in-group", group: &txtGroup, model: "not-there",
			kinds: []string{iqbenchmark.CaseKindText}, wantErr: iqbenchmark.ErrTargetModelUnavailable,
		},
		{
			name: "image-case-but-group-forbids-image", group: &txtGroup, model: "gemini-2.5-pro",
			kinds: []string{iqbenchmark.CaseKindImage}, wantErr: iqbenchmark.ErrTargetCapabilityMismatch,
		},
		{
			name: "mixed-cases-need-image", group: &txtGroup, model: "gemini-2.5-pro",
			kinds:   []string{iqbenchmark.CaseKindText, iqbenchmark.CaseKindImage},
			wantErr: iqbenchmark.ErrTargetCapabilityMismatch,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.ValidateTarget(context.Background(), tc.group, tc.model, tc.kinds)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			assert.True(t, errors.Is(err, tc.wantErr), "want %v, got %v", tc.wantErr, err)
		})
	}
}
