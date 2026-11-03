package iqbenchmark_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// 本文件穷举「可展示分组白名单」的语义。
//
// 白名单是公开侧唯一的安全边界：它一旦判错，内部拓扑就直接摊在
// 公开页面上。所以这里对每种输入都要求一个确定的答案，
// 尤其是「没配置」这种最常见、也最容易默认放行的情形。

func TestGroupExposedEmptyWhitelistDeniesEverything(t *testing.T) {
	// 用户裁决：空白名单 = 什么都不显示（fail-closed）。
	// 这条是整个白名单设计的基线，改动它等于改变安全默认值。
	s := iqbenchmark.DefaultSettings()
	require.Empty(t, s.ExposeGroups, "默认配置必须没有白名单")
	require.False(t, s.GroupExposed(1))
	require.False(t, s.GroupExposed(999))
}

func TestGroupExposedOnlyListedGroups(t *testing.T) {
	s := iqbenchmark.DefaultSettings()
	s.ExposeGroups = []iqbenchmark.ExposeGroup{
		{GroupID: 3, ExposeName: "组三"},
		{GroupID: 7, ExposeName: "组七"},
	}
	require.True(t, s.GroupExposed(3))
	require.True(t, s.GroupExposed(7))
	require.False(t, s.GroupExposed(5), "白名单外的分组必须被拒")
	require.False(t, s.GroupExposed(0), "分组 0 不是合法分组")
	require.False(t, s.GroupExposed(-1))
}

func TestExposeGroupIDs(t *testing.T) {
	s := iqbenchmark.DefaultSettings()
	require.Nil(t, s.ExposeGroupIDs(), "空白名单返回 nil，让调用方能区分「不限」与「全禁」")

	s.ExposeGroups = []iqbenchmark.ExposeGroup{
		{GroupID: 3}, {GroupID: 0}, {GroupID: 7}, {GroupID: -2},
	}
	require.Equal(t, []int64{3, 7}, s.ExposeGroupIDs(), "非正 ID 必须被剔除")
}

func TestExposeGroupIDsReturnsCopy(t *testing.T) {
	// 返回副本：调用方就地修改不能污染配置缓存，否则一次越权读取
	// 就能把白名单在进程内永久改掉。
	s := iqbenchmark.DefaultSettings()
	s.ExposeGroups = []iqbenchmark.ExposeGroup{{GroupID: 3}, {GroupID: 7}}

	ids := s.ExposeGroupIDs()
	ids[0] = 999
	require.Equal(t, []int64{3, 7}, s.ExposeGroupIDs(), "修改返回值不得影响配置本身")
}

func TestExposeNameOf(t *testing.T) {
	s := iqbenchmark.DefaultSettings()
	s.ExposeGroups = []iqbenchmark.ExposeGroup{
		{GroupID: 3, ExposeName: "  组三  "},
		{GroupID: 7, ExposeName: ""},
	}
	require.Equal(t, "组三", s.ExposeNameOf(3), "展示名必须去首尾空白")
	require.Empty(t, s.ExposeNameOf(7), "空展示名保持为空，绝不回退成上游分组名")
	require.Empty(t, s.ExposeNameOf(5))
}

func TestNormalizeDropsInvalidExposeGroups(t *testing.T) {
	s := iqbenchmark.DefaultSettings()
	s.ExposeGroups = []iqbenchmark.ExposeGroup{
		{GroupID: 0, ExposeName: "空行"},
		{GroupID: -1, ExposeName: "负数"},
		{GroupID: 3, ExposeName: "  正常  "},
		{GroupID: 3, ExposeName: "重复"},
		{GroupID: 7, ExposeName: ""},
	}
	got := s.Normalize()

	require.Len(t, got.ExposeGroups, 2, "非法 ID 与重复项都必须被剔除")
	require.Equal(t, int64(3), got.ExposeGroups[0].GroupID)
	require.Equal(t, "正常", got.ExposeGroups[0].ExposeName, "展示名去空白")
	require.Equal(t, int64(7), got.ExposeGroups[1].GroupID)
}

func TestNormalizeAllInvalidBecomesNil(t *testing.T) {
	// 全是脏数据 → 归一化成 nil，而不是留一个空切片。
	// 这样落库形态与「从未配置」一致，运维一眼能看出「没配」而非「配错了」。
	s := iqbenchmark.DefaultSettings()
	s.ExposeGroups = []iqbenchmark.ExposeGroup{{GroupID: 0}, {GroupID: -5}}
	require.Nil(t, s.Normalize().ExposeGroups)
}

func TestExposeGroupsSurviveJSONRoundTrip(t *testing.T) {
	// 配置是存 JSON 的，字段名写错就等于白名单静默失效 ——
	// 而失效的方向是不显示（安全），但管理员会以为配好了却看不到东西。
	s := iqbenchmark.DefaultSettings()
	s.ExposeGroups = []iqbenchmark.ExposeGroup{
		{GroupID: 3, ExposeName: "组三", SortOrder: 2},
	}
	payload, err := json.Marshal(s)
	require.NoError(t, err)
	require.Contains(t, string(payload), `"expose_groups"`)

	var back iqbenchmark.Settings
	require.NoError(t, json.Unmarshal(payload, &back))
	require.Len(t, back.ExposeGroups, 1)
	require.Equal(t, int64(3), back.ExposeGroups[0].GroupID)
	require.Equal(t, "组三", back.ExposeGroups[0].ExposeName)
	require.Equal(t, 2, back.ExposeGroups[0].SortOrder)
}

func TestExposeGroupsOmittedFromJSONKeepsWhitelistEmpty(t *testing.T) {
	// 老配置文件里没有 expose_groups 字段：解析后必须是空 → 全禁，
	// 绝不能因为「缺字段」被当成「不限」。
	var s iqbenchmark.Settings
	require.NoError(t, json.Unmarshal([]byte(`{"enabled":true}`), &s))
	require.Empty(t, s.ExposeGroups)
	require.False(t, s.GroupExposed(1))
}
