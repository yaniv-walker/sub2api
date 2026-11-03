package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	upstreamsvc "github.com/Wei-Shaw/sub2api/internal/service"
)

// userTestStore 只实现自测用到的存储方法。
//
// 用嵌入接口而不是实现整个 Store：Store 有 30+ 个方法，为一次单测全实现
// 会让测试文件淹没在样板代码里，且新增方法时每个假实现都要跟着改。
// 嵌入后未被覆盖的方法调用会 nil panic —— 这恰好也是我们想要的：
// 一旦自测链路偷偷调用了不该用的存储方法，测试会立刻炸出来。
type userTestStore struct {
	Store

	cases     map[int64]iqbenchmark.Case
	quota     iqbenchmark.UserTestQuotaSnapshot
	quotaErr  error
	dedupe    int
	created   []iqbenchmark.UserTestCreateInput
	finished  []finishCall
	records   map[int64]*iqbenchmark.UserTest
	listItems []iqbenchmark.UserTest
	listTotal int64
	createErr error
	// lastPage/lastPageSize 记录实际传给存储层的分页参数（用于断言夹取）。
	lastPage     int
	lastPageSize int
}

type finishCall struct {
	ID         int64
	Status     string
	ErrorCode  string
	ArtifactID int64
}

func (s *userTestStore) GetCase(_ context.Context, id int64) (*iqbenchmark.Case, error) {
	c, ok := s.cases[id]
	if !ok {
		return nil, iqbenchmark.ErrCaseNotFound
	}
	return &c, nil
}

func (s *userTestStore) ListCasesByIDs(_ context.Context, ids []int64) (map[int64]iqbenchmark.Case, error) {
	out := map[int64]iqbenchmark.Case{}
	for _, id := range ids {
		if c, ok := s.cases[id]; ok {
			out[id] = c
		}
	}
	return out, nil
}

func (s *userTestStore) UserTestQuota(_ context.Context, _ int64, _ time.Time) (iqbenchmark.UserTestQuotaSnapshot, error) {
	if s.quotaErr != nil {
		return iqbenchmark.UserTestQuotaSnapshot{}, s.quotaErr
	}
	return s.quota, nil
}

func (s *userTestStore) CountUserTestDedupe(_ context.Context, _ string, _ time.Time) (int, error) {
	return s.dedupe, nil
}

func (s *userTestStore) CreateUserTest(_ context.Context, in iqbenchmark.UserTestCreateInput) (*iqbenchmark.UserTest, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	s.created = append(s.created, in)
	record := &iqbenchmark.UserTest{
		ID:        int64(len(s.created)),
		UserID:    in.UserID,
		APIKeyID:  in.APIKeyID,
		CaseID:    in.CaseID,
		GroupID:   in.GroupID,
		Model:     in.Model,
		Status:    iqbenchmark.UserTestStatusPending,
		CreatedAt: time.Unix(1700000000, 0),
	}
	if s.records == nil {
		s.records = map[int64]*iqbenchmark.UserTest{}
	}
	s.records[record.ID] = record
	return record, nil
}

func (s *userTestStore) GetUserTest(_ context.Context, id int64) (*iqbenchmark.UserTest, error) {
	if r, ok := s.records[id]; ok {
		return r, nil
	}
	return nil, iqbenchmark.ErrUserTestNotFound
}

func (s *userTestStore) ListUserTestsByUser(_ context.Context, _ int64, page, pageSize int) ([]iqbenchmark.UserTest, int64, error) {
	s.lastPage = page
	s.lastPageSize = pageSize
	return s.listItems, s.listTotal, nil
}

func (s *userTestStore) FinishUserTest(_ context.Context, id int64, status, errorCode, _ string, artifactID int64, _ float64) error {
	s.finished = append(s.finished, finishCall{ID: id, Status: status, ErrorCode: errorCode, ArtifactID: artifactID})
	if r, ok := s.records[id]; ok {
		r.Status = status
		r.ErrorCode = errorCode
		r.ArtifactID = artifactID
	}
	return nil
}

// fakeKeyResolver 是 KeyResolver 的替身。
type fakeKeyResolver struct {
	owned      []int64
	verifyErr  error
	key        *upstreamsvc.APIKey
	getErr     error
	checkErr   error
	verifyCall int
}

