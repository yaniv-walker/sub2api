//go:build unit

package modeldegradation

import (
	"context"
	"errors"
	"testing"
	"time"
)

// 内存 Store 让 service 层的校验逻辑可以脱离数据库测试。
type memStore struct {
	runs       []*Run
	items      map[int64][]Item
	nextRunID  int64
	nextItemID int64
	tasks      []Task
	vis        Visibility
	finish     *RunFinishInput
}

func newMemStore() *memStore {
	return &memStore{
		items:      make(map[int64][]Item),
		nextRunID:  1,
		nextItemID: 1,
	}
}

func (m *memStore) ListTasks(_ context.Context, _ bool) ([]Task, error) {
	return m.tasks, nil
}

func (m *memStore) GetTaskByKey(_ context.Context, taskType, key string) (*Task, error) {
	for i := range m.tasks {
		if m.tasks[i].Type == taskType && m.tasks[i].Key == key {
			return &m.tasks[i], nil
		}
	}
	return nil, ErrRunNotFound
}

func (m *memStore) CreateRun(_ context.Context, in RunCreateInput) (*Run, error) {
	r := &Run{
		ID:                   m.nextRunID,
		UserID:               in.UserID,
		OwnerEmail:           in.OwnerEmail,
		BaseURLDisplay:       in.BaseURLDisplay,
		ModelID:              in.ModelID,
		Protocol:             in.Protocol,
		ReasoningEffort:      in.ReasoningEffort,
		RenderTimeoutSeconds: in.RenderTimeoutSeconds,
		Status:               StatusRunning,
		Verdict:              VerdictUnknown,
		CreatedAt:            time.Now(),
	}
	m.nextRunID++
	m.runs = append(m.runs, r)
	return r, nil
}

func (m *memStore) InsertItems(_ context.Context, runID int64, items []Item) error {
	for i := range items {
		items[i].ID = m.nextItemID
		m.nextItemID++
	}
	m.items[runID] = append(m.items[runID], items...)
	return nil
}

func (m *memStore) UpdateItem(_ context.Context, itemID int64, in ItemResultInput) error {
	for runID := range m.items {
		for i := range m.items[runID] {
			if m.items[runID][i].ID == itemID {
				m.items[runID][i].Response = in.Response
				m.items[runID][i].Passed = in.Passed
				m.items[runID][i].Verdict = in.Verdict
				m.items[runID][i].Detail = in.Detail
				m.items[runID][i].Artwork = in.Artwork
				m.items[runID][i].DurationMS = in.DurationMS
			}
		}
	}
	return nil
}

func (m *memStore) FinishRun(_ context.Context, id int64, in RunFinishInput) error {
	m.finish = &in
	for _, r := range m.runs {
		if r.ID == id {
			r.Status = in.Status
			r.Verdict = in.Verdict
			r.LogicPassed = in.LogicPassed
			r.LogicTotal = in.LogicTotal
			r.RenderVerdict = in.RenderVerdict
		}
	}
	return nil
}

func (m *memStore) CancelRun(_ context.Context, id int64, at time.Time) (bool, error) {
	for _, r := range m.runs {
		if r.ID == id && r.Status == StatusRunning {
			r.Status = StatusCancelled
			t := at
			r.CompletedAt = &t
			return true, nil
		}
	}
	return false, nil
}

func (m *memStore) GetRun(_ context.Context, id int64) (*Run, error) {
	for _, r := range m.runs {
		if r.ID == id {
			return r, nil
		}
	}
	return nil, ErrRunNotFound
}

func (m *memStore) GetRunDetail(_ context.Context, id int64) (*RunDetail, error) {
	for _, r := range m.runs {
		if r.ID == id {
			return &RunDetail{Run: *r, Items: m.items[id]}, nil
		}
	}
	return nil, ErrRunNotFound
}

func (m *memStore) ListRuns(_ context.Context, _ RunFilter) ([]Run, int64, error) {
	out := make([]Run, 0, len(m.runs))
	for _, r := range m.runs {
		out = append(out, *r)
	}
	return out, int64(len(out)), nil
}

func (m *memStore) GetStats(_ context.Context, _ RunFilter) (Stats, error) {
	return Stats{RunCount: int64(len(m.runs))}, nil
}

func (m *memStore) GetVisibility(_ context.Context) (*Visibility, error) {
	v := m.vis
	return &v, nil
}

func (m *memStore) SetGroupHidden(_ context.Context, _ int64, _ bool) error   { return nil }
func (m *memStore) SetHostHidden(_ context.Context, _ string, _ bool) error   { return nil }
func (m *memStore) ListUnmatchedHosts(_ context.Context) ([]HostUsage, error) { return nil, nil }

// ── 校验测试 ──────────────────────────────────────────────────────────

