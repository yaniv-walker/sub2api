//go:build unit

package handler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/modeldegradation"
)

// 本文件锁住对外 DTO 的契约。
//
// 最重要的那条：**凭据永远不出现在响应里**。
// 靠「review 时记得检查」是不够的，这里用序列化后的字节直接断言 ——
// 将来任何人往 DTO 里加字段，只要带了 api_key 或原始 base_url，
// 这个测试就会红。

// testRun 造一条字段尽量填满的运行记录，用于序列化断言。
func testRun() modeldegradation.Run {
	completed := time.Date(2026, 1, 1, 0, 0, 2, 0, time.UTC)
	return modeldegradation.Run{
		ID:                   42,
		BaseURLDisplay:       "https://api.example.com/***",
		ModelID:              "gpt-4o",
		Protocol:             modeldegradation.ProtocolChatCompletions,
		ReasoningEffort:      modeldegradation.EffortHigh,
		RenderTimeoutSeconds: 900,
		Status:               modeldegradation.StatusCompleted,
		Verdict:              modeldegradation.VerdictNormal,
		LogicPassed:          1,
		LogicTotal:           1,
		RenderVerdict:        modeldegradation.RenderVerdictOK,
		ErrorMessage:         "",
		ErrorKind:            "",
		DurationMS:           1200,
		CreatedAt:            time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CompletedAt:          &completed,
		Retrying:             false,
		OwnerEmail:           "a***@example.com",
		GroupID:              7,
		GroupName:            "G1",
		IsOwner:              true,
		ArtworkPreview:       "<svg/>",
	}
}

// TestRunDTO_neverSerializesCredentials 是凭据约束的运行时防线。
func TestRunDTO_neverSerializesCredentials(t *testing.T) {
	raw, err := json.Marshal(toRunDTO(testRun()))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	body := string(raw)

	for _, banned := range []string{"api_key", "apiKey", "APIKey", "sk-", "bearer", "Bearer"} {
		if strings.Contains(body, banned) {
			t.Fatalf("响应里出现了凭据相关字段 %q: %s", banned, body)
		}
	}

	// base_url 必须是脱敏形态，不能带路径。
	if strings.Contains(body, "/v1") || strings.Contains(body, "secret") {
		t.Fatalf("base_url 泄漏了路径: %s", body)
	}
	if !strings.Contains(body, "https://api.example.com/***") {
		t.Fatalf("base_url 应为脱敏形态: %s", body)
	}
}

// TestRunDTO_jsonFieldNames 锁住字段名，避免前端静默拿到 undefined。
func TestRunDTO_jsonFieldNames(t *testing.T) {
	raw, err := json.Marshal(toRunDTO(testRun()))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	// 这些键前端逐个在读，少一个就是一处 undefined。
	want := []string{
		"id", "base_url", "model_id", "protocol", "reasoning_effort",
		"render_timeout_seconds", "status", "verdict", "logic_passed",
		"logic_total", "render_verdict", "error_message", "error_kind",
		"duration_ms", "created_at", "completed_at", "retrying",
		"owner_email", "group_id", "group_name", "is_owner", "artwork_preview",
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Fatalf("缺少字段 %q，前端会读到 undefined", k)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("字段数 %d 与预期 %d 不一致（新增字段请同步前端类型与测试）", len(got), len(want))
	}
}

// TestRunDetailDTO_alwaysHasItemsArray 保证 items 是数组而不是 null。
//
// 前端模板直接 v-for items，null 会让整块明细渲染不出来。
func TestRunDetailDTO_alwaysHasItemsArray(t *testing.T) {
	detail := &modeldegradation.RunDetail{Run: testRun(), Items: nil}
	raw, err := json.Marshal(toRunDetailDTO(detail))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !strings.Contains(string(raw), `"items":[]`) {
		t.Fatalf("空明细应序列化为 []，实际: %s", raw)
	}
}

// TestTaskBankDTO_alwaysHasArrays 保证题库两侧都是数组。
func TestTaskBankDTO_alwaysHasArrays(t *testing.T) {
	raw, err := json.Marshal(toTaskBankDTO(nil))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !strings.Contains(string(raw), `"logic":[]`) || !strings.Contains(string(raw), `"render":[]`) {
		t.Fatalf("空题库应序列化为两个空数组，实际: %s", raw)
	}
}

// TestItemDTO_artworkOmittedWhenEmpty 保证没有作品时不塞空字符串。
//
// 历史墙按 artwork 是否存在来决定画不画缩略图，空串会让它画出空框。
func TestItemDTO_artworkOmittedWhenEmpty(t *testing.T) {
	it := modeldegradation.Item{
		Type:     modeldegradation.TaskTypeLogic,
		Key:      "candy-shape-flavor",
		Prompt:   "p",
		Expected: "21",
		Response: "21",
		Passed:   true,
		Verdict:  modeldegradation.ItemVerdictOK,
		Detail:   "命中答案 21",
		Artwork:  "",
	}
	raw, err := json.Marshal(toItemDTO(it))
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if strings.Contains(string(raw), "artwork") {
		t.Fatalf("空作品应被 omitempty 省略，实际: %s", raw)
	}
}

// TestModelsRequest_acceptsCredentialsButHasNoResponseCounterpart 说明凭据流向是单向的。
//
// 请求体里有 api_key（用户当场填的），但没有任何响应 DTO 带它 ——
// 这个测试通过反射式断言把「单向」这件事写进代码。
func TestModelsRequest_acceptsCredentialsButHasNoResponseCounterpart(t *testing.T) {
	req := ModelsRequest{BaseURL: "https://a.example.com", APIKey: "sk-secret", Protocol: "chat_completions"}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	// 请求体确实带着凭据（要发给上游用），这是预期行为。
	if !strings.Contains(string(raw), "sk-secret") {
		t.Fatalf("请求体应携带凭据: %s", raw)
	}

	// 但任何响应 DTO 都不该带。
	for name, v := range map[string]any{
		"RunDTO":       toRunDTO(testRun()),
		"RunDetailDTO": toRunDetailDTO(&modeldegradation.RunDetail{Run: testRun()}),
		"TaskBankDTO":  toTaskBankDTO(nil),
		"StatsDTO":     StatsDTO{PassRate: 1, LastDurationMS: 100, RunCount: 1},
		"VisibilityDTO": VisibilityDTO{
			HiddenGroupIDs: []int64{},
			HiddenHosts:    []string{},
			Hosts:          []HostDTO{},
		},
	} {
		body, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s 序列化失败: %v", name, err)
		}
		if strings.Contains(string(body), "sk-secret") {
			t.Fatalf("%s 泄漏了凭据: %s", name, body)
		}
	}
}
