//go:build unit

package repository

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/modeldegradation"
)

// buildRunWhere 决定两件要紧的事：
//   1. 公共墙的隐藏过滤是否真的进了 SQL（查完再筛会打乱分页）
//   2. 参数占位符编号是否与 args 顺序一致（错位会静默套用错误的过滤条件）
// 二错其一都不是「显示不好看」，是「该隐藏的没隐藏」。

func TestBuildRunWhere_NoFilter(t *testing.T) {
	where, args := buildRunWhere(modeldegradation.RunFilter{})
	if where != "" {
		t.Fatalf("无条件时 where 应为空串，实际 %q", where)
	}
	if len(args) != 0 {
		t.Fatalf("无条件时不应有参数，实际 %v", args)
	}
}

func TestBuildRunWhere_UserID(t *testing.T) {
	where, args := buildRunWhere(modeldegradation.RunFilter{UserID: 7})
	if !strings.Contains(where, "user_id = $1") {
		t.Fatalf("where = %q", where)
	}
	if len(args) != 1 || args[0] != int64(7) {
		t.Fatalf("args = %v", args)
	}
}

func TestBuildRunWhere_Verdict(t *testing.T) {
	where, args := buildRunWhere(modeldegradation.RunFilter{Verdict: "degraded"})
	if !strings.Contains(where, "verdict = $1") {
		t.Fatalf("where = %q", where)
	}
	if len(args) != 1 || args[0] != "degraded" {
		t.Fatalf("args = %v", args)
	}
}

func TestBuildRunWhere_HiddenGroups(t *testing.T) {
	f := modeldegradation.RunFilter{ExcludeGroupIDs: []int64{3, 5}}
	where, args := buildRunWhere(f)

	if !strings.Contains(where, "group_id NOT IN ($1,$2)") {
		t.Fatalf("where = %q", where)
	}
	if len(args) != 2 {
		t.Fatalf("args = %v", args)
	}
	// 顺序必须与占位符一致。
	if args[0] != int64(3) || args[1] != int64(5) {
		t.Fatalf("args 顺序错位: %v", args)
	}
}

func TestBuildRunWhere_HiddenHosts(t *testing.T) {
	f := modeldegradation.RunFilter{ExcludeHosts: []string{"a.example.com"}}
	where, args := buildRunWhere(f)

	if !strings.Contains(where, "base_url_display NOT LIKE $1") {
		t.Fatalf("where = %q", where)
	}
	if len(args) != 1 {
		t.Fatalf("args = %v", args)
	}
	// LIKE 模式必须是 https://host/% 形态，与落库的脱敏形态对上。
	want := "https://a.example.com/%"
	if args[0] != want {
		t.Fatalf("LIKE 模式 = %v, want %q", args[0], want)
	}
}

func TestBuildRunWhere_MixedFiltersPlaceholderOrder(t *testing.T) {
	// 这是最容易出错的一处：多个条件时占位符编号必须连续且与 args 对齐。
	f := modeldegradation.RunFilter{
		UserID:          9,
		Verdict:         "normal",
		ExcludeGroupIDs: []int64{2},
		ExcludeHosts:    []string{"x.example.com", "y.example.com"},
	}
	where, args := buildRunWhere(f)

	// 期望 $1=user_id, $2=verdict, $3=group, $4=host x, $5=host y
	if !strings.Contains(where, "user_id = $1") {
		t.Fatalf("user_id 占位符错: %q", where)
	}
	if !strings.Contains(where, "verdict = $2") {
		t.Fatalf("verdict 占位符错: %q", where)
	}
	if !strings.Contains(where, "group_id NOT IN ($3)") {
		t.Fatalf("group 占位符错: %q", where)
	}
	if !strings.Contains(where, "NOT LIKE $4") {
		t.Fatalf("host1 占位符错: %q", where)
	}
	if !strings.Contains(where, "NOT LIKE $5") {
		t.Fatalf("host2 占位符错: %q", where)
	}

	if len(args) != 5 {
		t.Fatalf("args 数量 = %d, want 5 (%v)", len(args), args)
	}
	// 逐位校验，确保编号与值真的对齐。
	if args[0] != int64(9) {
		t.Fatalf("args[0] = %v, want 9", args[0])
	}
	if args[1] != "normal" {
		t.Fatalf("args[1] = %v, want normal", args[1])
	}
	if args[2] != int64(2) {
		t.Fatalf("args[2] = %v, want 2", args[2])
	}
	if args[3] != "https://x.example.com/%" {
		t.Fatalf("args[3] = %v", args[3])
	}
	if args[4] != "https://y.example.com/%" {
		t.Fatalf("args[4] = %v", args[4])
	}
}

func TestBuildRunWhere_IncludeHiddenSkipsExclusion(t *testing.T) {
	// IncludeHidden=true 时（管理侧）不应有任何排除条件。
	f := modeldegradation.RunFilter{
		IncludeHidden:   true,
		ExcludeGroupIDs: []int64{3},
		ExcludeHosts:    []string{"a.example.com"},
	}
	where, args := buildRunWhere(f)

	if where != "" {
		t.Fatalf("IncludeHidden 时不应有 where，实际 %q", where)
	}
	if len(args) != 0 {
		t.Fatalf("IncludeHidden 时不应有参数，实际 %v", args)
	}
}

func TestBuildRunWhere_TaskTypeUsesExists(t *testing.T) {
	// 题型不是 runs 的列，必须用 EXISTS 子查询，
	// 否则要么报错（列不存在），要么被误加到错误的地方。
	f := modeldegradation.RunFilter{TaskType: "render"}
	where, args := buildRunWhere(f)

	if !strings.Contains(where, "EXISTS") {
		t.Fatalf("题型过滤必须用 EXISTS，实际 %q", where)
	}
	if !strings.Contains(where, "i.task_type = $1") {
		t.Fatalf("where = %q", where)
	}
	if len(args) != 1 || args[0] != "render" {
		t.Fatalf("args = %v", args)
	}
}

func TestBuildRunWhere_ConditionsJoinedWithAND(t *testing.T) {
	f := modeldegradation.RunFilter{UserID: 1, Verdict: "normal"}
	where, _ := buildRunWhere(f)
	if !strings.Contains(where, " AND ") {
		t.Fatalf("多条件应以 AND 连接，实际 %q", where)
	}
	if !strings.HasPrefix(where, " WHERE ") {
		t.Fatalf("应带 WHERE 前缀，实际 %q", where)
	}
}