func validRequest() StartRequest {
	return StartRequest{
		BaseURL:              "https://8.8.8.8",
		APIKey:               "sk-test-key",
		ModelID:              "gpt-4",
		Protocol:             ProtocolChatCompletions,
		ReasoningEffort:      EffortHigh,
		RenderTimeoutSeconds: 900,
		Tasks:                []string{TaskTypeLogic},
		Questions: []Question{
			{TaskType: TaskTypeLogic, Key: "candy-shape-flavor", Prompt: "题目", Expected: "21"},
		},
	}
}

func TestStart_ValidationOrderAndMessages(t *testing.T) {
	// 校验顺序要稳定：前端先拦一次、后端再拦一次，文案必须一致，
	// 否则用户会看到「前端说 A、后端说 B」。
	cases := []struct {
		name   string
		mutate func(*StartRequest)
		want   error
	}{
		{"缺 api key", func(r *StartRequest) { r.APIKey = "" }, ErrAPIKeyMissing},
		{"缺模型", func(r *StartRequest) { r.ModelID = "" }, ErrModelMissing},
		{"缺题目", func(r *StartRequest) { r.Questions = nil }, ErrNoQuestions},
		{"缺检测项", func(r *StartRequest) { r.Tasks = nil }, ErrNoTasks},
		{
			"生成题超时超范围",
			func(r *StartRequest) {
				r.Tasks = []string{TaskTypeRender}
				r.RenderTimeoutSeconds = 7200 // 120 分钟，超出上限 60
			},
			ErrRenderTimeout,
		},
		{
			"自定义逻辑题缺期望答案",
			func(r *StartRequest) {
				r.Questions = []Question{{TaskType: TaskTypeLogic, Key: "", Prompt: "自定义题", Expected: ""}}
			},
			ErrLogicNeedsExpect,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemStore()
			svc := NewService(store, nil)
			req := validRequest()
			tc.mutate(&req)

			_, err := svc.Start(context.Background(), 1, "a@b.com", req)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			// 校验失败不应留下任何运行记录。
			if len(store.runs) != 0 {
				t.Fatalf("校验失败时不应创建运行记录，实际 %d 条", len(store.runs))
			}
		})
	}
}

func TestStart_RejectsPrivateBaseURL(t *testing.T) {
	// 这是「用户填的地址不会变成内网探测跳板」的直接证据。
	cases := []string{
		"https://127.0.0.1",
		"https://10.0.0.1",
		"https://169.254.169.254",
		"http://8.8.8.8",
	}
	for _, base := range cases {
		t.Run(base, func(t *testing.T) {
			store := newMemStore()
			svc := NewService(store, nil)
			req := validRequest()
			req.BaseURL = base

			if _, err := svc.Start(context.Background(), 1, "a@b.com", req); err == nil {
				t.Fatalf("BaseURL=%q 应被拒绝", base)
			}
			if len(store.runs) != 0 {
				t.Fatal("地址被拒时不应创建运行记录")
			}
		})
	}
}

func TestStart_CredentialsNeverPersisted(t *testing.T) {
	// 「凭据不落库」是承诺。这里用内存 Store 抓取实际写进去的内容，
	// 逐字段确认没有任何地方能装下 api_key 或原始 base_url。
	store := newMemStore()
	svc := NewService(store, nil)

	secretKey := "sk-super-secret-key-do-not-store"
	rawBase := "https://8.8.8.8/v1/private-tenant"

	req := validRequest()
	req.APIKey = secretKey
	req.BaseURL = rawBase

	if _, err := svc.Start(context.Background(), 1, "a@b.com", req); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	if len(store.runs) != 1 {
		t.Fatalf("应创建 1 条运行记录，实际 %d", len(store.runs))
	}

	r := store.runs[0]

	// 落库的 base_url 必须是脱敏形态：保留 host，抹掉路径。
	if r.BaseURLDisplay != "https://8.8.8.8/***" {
		t.Fatalf("BaseURLDisplay = %q，必须脱敏成 https://8.8.8.8/***", r.BaseURLDisplay)
	}
	// 路径里的私有租户信息不能出现。
	if containsSubstring(r.BaseURLDisplay, "private-tenant") {
		t.Fatalf("脱敏后的地址泄漏了路径信息: %q", r.BaseURLDisplay)
	}

	// Run 结构体里根本没有 api_key 字段，这里做一次显式断言，
	// 防止将来有人给它加回来。
	if containsSubstring(anyToString(r), secretKey) {
		t.Fatal("运行记录里出现了 api_key")
	}
}

// anyToString 把 Run 的关键字段拼成一个字符串用于「是否泄漏」检查。
func anyToString(r *Run) string {
	return r.OwnerEmail + "|" + r.BaseURLDisplay + "|" + r.ModelID + "|" +
		r.Protocol + "|" + r.ReasoningEffort + "|" + r.ErrorMessage + "|" + r.GroupName
}