func (f *fakeKeyResolver) VerifyOwnership(_ context.Context, _ int64, _ []int64) ([]int64, error) {
	f.verifyCall++
	if f.verifyErr != nil {
		return nil, f.verifyErr
	}
	return f.owned, nil
}

func (f *fakeKeyResolver) GetByID(_ context.Context, _ int64) (*upstreamsvc.APIKey, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.key, nil
}

func (f *fakeKeyResolver) CheckAPIKeyQuotaAndExpiry(_ *upstreamsvc.APIKey) error {
	return f.checkErr
}

// fakeUserTestLimiter 是 UserTestLimiter 的替身。
type fakeUserTestLimiter struct {
	slotOK  bool
	release int
}

func (f *fakeUserTestLimiter) AcquireUserTestSlot(_ context.Context, _ int64, _ int) (func(), bool) {
	if !f.slotOK {
		return nil, false
	}
	return func() { f.release++ }, true
}

// newUserTestFixture 构造一份可直接驱动 Submit 的完整依赖。
//
// 复用 targets_test.go 里的 fakeGroupStore / newTargetsFixture，
// 避免同一份替身在两个测试文件里各写一遍（签名一改就要改两处）。
func newUserTestFixture(t *testing.T, settings Settings) (*UserTestService, *userTestStore, *fakeKeyResolver) {
	t.Helper()
	store := &userTestStore{
		cases: map[int64]iqbenchmark.Case{
			7: {
				ID: 7, Name: "cap", Kind: iqbenchmark.CaseKindText,
				Prompt: "hi", Enabled: true, AllowUserTest: true,
			},
		},
	}
	keys := &fakeKeyResolver{
		owned: []int64{11},
		key: &upstreamsvc.APIKey{
			ID: 11, UserID: 42, Key: "sk-user-plaintext", Status: upstreamsvc.StatusActive,
		},
	}
	targets, _ := newTargetsFixture(
		map[int64][]string{3: {"gpt-4o"}},
		[]upstreamsvc.Group{
			{
				ID: 3, Name: "g", Status: upstreamsvc.StatusActive,
				AllowImageGeneration: true,
				// AccountCount 必须非零：ResolveGroupModels 会把「没有可调度账号」
				// 视为空态并拒绝，这是与线上一致的行为，不是测试噪声。
				AccountCount: 1, ActiveAccountCount: 1,
			},
		},
	)
	settingsProvider := iqbenchmark.NewSettingsProvider(&staticSettingsStore{value: settings}, nil)
	svc := NewUserTestService(
		store, nil, keys, targets, settingsProvider,
		&fakeUserTestLimiter{slotOK: true},
		slog.New(slog.DiscardHandler),
	)
	svc.Async = false
	return svc, store, keys
}

// staticSettingsStore 是固定返回一份设置的 SettingsStore。
//
// provider 用 json.Unmarshal 解析存储值，所以这里必须序列化成 JSON
// （直接返回结构体字段拼接的字符串会被判为 malformed 而回落默认值，
// 那样测的就不是我们传进去的那份设置了）。
type staticSettingsStore struct {
	value Settings
}

