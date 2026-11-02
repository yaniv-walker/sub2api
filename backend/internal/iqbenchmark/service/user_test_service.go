package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	upstreamsvc "github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// 自测的固定策略（不开放给管理员配置，避免误配置放大风险）。
const (
	// UserTestDedupeWindow 是相同请求的去重窗口。
	//
	// 10 分钟的依据：一次自测的典型耗时是秒级到一分钟级，
	// 10 分钟足以覆盖「反复点按钮 / 前端重试」，又不至于让用户
	// 想重测一次都要等太久。
	UserTestDedupeWindow = 10 * time.Minute
	// UserTestListMaxPageSize 是自测历史的最大分页。
	UserTestListMaxPageSize = 50
)

// KeyResolver 是「用用户自己的本站 Key」所需的最小上游能力。
//
// 刻意定义成本模块的窄接口，而不是直接依赖 *upstreamsvc.APIKeyService：
// 单测可以注入假实现，也不必把上游 service 包的依赖面拉进本模块。
//
// 三个方法分别对应上游已验证存在的导出能力（无需改动上游）：
//   - VerifyOwnership：校验归属，且对「不存在」与「不属于你」返回同样的空结果，
//     天然避免 Key 存在性泄露；
//   - GetByID：取 Key 本体（含明文与分组）；
//   - CheckAPIKeyQuotaAndExpiry：校验过期与额度耗尽。
type KeyResolver interface {
	VerifyOwnership(ctx context.Context, userID int64, apiKeyIDs []int64) ([]int64, error)
	GetByID(ctx context.Context, id int64) (*upstreamsvc.APIKey, error)
	CheckAPIKeyQuotaAndExpiry(apiKey *upstreamsvc.APIKey) error
}

// UserTestService 编排「用户用自己本站 API Key 一键自测」。
//
// 它与 RunService 的职责边界很清楚：
//   - 本服务只做**请求侧**的事：校验、配额、并发、去重、审核状态、状态机；
//   - 真正的模型调用复用 Executor（与定时检测同一份代码）。
//
// 因此两条链路唯一的差异是 ExecutionIdentity 的构造，
// 这也是「用谁的 Key 就扣谁的额度」能天然成立的原因。
type UserTestService struct {
	Store    Store
	Executor *Executor
	Keys     KeyResolver
	Targets  *TargetsService
	Settings *SettingsProvider
	Limiter  UserTestLimiter
	Logger   *slog.Logger
	Now      func() time.Time
	// Async 决定提交后是否立刻在后台 goroutine 执行。
	// 生产为 true；单测置 false 以便同步断言。
	Async bool
}

// UserTestLimiter 是自测需要的**在途并发**能力（由 ratelimit 子包实现）。
//
// 只做并发槽位，不做日次数/日成本：那两项以数据库为准（见 checkQuota）。
// 刻意不让 Redis 再数一遍 —— 两个计数器一旦漂移，用户在「我的自测历史」
// 里看到的条数就会和系统拒他的理由对不上，而这类不一致无法向用户解释。
//
// 并发槽位必须走原子计数，是因为「先查后写」在这件事上有真实的 TOCTOU：
// 两个请求同时读到 InFlightCount=0，都会通过，于是同时发出两次真实调用。
type UserTestLimiter interface {
	// AcquireUserTestSlot 占用一个在途槽位，返回释放函数。
	// 第二个返回值为 false 表示已达并发上限（调用方应返回 ErrUserTestConcurrent）。
	AcquireUserTestSlot(ctx context.Context, userID int64, maxConcurrent int) (func(), bool)
}

// NewUserTestService 构造自测服务。
func NewUserTestService(
	store Store,
	executor *Executor,
	keys KeyResolver,
	targets *TargetsService,
	settings *SettingsProvider,
	limiter UserTestLimiter,
	logger *slog.Logger,
) *UserTestService {
	if logger == nil {
		logger = slog.Default()
	}
	return &UserTestService{
		Store:    store,
		Executor: executor,
		Keys:     keys,
		Targets:  targets,
		Settings: settings,
		Limiter:  limiter,
		Logger:   logger,
		Now:      time.Now,
		Async:    true,
	}
}