func containsSubstring(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestNormalizeEffort(t *testing.T) {
	// messages 协议不支持 effort，必须清空 —— 否则发出去就是 400，
	// 那个 400 会被归一化成 server_error，看起来像上游挂了。
	if got := normalizeEffort(ProtocolMessages, EffortHigh); got != "" {
		t.Fatalf("messages 的 effort 应被清空，实际 %q", got)
	}
	if got := normalizeEffort(ProtocolChatCompletions, EffortHigh); got != EffortHigh {
		t.Fatalf("chat_completions 应保留 effort，实际 %q", got)
	}
	if got := normalizeEffort(ProtocolResponses, EffortLow); got != EffortLow {
		t.Fatalf("responses 应保留 effort，实际 %q", got)
	}
}

func TestContains(t *testing.T) {
	if !contains([]string{"a", "render"}, "render") {
		t.Fatal("应找到")
	}
	if contains([]string{"a"}, "render") {
		t.Fatal("不应找到")
	}
	if contains(nil, "render") {
		t.Fatal("nil 切片不应找到")
	}
}

func TestStart_ConcurrencyGate(t *testing.T) {
	// 同一用户同时只允许一个在跑的检测。
	// 否则同时开多个生成题会互相限流，全部失败 —— 那看起来像降智。
	store := newMemStore()
	svc := NewService(store, nil)

	// 手动占用闸门，模拟「已有一个在跑」。
	svc.mu.Lock()
	svc.inflight[1] = func() {}
	svc.mu.Unlock()

	_, err := svc.Start(context.Background(), 1, "a@b.com", validRequest())
	if !errors.Is(err, ErrConcurrencyLimit) {
		t.Fatalf("err = %v, want ErrConcurrencyLimit", err)
	}

	// 换个用户应当不受影响（靠的是每用户闸门，不是全局锁）。
	svc.mu.Lock()
	delete(svc.inflight, 1)
	svc.mu.Unlock()

	if _, err := svc.Start(context.Background(), 2, "c@d.com", validRequest()); err != nil {
		t.Fatalf("另一个用户应能发起: %v", err)
	}
}

func TestCancel_AlreadyEnded(t *testing.T) {
	// 已经结束时返回 ErrRunNotRunning —— 对应参考实现的 409，
	// 前端据此把它当「已完成」而不是失败。
	store := newMemStore()
	svc := NewService(store, nil)

	if _, err := store.CreateRun(context.Background(), RunCreateInput{UserID: 1}); err != nil {
		t.Fatalf("CreateRun 失败: %v", err)
	}
	store.runs[0].Status = StatusCompleted

	if err := svc.Cancel(context.Background(), 1, 1); !errors.Is(err, ErrRunNotRunning) {
		t.Fatalf("err = %v, want ErrRunNotRunning", err)
	}
}

func TestCancel_NotFound(t *testing.T) {
	store := newMemStore()
	svc := NewService(store, nil)
	if err := svc.Cancel(context.Background(), 999, 1); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("err = %v, want ErrRunNotFound", err)
	}
}

func TestTaskBank_SplitsByType(t *testing.T) {
	store := newMemStore()
	store.tasks = []Task{
		{ID: 1, Type: TaskTypeLogic, Key: "a", Title: "逻辑题"},
		{ID: 2, Type: TaskTypeRender, Key: "b", Title: "生成题"},
		{ID: 3, Type: TaskTypeLogic, Key: "c", Title: "逻辑题2"},
	}
	svc := NewService(store, nil)

	bank, err := svc.TaskBank(context.Background())
	if err != nil {
		t.Fatalf("TaskBank 失败: %v", err)
	}
	if len(bank.Logic) != 2 {
		t.Fatalf("Logic 数量 = %d, want 2", len(bank.Logic))
	}
	if len(bank.Render) != 1 {
		t.Fatalf("Render 数量 = %d, want 1", len(bank.Render))
	}
}

func TestTaskBank_EmptyIsNotNil(t *testing.T) {
	// 空题库要返回空切片而不是 nil，否则前端 JSON 会拿到 null 而不是 []，
	// 模板里 v-for 遍历 null 会报错。
	store := newMemStore()
	svc := NewService(store, nil)

	bank, err := svc.TaskBank(context.Background())
	if err != nil {
		t.Fatalf("TaskBank 失败: %v", err)
	}
	if bank.Logic == nil {
		t.Fatal("Logic 不应为 nil")
	}
	if bank.Render == nil {
		t.Fatal("Render 不应为 nil")
	}
}

func TestGetRunDetail_IsOwnerFlag(t *testing.T) {
	store := newMemStore()
	svc := NewService(store, nil)
	if _, err := store.CreateRun(context.Background(), RunCreateInput{UserID: 5}); err != nil {
		t.Fatalf("CreateRun 失败: %v", err)
	}

	d, err := svc.GetRunDetail(context.Background(), 1, 5)
	if err != nil {
		t.Fatalf("GetRunDetail 失败: %v", err)
	}
	if !d.IsOwner {
		t.Fatal("本人查看时 IsOwner 应为 true")
	}

	d2, err := svc.GetRunDetail(context.Background(), 1, 6)
	if err != nil {
		t.Fatalf("GetRunDetail 失败: %v", err)
	}
	if d2.IsOwner {
		t.Fatal("他人查看时 IsOwner 应为 false")
	}
}