func (s *staticSettingsStore) Get(_ context.Context, _ string) (string, error) {
	payload, err := json.Marshal(s.value)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func (s *staticSettingsStore) Set(_ context.Context, _, _ string) error { return nil }

// userTestOn 返回自测已开启的设置。
func userTestOn() Settings {
	s := DefaultSettings()
	s.Enabled = true
	s.UserTestEnabled = true
	s.UserTestDailyLimit = 5
	s.UserTestMaxConcurrent = 1
	// 分组白名单：默认空的语义是「什么都不显示」（fail-closed），
	// 而这里的测试夹具用的是分组 3 / 5。不显式登记它们，
	// 所有自测用例都会被白名单挡掉 —— 那是正确行为，但不是这些
	// 用例想测的东西。凡是要放宽可见性的用例，都必须显式写出来。
	s.ExposeGroups = []iqbenchmark.ExposeGroup{
		{GroupID: 3, ExposeName: "组三", SortOrder: 1},
		{GroupID: 5, ExposeName: "组五", SortOrder: 2},
	}
	return s
}

// TestUserTestRejectsWhenDisabled 覆盖开关闸门。
func TestUserTestRejectsWhenDisabled(t *testing.T) {
	settings := userTestOn()
	settings.UserTestEnabled = false
	svc, store, _ := newUserTestFixture(t, settings)

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.ErrorIs(t, err, iqbenchmark.ErrUserTestDisabled)
	require.Empty(t, store.created, "a rejected request must not create a record")
}

// TestUserTestRejectsCaseNotOpen 覆盖「题目未开放自测」。
func TestUserTestRejectsCaseNotOpen(t *testing.T) {
	svc, store, _ := newUserTestFixture(t, userTestOn())
	store.cases[7] = iqbenchmark.Case{
		ID: 7, Name: "cap", Kind: iqbenchmark.CaseKindText, Prompt: "hi",
		Enabled: true, AllowUserTest: false,
	}

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.ErrorIs(t, err, iqbenchmark.ErrCaseNotOpenForUserTest)
	require.Empty(t, store.created)
}

// TestUserTestRejectsForeignKey 覆盖「Key 不属于我」必须与「不存在」返回同一个错误。
func TestUserTestRejectsForeignKey(t *testing.T) {
	svc, store, keys := newUserTestFixture(t, userTestOn())
	keys.owned = nil // 归属校验查不到

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.ErrorIs(t, err, iqbenchmark.ErrAPIKeyNotOwned)
	require.Empty(t, store.created)
}

// TestUserTestRejectsInactiveKey 覆盖 Key 被禁用。
func TestUserTestRejectsInactiveKey(t *testing.T) {
	svc, store, keys := newUserTestFixture(t, userTestOn())
	inactive := *keys.key
	inactive.Status = "disabled"
	keys.key = &inactive

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.ErrorIs(t, err, iqbenchmark.ErrAPIKeyInactive)
	require.Empty(t, store.created)
}

// TestUserTestRejectsExpiredKey 覆盖 Key 过期映射为专门的哨兵（403 而不是 402）。
func TestUserTestRejectsExpiredKey(t *testing.T) {
	svc, store, keys := newUserTestFixture(t, userTestOn())
	keys.checkErr = errors.New("api key has expired")

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.ErrorIs(t, err, iqbenchmark.ErrAPIKeyExpired)
	require.Empty(t, store.created)
}

// TestUserTestRejectsEmptyKeyMaterial 覆盖「Key 记录存在但明文为空」。
//
// 这种情况真实存在（历史数据），必须挡在提交前，否则会以用户名义
// 发出一个必然 401 的请求。
func TestUserTestRejectsEmptyKeyMaterial(t *testing.T) {
	svc, store, keys := newUserTestFixture(t, userTestOn())
	blank := *keys.key
	blank.Key = "   "
	keys.key = &blank

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.ErrorIs(t, err, iqbenchmark.ErrAPIKeyNotOwned)
	require.Empty(t, store.created)
}

// TestUserTestDailyLimit 覆盖日次数配额。
func TestUserTestDailyLimit(t *testing.T) {
	svc, store, _ := newUserTestFixture(t, userTestOn())
	store.quota = iqbenchmark.UserTestQuotaSnapshot{TodayCount: 5}

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.ErrorIs(t, err, iqbenchmark.ErrUserTestDailyLimit)
	require.Empty(t, store.created)
}

// TestUserTestConcurrentLimit 覆盖在途数配额。
func TestUserTestConcurrentLimit(t *testing.T) {
	svc, store, _ := newUserTestFixture(t, userTestOn())
	store.quota = iqbenchmark.UserTestQuotaSnapshot{InFlightCount: 1}

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.ErrorIs(t, err, iqbenchmark.ErrUserTestConcurrent)
	require.Empty(t, store.created)
}

// TestUserTestCostBudgetZeroMeansUnlimited 覆盖用户裁决：
// daily_cost_budget=0 表示不限制，即使当日成本已经很大也必须放行。
func TestUserTestCostBudgetZeroMeansUnlimited(t *testing.T) {
	settings := userTestOn()
	settings.UserTestDailyCostBudget = 0
	svc, store, _ := newUserTestFixture(t, settings)
	store.quota = iqbenchmark.UserTestQuotaSnapshot{TodayCost: 999999}

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.NoError(t, err, "a zero budget must mean unlimited, not zero spend")
	require.Len(t, store.created, 1)
}

// TestUserTestCostBudgetBlocks 覆盖预算生效时把「预估」也算进去。
func TestUserTestCostBudgetBlocks(t *testing.T) {
	settings := userTestOn()
	settings.UserTestDailyCostBudget = 1.0
	svc, store, _ := newUserTestFixture(t, settings)
	store.quota = iqbenchmark.UserTestQuotaSnapshot{TodayCost: 0.9}

	in := validSubmitInput()
	in.EstimatedCost = 0.2 // 0.9 + 0.2 > 1.0
	_, err := svc.Submit(context.Background(), in)
	require.ErrorIs(t, err, iqbenchmark.ErrUserTestBudgetExceeded)
	require.Empty(t, store.created)
}

// TestUserTestDedupe 覆盖去重窗口。
func TestUserTestDedupe(t *testing.T) {
	svc, store, _ := newUserTestFixture(t, userTestOn())
	store.dedupe = 1

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.ErrorIs(t, err, iqbenchmark.ErrUserTestDuplicate)
	require.Empty(t, store.created)
}

// TestUserTestBuildsUserTestIdentity 是本次改动最重要的断言：
// 自测构造出来的身份必须是 user_test + 真实用户 + 用户自己的 Key，
// 且 run/task 必须为 0（由仓库层写成 NULL）。
func TestUserTestBuildsUserTestIdentity(t *testing.T) {
	svc, store, _ := newUserTestFixture(t, userTestOn())

	record, err := svc.Submit(context.Background(), validSubmitInput())
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Len(t, store.created, 1)

	created := store.created[0]
	require.Equal(t, int64(42), created.UserID, "the record must belong to the requesting user")
	require.Equal(t, int64(11), created.APIKeyID)
	require.Equal(t, "gpt-4o", created.Model)
	require.NotEmpty(t, created.DedupeKey, "a dedupe key must always be written")

	// 只断言「是 64 位十六进制的 SHA-256」，不要断言它不包含某个短数字串：
	// 64 个 hex 字符里出现 "42" 几乎是必然（概率约 1/256 每位置），
	// 那种断言会随机翻红，而且并不能证明任何有价值的东西。
	require.Len(t, created.DedupeKey, 64)
	require.Regexp(t, "^[0-9a-f]{64}$", created.DedupeKey)
	// 真正要防的是「明文拼接」：若把 model 或 id 直接拼进去，
	// 键里会出现分隔符或非 hex 字符。
	require.NotContains(t, created.DedupeKey, ":")
	require.NotContains(t, created.DedupeKey, "gpt-4o")
}

// TestUserTestGetEnforcesOwnership 覆盖「不能靠传别人的 ID 读取记录」。
func TestUserTestGetEnforcesOwnership(t *testing.T) {
	svc, store, _ := newUserTestFixture(t, userTestOn())
	store.records = map[int64]*iqbenchmark.UserTest{
		9: {ID: 9, UserID: 999, Status: iqbenchmark.UserTestStatusSuccess},
	}

	_, err := svc.Get(context.Background(), 42, 9)
	require.ErrorIs(t, err, iqbenchmark.ErrUserTestNotFound,
		"another user's record must look exactly like a missing one")
}

// TestUserTestListClampsPageSize 覆盖分页参数归一。
//
// 断言实际传给存储层的 page/pageSize，而不是返回值 —— 后者在假实现里
// 恒等于固定值，测不出「有没有夹取」。
func TestUserTestListClampsPageSize(t *testing.T) {
	svc, store, _ := newUserTestFixture(t, userTestOn())

	// 超上限：pageSize 必须被夹到 UserTestListMaxPageSize。
	_, _, err := svc.List(context.Background(), 42, 0, 9999)
	require.NoError(t, err)
	require.Equal(t, UserTestListMaxPageSize, store.lastPageSize)
	require.Equal(t, 1, store.lastPage, "page<=0 must normalize to 1")

	// 非法 0：pageSize 回落默认值 20。
	_, _, err = svc.List(context.Background(), 42, 1, 0)
	require.NoError(t, err)
	require.Equal(t, 20, store.lastPageSize)

	// 合法值原样透传。
	_, _, err = svc.List(context.Background(), 42, 3, 7)
	require.NoError(t, err)
	require.Equal(t, 3, store.lastPage)
	require.Equal(t, 7, store.lastPageSize)
}

// TestStartOfDayUsesLocalMidnight 覆盖「今天」的时区口径。
//
// 关键断言不是 Location 的名字（t.Local() 一律返回 time.Local 这个单例，
// 名字永远是 "Local"），而是**取到的零点在本地时区里确实是 00:00**。
// 若实现改用 UTC，在东八区的 21:52 会得到前一天 16:00，日与小时都会不符。
func TestStartOfDayUsesLocalMidnight(t *testing.T) {
	// 固定在一个「本地时间与 UTC 不同日或不同小时」的时刻上，
	// 这样任何把时区写死成 UTC 的实现都会失败。
	loc := time.Local
	now := time.Date(2026, 11, 2, 21, 52, 0, 0, loc)

	start := startOfDay(now)
	require.Equal(t, 0, start.Hour())
	require.Equal(t, 0, start.Minute())
	require.Equal(t, 0, start.Second())
	require.Equal(t, 2, start.Day(), "the day must match the local calendar day")
	require.Equal(t, 11, int(start.Month()))
	// 同一天零点与 now 的偏移必须是当天已过的时长。
	require.Equal(t, 21*time.Hour+52*time.Minute, now.Sub(start))
}

// TestMapKeyCheckError 覆盖 Key 校验错误的映射。
func TestMapKeyCheckError(t *testing.T) {
	require.Nil(t, mapKeyCheckError(nil))
	require.ErrorIs(t, mapKeyCheckError(errors.New("api key has expired")), iqbenchmark.ErrAPIKeyExpired)
	require.ErrorIs(t, mapKeyCheckError(errors.New("quota exceeded")), iqbenchmark.ErrAPIKeyQuotaExhausted)
	// 未知原因一律按不可用处理（宁可拒绝，也不要带着可疑 Key 去打网关）。
	require.ErrorIs(t, mapKeyCheckError(errors.New("boom")), iqbenchmark.ErrAPIKeyInactive)
}

// TestUserTestImageCaseRequiresGroupCapability 覆盖图片题的分组能力预检。
func TestUserTestImageCaseRequiresGroupCapability(t *testing.T) {
	svc, store, _ := newUserTestFixture(t, userTestOn())
	store.cases[7] = iqbenchmark.Case{
		ID: 7, Name: "img", Kind: iqbenchmark.CaseKindImage,
		Prompt: "draw", Enabled: true, AllowUserTest: true,
	}

	// 换一个不允许图片生成的分组。
	svc.Targets, _ = newTargetsFixture(
		map[int64][]string{3: {"gpt-4o"}},
		[]upstreamsvc.Group{
			{
				ID: 3, Name: "g", Status: upstreamsvc.StatusActive,
				AllowImageGeneration: false,
				AccountCount:         1, ActiveAccountCount: 1,
			},
		},
	)

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.ErrorIs(t, err, iqbenchmark.ErrGroupImageGenerationDisabled)
	require.Empty(t, store.created)
}

// TestUserTestRejectsWhenSlotUnavailable 覆盖原子并发槽位被占满。
//
// 这条与 TestUserTestConcurrentLimit 不同：那个测的是「数据库快照已满」，
// 这个测的是「快照还来得及但原子计数已满」——两者同时存在，
// 因为快照读有 TOCTOU 窗口，必须由原子槽位补上。
func TestUserTestRejectsWhenSlotUnavailable(t *testing.T) {
	svc, store, _ := newUserTestFixture(t, userTestOn())
	limiter := &fakeUserTestLimiter{slotOK: false}
	svc.Limiter = limiter

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.ErrorIs(t, err, iqbenchmark.ErrUserTestConcurrent)
	require.Empty(t, store.created, "a request rejected by the slot must not write a record")
}

// TestUserTestReleasesSlotOnPreflightFailure 覆盖「前置失败必须归还槽位」。
//
// 不归还的话，用户几次被拒的尝试就能把自己的并发额度永久占满 ——
// 这是最隐蔽的一类自锁：错误信息说「并发已满」，而实际上没有任何任务在跑。
func TestUserTestReleasesSlotOnPreflightFailure(t *testing.T) {
	svc, store, _ := newUserTestFixture(t, userTestOn())
	limiter := &fakeUserTestLimiter{slotOK: true}
	svc.Limiter = limiter
	// 去重命中：发生在槽位占用之后、落记录之前。
	store.dedupe = 1

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.ErrorIs(t, err, iqbenchmark.ErrUserTestDuplicate)
	require.Equal(t, 1, limiter.release,
		"the slot must be released when the request fails after acquiring it")
}

// TestUserTestReleasesSlotOnSuccess 覆盖成功路径同样归还槽位。
func TestUserTestReleasesSlotOnSuccess(t *testing.T) {
	svc, store, _ := newUserTestFixture(t, userTestOn())
	limiter := &fakeUserTestLimiter{slotOK: true}
	svc.Limiter = limiter

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.NoError(t, err)
	require.Len(t, store.created, 1)
	require.Equal(t, 1, limiter.release, "the slot must be released exactly once")
}

// TestUserTestWorksWithoutLimiter 覆盖未接限流器的部署。
//
// Limiter 是附加防线而不是正确性前提：为 nil 时必须正常放行，
// 否则所有未接 Redis 的部署自测都完全不可用。
func TestUserTestWorksWithoutLimiter(t *testing.T) {
	svc, store, _ := newUserTestFixture(t, userTestOn())
	svc.Limiter = nil

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.NoError(t, err)
	require.Len(t, store.created, 1)
}

// TestUserTestGroupBoundToKeyWins 覆盖本次修复的核心漏洞：
// 请求里的 group_id 与 Key 绑定的分组不一致时，必须以后者为准。
//
// 为什么这是安全问题而不只是行为偏好：上游网关完全按 apiKey.GroupID
// 路由（billing_inflight_reservation.go 的 resolveGroupByAPIKey）。
// 若信任请求里的分组，用户就能选一个「不允许生图」的分组提交图片题，
// 让本地预检通过、实际在另一个分组里执行；更糟的是产出被记到 A 分组、
// 却在 B 分组跑出来，排行榜会把这个成绩算在错误的模型头上。
func TestUserTestGroupBoundToKeyWins(t *testing.T) {
	svc, store, keys := newUserTestFixture(t, userTestOn())
	// Key 实际绑在分组 3 上，但请求谎称分组 5。
	bound := int64(3)
	keys.key.GroupID = &bound
	in := validSubmitInput()
	in.GroupID = 5

	_, err := svc.Submit(context.Background(), in)
	require.NoError(t, err)
	require.Len(t, store.created, 1)
	require.Equal(t, int64(3), store.created[0].GroupID,
		"the persisted group must come from the key binding, not the request")
}

// TestUserTestUnboundKeyUsesRequestGroup 保证未绑分组的 Key 仍可用请求值：
// 上游对未绑定的 Key 就是按平台主键回落，此时请求里的分组才有意义。
func TestUserTestUnboundKeyUsesRequestGroup(t *testing.T) {
	svc, store, keys := newUserTestFixture(t, userTestOn())
	keys.key.GroupID = nil

	_, err := svc.Submit(context.Background(), validSubmitInput())
	require.NoError(t, err)
	require.Len(t, store.created, 1)
	require.Equal(t, int64(3), store.created[0].GroupID)
}

// TestUserTestDedupeKeyIncludesGroup 保证同一题在不同分组不算重复。
//
// 漏掉分组会让用户 10 分钟内无法测第二个分组，而错误提示是
// 「刚刚已提交过」—— 用户只会以为系统卡了。
func TestUserTestDedupeKeyIncludesGroup(t *testing.T) {
	a := userTestDedupeKey(1, 2, 3, "m")
	b := userTestDedupeKey(1, 2, 4, "m")
	require.NotEqual(t, a, b, "different groups must produce different dedupe keys")
	require.Len(t, a, 64)
}

// validSubmitInput 是一份通过所有前置校验的入参。
func validSubmitInput() iqbenchmark.UserTestSubmitInput {
	return iqbenchmark.UserTestSubmitInput{
		UserID:   42,
		APIKeyID: 11,
		CaseID:   7,
		GroupID:  3,
		Model:    "gpt-4o",
	}
}

// ---------- MyTargets ----------
//
// MyTargets 是普通用户唯一能拿到「可测分组 + 模型」的入口（管理侧的
// /admin/iq-benchmark/targets 需要管理员身份）。它错了，用户只会撞上
// MODEL_NOT_AVAILABLE 而无法自行纠正。

// TestMyTargetsAlwaysReturnsGroups 覆盖未带 Key 的场景：应原样返回全部分组。
func TestMyTargetsAlwaysReturnsGroups(t *testing.T) {
	svc, _, _ := newUserTestFixture(t, userTestOn())

	targets, err := svc.MyTargets(context.Background(), 42, 0)
	require.NoError(t, err)
	require.Len(t, targets.Groups, 1)
	require.Equal(t, int64(3), targets.Groups[0].ID)
}

// TestMyTargetsNarrowsToKeyBoundGroup 覆盖 Key 绑组时的收窄。
//
// 与 Submit 的分组解析必须同源：若这里给出多个分组而 Submit 只接受
// Key 那一个，用户选了别的分组就会莫名失败。
func TestMyTargetsNarrowsToKeyBoundGroup(t *testing.T) {
	svc, _, keys := newUserTestFixture(t, userTestOn())
	// 造两个分组，Key 绑定到 3。
	svc.Targets, _ = newTargetsFixture(
		map[int64][]string{3: {"gpt-4o"}, 5: {"claude-x"}},
		[]upstreamsvc.Group{
			{ID: 3, Name: "g3", Status: upstreamsvc.StatusActive, AllowImageGeneration: true,
				AccountCount: 1, ActiveAccountCount: 1},
			{ID: 5, Name: "g5", Status: upstreamsvc.StatusActive, AllowImageGeneration: true,
				AccountCount: 1, ActiveAccountCount: 1},
		},
	)
	bound := int64(3)
	keys.key = &upstreamsvc.APIKey{
		ID: 11, UserID: 42, Key: "sk-user-plaintext",
		Status: upstreamsvc.StatusActive, GroupID: &bound,
	}

	targets, err := svc.MyTargets(context.Background(), 42, 11)
	require.NoError(t, err)
	require.Len(t, targets.Groups, 1, "a group-bound key must expose exactly one group")
	require.Equal(t, int64(3), targets.Groups[0].ID)
}

// TestMyTargetsForeignKeyIsNotResolved 保证别人的 Key 不会收窄结果。
//
// VerifyOwnership 返回空即「不是你的」，此时必须**忽略**这把 Key，
// 而不是拿它的 GroupID 去过滤 —— 否则用户可以通过猜 Key ID 探到
// 别人分组的存在性。
func TestMyTargetsForeignKeyIsNotResolved(t *testing.T) {
	svc, _, keys := newUserTestFixture(t, userTestOn())
	svc.Targets, _ = newTargetsFixture(
		map[int64][]string{3: {"gpt-4o"}, 5: {"claude-x"}},
		[]upstreamsvc.Group{
			{ID: 3, Name: "g3", Status: upstreamsvc.StatusActive, AllowImageGeneration: true,
				AccountCount: 1, ActiveAccountCount: 1},
			{ID: 5, Name: "g5", Status: upstreamsvc.StatusActive, AllowImageGeneration: true,
				AccountCount: 1, ActiveAccountCount: 1},
		},
	)
	keys.owned = nil // 不属于该用户
	bound := int64(5)
	keys.key = &upstreamsvc.APIKey{
		ID: 11, UserID: 99, Key: "sk-other",
		Status: upstreamsvc.StatusActive, GroupID: &bound,
	}

	targets, err := svc.MyTargets(context.Background(), 42, 11)
	require.NoError(t, err)
	require.Len(t, targets.Groups, 2, "someone else's key must not narrow the result")
	ids := []int64{targets.Groups[0].ID, targets.Groups[1].ID}
	require.ElementsMatch(t, []int64{3, 5}, ids, "groups must be the unfiltered set")
}

// TestMyTargetsWithoutTargetsService 覆盖 Targets 未装配时的降级。
//
// 返回空集合而不是报错：前端据此隐藏「用户自测」入口，
// 而不是弹一个用户无法理解的错误。
func TestMyTargetsWithoutTargetsService(t *testing.T) {
	svc, _, _ := newUserTestFixture(t, userTestOn())
	svc.Targets = nil

	targets, err := svc.MyTargets(context.Background(), 42, 0)
	require.NoError(t, err)
	require.NotNil(t, targets)
	require.Empty(t, targets.Groups)
}

// TestMyTargetsRejectsInvalidUser 保证未登录身份不会拿到候选集合。
func TestMyTargetsRejectsInvalidUser(t *testing.T) {
	svc, _, _ := newUserTestFixture(t, userTestOn())

	_, err := svc.MyTargets(context.Background(), 0, 0)
	require.ErrorIs(t, err, iqbenchmark.ErrUserNotActive)
}