func (s *UserTestService) now() time.Time {
	if s == nil || s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func (s *UserTestService) settings(ctx context.Context) Settings {
	if s.Settings == nil {
		return DefaultSettings()
	}
	return s.Settings.Get(ctx)
}

// Submit 提交一次自测。
//
// 返回的 *iqbenchmark.UserTest 处于 pending 或已被同步跑完的终态。
// 所有校验都在**写入任何记录之前**完成 —— 否则被拒的请求会污染
// 日配额计数，用户会被自己的失败尝试锁住。
//
// 校验顺序刻意是「越便宜越靠前」：开关 → 题目 → 用户 → Key → 目标 →
// 配额。（内容审核在 handler 层、拿到 gin.Context 时同步完成。）
func (s *UserTestService) Submit(ctx context.Context, in iqbenchmark.UserTestSubmitInput) (*iqbenchmark.UserTest, error) {
	settings := s.settings(ctx)
	if !settings.Enabled || !settings.UserTestEnabled {
		// 两个开关分开判断但返回同一个错误：对调用方来说都是「没开」，
		// 区分只会泄露功能部署状态。
		return nil, iqbenchmark.ErrUserTestDisabled
	}
	if in.UserID <= 0 {
		return nil, iqbenchmark.ErrUserNotActive
	}

	// 1) 题目必须存在且开放自测。
	detected, err := s.Store.GetCase(ctx, in.CaseID)
	if err != nil {
		return nil, err
	}
	if !detected.Enabled || !detected.AllowUserTest {
		return nil, iqbenchmark.ErrCaseNotOpenForUserTest
	}

	// 2) 分组与模型必须是真实可用的目标（复用 A 的 resolver，不另写一套）。
	if s.Targets == nil {
		return nil, iqbenchmark.ErrTargetGroupUnavailable
	}
	group, err := s.Targets.ActiveGroup(ctx, in.GroupID)
	if err != nil {
		return nil, err
	}
	// 图片题要求分组开放图片生成，否则网关必拒 —— 先在本地挡掉，
	// 避免把用户的额度浪费在注定失败的调用上。
	if iqbenchmark.IsImageCase(detected.Kind) && !group.AllowImageGeneration {
		return nil, iqbenchmark.ErrGroupImageGenerationDisabled
	}
	if err := s.Targets.ValidateTarget(ctx, group, in.Model, []string{detected.Kind}); err != nil {
		return nil, err
	}

	// 3) Key 归属与状态（不泄露存在性）。
	if s.Keys == nil {
		return nil, iqbenchmark.ErrAPIKeyNotOwned
	}
	owned, err := s.Keys.VerifyOwnership(ctx, in.UserID, []int64{in.APIKeyID})
	if err != nil {
		return nil, fmt.Errorf("iqbenchmark: verify key ownership: %w", err)
	}
	if len(owned) == 0 {
		// 刻意与「不存在」返回同一个错误码。
		return nil, iqbenchmark.ErrAPIKeyNotOwned
	}
	apiKey, err := s.Keys.GetByID(ctx, in.APIKeyID)
	if err != nil {
		return nil, iqbenchmark.ErrAPIKeyNotOwned
	}
	if !apiKey.IsActive() {
		return nil, iqbenchmark.ErrAPIKeyInactive
	}
	if err := s.Keys.CheckAPIKeyQuotaAndExpiry(apiKey); err != nil {
		// 上游把过期与额度耗尽区分开了；这里映射成我们自己的哨兵，
		// 让 handler 能给出准确的 HTTP 状态（403 / 402）。
		return nil, mapKeyCheckError(err)
	}
	// 明文 Key 只在这里读一次，之后仅存在于内存中的 identity 里。
	gatewayKey := apiKey.Key
	if strings.TrimSpace(gatewayKey) == "" {
		return nil, iqbenchmark.ErrAPIKeyNotOwned
	}

	// 4) 配额三连：日次数 / 在途数 / 日成本预算。
	if err := s.checkQuota(ctx, in.UserID, settings, in.EstimatedCost); err != nil {
		return nil, err
	}

	// 5) 占一个**原子**在途槽位。
	//
	// checkQuota 里的在途判断是「读快照」，两个并发请求可能同时读到 0 而都通过。
	// 槽位用 Redis 原子计数补上这个洞。拿不到就按并发超限拒绝。
	release, ok := s.acquireSlot(ctx, in.UserID, settings.UserTestMaxConcurrent)
	if !ok {
		return nil, iqbenchmark.ErrUserTestConcurrent
	}
	// 释放责任转移：只要成功落记录，槽位就交由执行侧释放；
	// 本次调用返回前失败则必须在这里释放，否则会永久占住一格。
	slotReleased := false
	defer func() {
		if !slotReleased && release != nil {
			release()
		}
	}()

	// 6) 去重：窗口内已有相同的未失败请求则直接拒绝。
	dedupeKey := userTestDedupeKey(in.UserID, in.CaseID, in.Model)
	dupes, err := s.Store.CountUserTestDedupe(ctx, dedupeKey, s.now().Add(-UserTestDedupeWindow))
	if err != nil {
		return nil, err
	}
	if dupes > 0 {
		return nil, iqbenchmark.ErrUserTestDuplicate
	}

	// 7) 落记录（此刻起才计入配额）。
	record, err := s.Store.CreateUserTest(ctx, iqbenchmark.UserTestCreateInput{
		UserID:        in.UserID,
		APIKeyID:      in.APIKeyID,
		CaseID:        in.CaseID,
		GroupID:       in.GroupID,
		Model:         in.Model,
		EstimatedCost: in.EstimatedCost,
		DedupeKey:     dedupeKey,
	})
	if err != nil {
		return nil, fmt.Errorf("iqbenchmark: create user test: %w", err)
	}

	// 8) 执行。identity 与定时链路唯一不同的地方就在这几行。
	execIn := ExecuteInput{
		// 自测没有 run/task：两个 0 会被仓库层写成 NULL。
		RunID:  0,
		TaskID: 0,
		Identity: ExecutionIdentity{
			GatewayKey:        gatewayKey,
			UsageAPIKeyID:     in.APIKeyID,
			TriggeredByUserID: in.UserID,
			Source:            iqbenchmark.SourceUserTest,
		},
		Case:    detected,
		GroupID: in.GroupID,
		Model:   in.Model,
	}

	// 从这里开始槽位归执行侧释放；上面的 defer 不再重复释放。
	slotReleased = true

	if s.Async {
		// 异步执行：请求立刻返回 pending，前端轮询结果。
		go func() {
			defer releaseSlot(release)
			s.runDetached(context.WithoutCancel(ctx), record.ID, execIn, in.RequireReview)
		}()
		return record, nil
	}
	defer releaseSlot(release)
	return s.runSync(ctx, record.ID, execIn, in.RequireReview)
}

// acquireSlot 占用一个在途槽位。
//
// Limiter 为 nil 或槽位能力不可用时**放行**（返回空释放函数）：
// 并发保护是附加防线，不是正确性前提；而 checkQuota 已经用数据库快照
// 做过一次判断。宁可少一层保护，也不要让自测在所有部署里都不可用。
func (s *UserTestService) acquireSlot(ctx context.Context, userID int64, maxConcurrent int) (func(), bool) {
	if s.Limiter == nil || maxConcurrent <= 0 {
		return nil, true
	}
	return s.Limiter.AcquireUserTestSlot(ctx, userID, maxConcurrent)
}

// releaseSlot 安全释放槽位（nil 与 panic 都不应影响主流程）。
func releaseSlot(release func()) {
	if release == nil {
		return
	}
	defer func() { _ = recover() }()
	release()
}

// runSync 同步执行（仅单测与关闭异步时使用）。
func (s *UserTestService) runSync(ctx context.Context, id int64, in ExecuteInput, requireReview bool) (*iqbenchmark.UserTest, error) {
	s.execute(ctx, id, in, requireReview)
	return s.Store.GetUserTest(ctx, id)
}

// runDetached 在后台执行一次自测并落终态。
//
// 用 context.WithoutCancel：HTTP 请求上下文在响应写完后立刻取消，
// 若直接沿用会把正在进行的模型调用一起取消，导致「用户点了、钱扣了、
// 产出却没写」—— 这是最糟糕的一种失败。
//
// 仍保留 deadline 兜底：模型调用本身有超时（settings.ModelTimeoutSeconds），
// 由 Executor 的 GatewayClient 强制执行。
func (s *UserTestService) runDetached(ctx context.Context, id int64, in ExecuteInput, requireReview bool) {
	defer func() {
		// 后台 goroutine 里的 panic 会直接打挂整个进程，必须兜住。
		if rec := recover(); rec != nil {
			s.Logger.Error("iqbenchmark: user test panicked",
				"user_test_id", id, "panic", rec)
			s.finishWithError(context.WithoutCancel(ctx), id,
				iqbenchmark.UserTestStatusFailed, iqbenchmark.ErrCodeUpstreamError,
				"internal error while running self-test")
		}
	}()
	s.execute(ctx, id, in, requireReview)
}

// execute 执行并落终态。
func (s *UserTestService) execute(ctx context.Context, id int64, in ExecuteInput, requireReview bool) {
	if s.Executor == nil {
		s.finishWithError(ctx, id, iqbenchmark.UserTestStatusFailed,
			iqbenchmark.ErrCodeUpstreamError, "self-test executor is not configured")
		return
	}
	if err := s.Store.FinishUserTest(ctx, id, iqbenchmark.UserTestStatusRunning, "", "", 0, 0); err != nil {
		// 状态推进失败不阻断执行：终态写入时会再试一次。
		s.Logger.Warn("iqbenchmark: mark user test running failed", "user_test_id", id, "error", err)
	}

	result, err := s.Executor.Execute(ctx, in, requireReview)
	if err != nil {
		s.finishWithError(ctx, id, iqbenchmark.UserTestStatusFailed,
			iqbenchmark.ErrCodeUpstreamError, err.Error())
		return
	}

	status := iqbenchmark.UserTestStatusSuccess
	if result.Status != iqbenchmark.ArtifactStatusSuccess {
		// 产出失败/超时/被拦截：自测本身也算「跑完了」，但不是 success。
		status = iqbenchmark.UserTestStatusFailed
	}
	artifactID := int64(0)
	if result.ArtifactID > 0 {
		artifactID = result.ArtifactID
	}
	if err := s.Store.FinishUserTest(ctx, id, status, result.ErrorCode, "", artifactID, 0); err != nil {
		s.Logger.Error("iqbenchmark: finish user test failed",
			"user_test_id", id, "status", status, "error", err)
	}
}

// finishWithError 落一条带错误摘要的终态。
func (s *UserTestService) finishWithError(ctx context.Context, id int64, status, code, summary string) {
	if err := s.Store.FinishUserTest(ctx, id, status, code, summary, 0, 0); err != nil {
		s.Logger.Error("iqbenchmark: finish user test with error failed",
			"user_test_id", id, "code", code, "error", err)
	}
}

// checkQuota 做三道配额预检。
//
// 顺序：日次数（最便宜，纯计数）→ 在途数 → 日成本预算。
// 成本预算放最后是因为它需要浮点累加，且 0 表示不限制时可以直接跳过。
func (s *UserTestService) checkQuota(ctx context.Context, userID int64, settings Settings, estimatedCost float64) error {
	dayStart := startOfDay(s.now())

	snapshot, err := s.Store.UserTestQuota(ctx, userID, dayStart)
	if err != nil {
		return fmt.Errorf("iqbenchmark: read user test quota: %w", err)
	}
	if settings.UserTestDailyLimit > 0 && snapshot.TodayCount >= settings.UserTestDailyLimit {
		return iqbenchmark.ErrUserTestDailyLimit
	}
	if settings.UserTestMaxConcurrent > 0 && snapshot.InFlightCount >= settings.UserTestMaxConcurrent {
		return iqbenchmark.ErrUserTestConcurrent
	}
	// 0 表示不限制（用户裁决）：此时不做任何成本判断。
	if settings.UserTestDailyCostBudget > 0 {
		// 用「已发生 + 本次预估」与预算比较：预估必须计入，
		// 否则用户可以靠连续提交把实际支出推到远高于预算。
		if snapshot.TodayCost+estimatedCost > settings.UserTestDailyCostBudget {
			return iqbenchmark.ErrUserTestBudgetExceeded
		}
	}
	return nil
}

// List 读取当前用户的自测历史。
func (s *UserTestService) List(ctx context.Context, userID int64, page, pageSize int) ([]iqbenchmark.UserTest, int64, error) {
	if pageSize > UserTestListMaxPageSize {
		pageSize = UserTestListMaxPageSize
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	return s.Store.ListUserTestsByUser(ctx, userID, page, pageSize)
}

// Get 读取单条自测，且强制校验归属 —— 不能靠前端只传自己的 ID。
func (s *UserTestService) Get(ctx context.Context, userID, id int64) (*iqbenchmark.UserTest, error) {
	record, err := s.Store.GetUserTest(ctx, id)
	if err != nil {
		return nil, err
	}
	if record.UserID != userID {
		// 与「不存在」返回同样的错误，避免枚举他人记录。
		return nil, iqbenchmark.ErrUserTestNotFound
	}
	return record, nil
}

// userTestDedupeKey 生成去重键。
//
// 用 SHA-256 而不是明文拼接：避免把 user_id/case_id 以可读形式
// 散落到索引与日志里（键会出现在慢查询日志中）。
func userTestDedupeKey(userID, caseID int64, model string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", userID, caseID, model)))
	return hex.EncodeToString(sum[:])
}

// startOfDay 取本地时区的当日零点。
//
// 用本地时区而不是 UTC：用户理解的「今天」是自己所在时区的今天，
// 用 UTC 会让东八区用户的配额在早上 8 点重置，明显不符合直觉。
func startOfDay(t time.Time) time.Time {
	local := t.Local()
	y, m, d := local.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, local.Location())
}

// mapKeyCheckError 把上游的 Key 校验错误映射为本模块的哨兵。
//
// 上游把「过期」与「额度耗尽」区分为不同错误；这里用文案匹配而不是
// errors.Is，因为上游没有导出对应的哨兵值。匹配失败时一律按「Key 不可用」
// 处理：宁可拒绝，也不要带着可疑的 Key 去打网关 —— 那会以用户的名义产生费用。
func mapKeyCheckError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "expired"):
		return iqbenchmark.ErrAPIKeyExpired
	case strings.Contains(msg, "quota"):
		return iqbenchmark.ErrAPIKeyQuotaExhausted
	default:
		return iqbenchmark.ErrAPIKeyInactive
	}
}
