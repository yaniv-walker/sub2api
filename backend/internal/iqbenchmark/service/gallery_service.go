package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// GalleryService 提供「作品展」的读写业务逻辑。
//
// 管理侧与公开侧共用一个服务，但**不共用 DTO**：
// 这里返回的是领域对象，字段可见性完全由 handler 的白名单 DTO 决定。
// 这样做的好处是同一份查询既能喂管理界面也能喂公开界面，
// 泄漏风险集中在一个 switch（toPublicArtifact）里，便于审计。
type GalleryService struct {
	Store  Store
	Logger *slog.Logger
	Now    func() time.Time
	// Targets 解析「分组 + 模型」候选集合并做目标二次校验。
	// 允许为 nil（既有单测可不构造），为 nil 时跳过目标校验。
	Targets *TargetsService
	// Settings 提供「可展示分组」白名单等配置。
	//
	// 允许为 nil：为 nil 时**公开侧看不到任何分组**（fail-closed）。
	// 这是刻意的默认 —— 白名单缺失时宁可变空，也不要把内部分组
	// 拓扑默认摊开。管理侧不受影响（它本来就要看全部）。
	Settings *SettingsProvider
}

// NewGalleryService 构造作品展服务。
//
// 不含目标解析器：用 WithTargets 单独注入，避免所有既有测试调用点都要改签名。
func NewGalleryService(store Store, logger *slog.Logger) *GalleryService {
	if logger == nil {
		logger = slog.Default()
	}
	return &GalleryService{Store: store, Logger: logger, Now: time.Now}
}

// WithSettings 注入配置提供者（返回自身，便于链式构造）。
func (s *GalleryService) WithSettings(settings *SettingsProvider) *GalleryService {
	if s != nil {
		s.Settings = settings
	}
	return s
}

// exposure 返回当前的可展示分组白名单快照。
//
// 单独抽出来是为了让「读配置」这件事只有一个写法：任何需要判断
// 「这个分组能不能公开」的地方都调它，而不是各自去读 s.Settings
// —— 后者迟早会在某处漏掉 nil 检查而 panic。
func (s *GalleryService) exposure(ctx context.Context) iqbenchmark.Settings {
	if s == nil || s.Settings == nil {
		// 未注入配置：返回零值 Settings，ExposeGroups 为空
		// ⇒ GroupExposed 恒 false，公开侧看不到任何分组。
		return iqbenchmark.Settings{}
	}
	return s.Settings.Get(ctx)
}

// WithTargets 注入目标解析器（返回自身，便于链式构造）。
//
// targets 为 nil 时保持「跳过目标校验」的行为，既有单测因此无需改动。
func (s *GalleryService) WithTargets(targets *TargetsService) *GalleryService {
	if s != nil {
		s.Targets = targets
	}
	return s
}

func (s *GalleryService) now() time.Time {
	if s == nil || s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

// ---------- 题目 ----------

// CreateCase 新建题目并做完整校验。
func (s *GalleryService) CreateCase(ctx context.Context, in CaseCreateInput) (*iqbenchmark.Case, error) {
	in.Name = trimSpace(in.Name)
	in.Prompt = trimSpace(in.Prompt)
	in.Expected = trimSpace(in.Expected)
	if err := validateCase(in.Name, in.Prompt, in.Kind, in.ExpectKind, in.Scoring,
		in.Expected, in.Builtin); err != nil {
		return nil, err
	}
	return s.Store.CreateCase(ctx, in)
}

// UpdateCase 全量更新题目。
func (s *GalleryService) UpdateCase(ctx context.Context, id int64, in CaseUpdateInput) (*iqbenchmark.Case, error) {
	in.Name = trimSpace(in.Name)
	in.Prompt = trimSpace(in.Prompt)
	in.Expected = trimSpace(in.Expected)
	// builtin 不可通过更新修改，所以这里要读现有值来判断
	// 「自定义题必须有 expected」这条规则是否适用。
	existing, err := s.Store.GetCase(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := validateCase(in.Name, in.Prompt, in.Kind, in.ExpectKind, in.Scoring,
		in.Expected, existing.Builtin); err != nil {
		return nil, err
	}
	return s.Store.UpdateCase(ctx, id, in)
}

// DeleteCase 删除题目。
//
// 三条保护，顺序不能变：
//  1. 内置题目不可删（它是基准题，删了整个对比就没有共同参照）；
//  2. 有启用任务引用时不可删（否则任务会静默少跑一题）；
//  3. 只被停用任务引用时，先从这些任务里摘掉该题，再删 ——
//     否则停用任务重新启用后会带着一个已删除的 case_id。
func (s *GalleryService) DeleteCase(ctx context.Context, id int64) error {
	enabledRefs, err := s.Store.CountEnabledTasksReferencingCase(ctx, id)
	if err != nil {
		return fmt.Errorf("iqbenchmark: count enabled task references: %w", err)
	}
	if enabledRefs > 0 {
		return iqbenchmark.ErrCaseInUse
	}
	allRefs, err := s.Store.CountTasksReferencingCase(ctx, id)
	if err != nil {
		return fmt.Errorf("iqbenchmark: count task references: %w", err)
	}
	if allRefs > 0 {
		if err := s.Store.RemoveCaseFromDisabledTasks(ctx, id); err != nil {
			return fmt.Errorf("iqbenchmark: remove case from disabled tasks: %w", err)
		}
	}
	return s.Store.DeleteCase(ctx, id)
}

// ListCases 列出题目（enabledOnly 供公开侧使用）。
func (s *GalleryService) ListCases(ctx context.Context, enabledOnly bool) ([]iqbenchmark.Case, error) {
	return s.Store.ListCases(ctx, enabledOnly)
}

// GetCase 读取单个题目。
func (s *GalleryService) GetCase(ctx context.Context, id int64) (*iqbenchmark.Case, error) {
	return s.Store.GetCase(ctx, id)
}

// validateCase 校验题目字段组合。
func validateCase(name, prompt, kind, expectKind, scoring, expected string, builtin bool) error {
	if name == "" {
		return ErrEmptyName
	}
	if prompt == "" {
		return ErrEmptyPrompt
	}
	if !iqbenchmark.ValidCaseKind(kind) {
		return ErrInvalidKind
	}
	if !iqbenchmark.ValidExpectKind(expectKind) {
		return ErrInvalidExpectKind
	}
	if !iqbenchmark.ValidScoring(scoring) {
		return ErrInvalidScoring
	}
	// 图片题必须期望图片产物；文本/代码题不能期望 image_url。
	// 允许的前提是两侧语义一致，否则执行期必然产出「永远失败」的题目。
	if iqbenchmark.IsImageCase(kind) && expectKind != iqbenchmark.ExpectKindImageURL {
		return ErrKindExpectMismatch
	}
	if !iqbenchmark.IsImageCase(kind) && expectKind == iqbenchmark.ExpectKindImageURL {
		return ErrKindExpectMismatch
	}
	// 自定义的文本/代码题必须有参考答案，否则这道题永远判不出 normal/degraded，
	// 只会一直落在 generated 上 —— 那等于把「没判」伪装成「不能判」。
	//
	// 内置题豁免：它们是随版本发布的基准题，允许暂时不带 expected
	// （管理员可以编辑补上）。图片题天然豁免（产出是图，没有可比的文本）。
	if !builtin && !iqbenchmark.IsImageCase(kind) && expected == "" {
		return ErrMissingExpected
	}
	return nil
}

// ---------- 任务 ----------

// CreateTask 新建任务。
func (s *GalleryService) CreateTask(ctx context.Context, in TaskCreateInput) (*iqbenchmark.Task, error) {
	if err := validateTask(in.Name, in.GroupID, in.Model, in.CaseIDs, in.IntervalMinutes, in.JitterSeconds); err != nil {
		return nil, err
	}
	in.Name = trimSpace(in.Name)
	in.Model = trimSpace(in.Model)
	// 目标二次校验：确认 (分组, 模型, 题目能力) 三元组在当前运行态下真的可用。
	// 前端下拉已经限制了可选范围，这里再校验一次是为了防止绕过前端直接调 API。
	if err := s.validateTarget(ctx, in.GroupID, in.Model, in.CaseIDs); err != nil {
		return nil, err
	}
	// 启用中的任务在创建时如果没给 next_run_at，立即排一次（不必等一个完整间隔）。
	if in.Enabled && in.NextRunAt == nil {
		next := s.now()
		in.NextRunAt = &next
	}
	task, err := s.Store.CreateTask(ctx, in)
	if err != nil {
		return nil, err
	}
	return task, nil
}

// UpdateTask 全量更新任务。
func (s *GalleryService) UpdateTask(ctx context.Context, id int64, in TaskUpdateInput) (*iqbenchmark.Task, error) {
	if err := validateTask(in.Name, in.GroupID, in.Model, in.CaseIDs, in.IntervalMinutes, in.JitterSeconds); err != nil {
		return nil, err
	}
	in.Name = trimSpace(in.Name)
	in.Model = trimSpace(in.Model)
	if err := s.validateTarget(ctx, in.GroupID, in.Model, in.CaseIDs); err != nil {
		return nil, err
	}
	return s.Store.UpdateTask(ctx, id, in)
}

// validateTarget 校验任务目标（分组 + 模型）在当前运行态下是否可用。
//
// 未注入 Targets 解析器时直接放行：这样既有单测与不关心目标的调用方无需构造它，
// 而生产链路（module.go）总会注入 —— 校验强度不会因为可选注入而静默降级，
// 因为 targets 为空只可能出现在测试装配里。
func (s *GalleryService) validateTarget(ctx context.Context, groupID int64, model string, caseIDs []int64) error {
	if s == nil || s.Targets == nil {
		return nil
	}
	group, err := s.Targets.ActiveGroup(ctx, groupID)
	if err != nil {
		return err
	}
	kinds, err := s.caseKinds(ctx, caseIDs)
	if err != nil {
		return err
	}
	return s.Targets.ValidateTarget(ctx, group, model, kinds)
}

// caseKinds 取出题目类型，用于能力匹配校验。
// 题目不存在时返回 ErrCaseNotFound（与既有题目校验口径一致）。
func (s *GalleryService) caseKinds(ctx context.Context, caseIDs []int64) ([]string, error) {
	kinds := make([]string, 0, len(caseIDs))
	for _, id := range caseIDs {
		c, err := s.Store.GetCase(ctx, id)
		if err != nil {
			return nil, err
		}
		if c == nil {
			return nil, iqbenchmark.ErrCaseNotFound
		}
		kinds = append(kinds, c.Kind)
	}
	return kinds, nil
}

// ListTasks 列出全部任务。
func (s *GalleryService) ListTasks(ctx context.Context) ([]iqbenchmark.Task, error) {
	return s.Store.ListTasks(ctx)
}

// GetTask 读取单个任务。
func (s *GalleryService) GetTask(ctx context.Context, id int64) (*iqbenchmark.Task, error) {
	return s.Store.GetTask(ctx, id)
}

// DeleteTask 删除任务（历史 run 与产出保留，用于回溯）。
func (s *GalleryService) DeleteTask(ctx context.Context, id int64) error {
	return s.Store.DeleteTask(ctx, id)
}

// SetTaskEnabled 启用/停用任务。
//
// 启用时若上次是被熔断挂起的（quota_exhausted 等），必须同时清掉熔断状态，
// 否则任务「已启用但不跑」，管理员会误以为功能坏了。
func (s *GalleryService) SetTaskEnabled(ctx context.Context, id int64, enabled bool) (*iqbenchmark.Task, error) {
	task, err := s.Store.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.Store.SetTaskEnabled(ctx, id, enabled); err != nil {
		return nil, err
	}
	if enabled {
		switch {
		// 这些状态都是「停下来等人工」。重新启用即视为人工恢复，回到 idle 重新排队。
		// invalid_target 也归入此类：重新启用后若目标仍然失效，调度器会再次熔断，
		// 不会造成空转（每次只尝试一次就停）。
		case iqbenchmark.TaskStatusNeedsManualRecovery(task.Status):
			next := s.now()
			if err := s.Store.SetTaskStatus(ctx, id, iqbenchmark.TaskStatusIdle, "", &next); err != nil {
				return nil, err
			}
		case task.Status == iqbenchmark.TaskStatusIdle:
			if task.NextRunAt == nil {
				next := s.now()
				if err := s.Store.SetTaskNextRunAt(ctx, id, next); err != nil {
					return nil, err
				}
			}
		}
	}
	return s.Store.GetTask(ctx, id)
}

// RunTaskNow 立即执行一次（管理员手动触发）。
//
// 不走调度器：手动执行不占用租约、不推进 next_run_at，
// 因此不会打乱既有的定时节奏。
func (s *GalleryService) RunTaskNow(ctx context.Context, runner *RunService, id int64, agent *iqbenchmark.InternalAgent) (*iqbenchmark.Run, error) {
	task, err := s.Store.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	return runner.RunTask(ctx, RunTaskInput{
		Task:    task,
		Agent:   agent,
		Trigger: iqbenchmark.RunTriggerManual,
	})
}

// validateTask 校验任务字段。
func validateTask(name string, groupID int64, model string, caseIDs []int64, interval, jitter int) error {
	if trimSpace(name) == "" {
		return ErrEmptyName
	}
	if groupID <= 0 {
		return iqbenchmark.ErrTaskGroupMissing
	}
	if trimSpace(model) == "" {
		return ErrEmptyPrompt
	}
	if len(caseIDs) == 0 {
		return iqbenchmark.ErrTaskNoCases
	}
	return validateInterval(interval, jitter)
}

// ---------- 执行记录 ----------

// ListRuns 分页列出某个任务的执行记录。
func (s *GalleryService) ListRuns(ctx context.Context, taskID int64, page, pageSize int) ([]iqbenchmark.Run, int64, error) {
	page, pageSize = normalizePaging(page, pageSize)
	return s.Store.ListRunsByTask(ctx, taskID, page, pageSize)
}

// ---------- 产出 ----------

// ArtifactListQuery 是作品列表的入参（HTTP query 的领域映射）。
//
// Source 是「统计口径」参数：默认 scheduled（只看定时检测）。
// 传 all 表示不过滤，传 user_test 只看用户自测。
// 归一化在 NormalizeSourceFilter 里做，非法值降级为默认口径而不是报错。
//
// ModeratedOnly 决定是否只返回审核通过的作品：公开侧为 true，
// 管理侧为 false（管理员要能看到待审的）。
type ArtifactListQuery struct {
	Model  string
	CaseID *int64
	Status string
	Kind   string
	Source string
	Since  *time.Time
	Until  *time.Time
	// ModeratedOnly 是**公开侧**口径：只展示审核通过的作品。
	ModeratedOnly bool
	// ModerationStatus 是**管理侧**口径：按审核状态筛选（如 pending = 审核队列）。
	//
	// 两个字段分开而不是复用一个，是因为语义不同：公开侧是「无论如何只给
	// 通过的」，管理侧是「我要看某个状态的」。合并会出现「管理侧设了
	// ModerationStatus=pending 却被 ModeratedOnly 强制改成 approved」
	// 这种静默失效。
	ModerationStatus string
	Page             int
	PageSize         int
}

// ListArtifacts 列出作品。
//
// page_size 超过 MaxPublicPageSize 时**直接报错**而不是静默截断：
// 静默截断会让调用方以为拿到了完整数据，分页计算随之出错。
func (s *GalleryService) ListArtifacts(ctx context.Context, q ArtifactListQuery) (*iqbenchmark.ArtifactListResult, error) {
	if q.PageSize > MaxPublicPageSize {
		return nil, iqbenchmark.ErrPageSizeTooLarge
	}
	page, pageSize := normalizePaging(q.Page, q.PageSize)
	filter := iqbenchmark.ArtifactListFilter{
		Model:    trimSpace(q.Model),
		CaseID:   q.CaseID,
		Status:   trimSpace(q.Status),
		Kind:     trimSpace(q.Kind),
		Source:   iqbenchmark.SourceFilterToRepo(q.Source),
		Since:    q.Since,
		Until:    q.Until,
		Page:     page,
		PageSize: pageSize,
	}
	if q.ModeratedOnly {
		filter.ModerationStatus = iqbenchmark.ModerationApproved
	} else if st := trimSpace(q.ModerationStatus); st != "" {
		// 管理侧的显式筛选。不校验取值合法性：非法值查不到任何行，
		// 这比返回 400 更符合「筛选器」的直觉，也不会泄露合法取值集合。
		filter.ModerationStatus = st
	}
	// 公开侧还要套一层「分组白名单」。
	//
	// 用 ModeratedOnly 作为「这是公开侧」的判据，与上面审核口径复用
	// 同一个信号源：分开判据会出现「审核按公开侧算、白名单按管理侧算」
	// 这种半公开状态，那正是最容易泄露的形态。
	if q.ModeratedOnly {
		exposed := s.exposure(ctx).ExposeGroupIDs()
		if len(exposed) == 0 {
			// 白名单为空 ⇒ 直接返回空页，不查库。
			// 这样既省一次查询，也保证「没配置就绝对看不到东西」
			// 不依赖仓储层是否记得处理空集合。
			return &iqbenchmark.ArtifactListResult{
				Items: []iqbenchmark.Artifact{}, Total: 0, Page: page, PageSize: pageSize,
			}, nil
		}
		filter.GroupIDs = exposed
	}
	return s.Store.ListArtifacts(ctx, filter)
}

// GetArtifact 读取单个作品。
func (s *GalleryService) GetArtifact(ctx context.Context, id int64) (*iqbenchmark.Artifact, error) {
	return s.Store.GetArtifact(ctx, id)
}

// ArtifactCompareItem 是对比视图的一列。
type ArtifactCompareItem struct {
	Artifact *iqbenchmark.Artifact
}

// Compare 按 ID 批量取作品，用于「同题多模型」对比。
//
// 顺序严格按传入 ids 返回（不按数据库顺序）：对比视图的列序必须稳定，
// 否则同一组 ID 每次刷新列位置都在跳。
func (s *GalleryService) Compare(ctx context.Context, ids []int64) ([]iqbenchmark.Artifact, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 8 {
		// 最多 8 列：再多前端无法有效并排阅读。
		ids = ids[:8]
	}
	out := make([]iqbenchmark.Artifact, 0, len(ids))
	for _, id := range ids {
		artifact, err := s.Store.GetArtifact(ctx, id)
		if err != nil {
			if errors.Is(err, iqbenchmark.ErrArtifactNotFound) {
				// 单个作品被清理不应让整次对比失败，跳过即可。
				continue
			}
			return nil, err
		}
		out = append(out, *artifact)
	}
	return out, nil
}

// ---------- 总览 ----------

// GetOverview 返回作品展总览。
//
// source 的归一化在服务层完成（fail-closed 到 scheduled），
// 因此即使调用方忘了传或传了脏值，统计口径也不会被放大成全量。
// 要全量必须显式传 SourceAll。
//
// 公开侧的分组白名单也会作用到这里：总览的分桶按「分组 × 模型」聚合，
// 不套白名单等于把被隐藏分组的模型名、数量、耗时全部泄出去 ——
// 列表藏起来了、统计却还在报数，是最典型的一类侧信道泄漏。
// 管理侧传 restrictToExposed=false 拿全量。
func (s *GalleryService) GetOverview(ctx context.Context, bucketLimit int, source string, restrictToExposed bool) (*iqbenchmark.Overview, error) {
	if bucketLimit <= 0 {
		bucketLimit = 50
	}
	var exposed []int64
	if restrictToExposed {
		exposed = s.exposure(ctx).ExposeGroupIDs()
		if len(exposed) == 0 {
			// 白名单为空：返回全零总览，不查库。
			// 注意这里仍然返回 CaseCount（题目数与分组无关），
			// 否则公开页会以为「连题都没有」而隐藏整个入口。
			counts, err := s.Store.GetOverviewCounts(ctx, iqbenchmark.SourceFilterToRepo(source), []int64{})
			if err != nil {
				return nil, err
			}
			return &iqbenchmark.Overview{
				CaseCount: counts.CaseCount,
				Buckets:   []iqbenchmark.OverviewBucket{},
			}, nil
		}
	}
	repoSource := iqbenchmark.SourceFilterToRepo(source)
	counts, err := s.Store.GetOverviewCounts(ctx, repoSource, exposed)
	if err != nil {
		return nil, err
	}
	rows, err := s.Store.GetOverviewBuckets(ctx, bucketLimit, repoSource, exposed)
	if err != nil {
		return nil, err
	}
	buckets := make([]iqbenchmark.OverviewBucket, 0, len(rows))
	for i := range rows {
		row := rows[i]
		buckets = append(buckets, iqbenchmark.OverviewBucket{
			GroupID:      row.GroupID,
			Model:        row.Model,
			Total:        row.Total,
			SuccessCount: row.SuccessCount,
			FailedCount:  row.FailedCount,
			AvgDurationM: row.AvgDurationM,
			LastRunAt:    row.LastRunAt,
		})
	}
	return &iqbenchmark.Overview{
		TotalArtifacts: counts.TotalArtifacts,
		ImageArtifacts: counts.ImageArtifacts,
		TextArtifacts:  counts.TextArtifacts,
		CaseCount:      counts.CaseCount,
		TaskCount:      counts.TaskCount,
		RunningTasks:   counts.RunningTasks,
		Buckets:        buckets,
	}, nil
}

// normalizePaging 归一化分页参数。
func normalizePaging(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = defaultListPageSize
	}
	if pageSize > MaxPublicPageSize {
		pageSize = MaxPublicPageSize
	}
	return page, pageSize
}

func trimSpace(s string) string { return strings.TrimSpace(s) }

// ---------- 监控矩阵 ----------

// MatrixMaxCells 是矩阵的硬上限（参考实现的常量）。
//
// 与 MaxPublicPageSize=50 是**两个不同的东西**，不要合并：
// 50 是分页列表的每页上限，60 是矩阵这个固定形状看板的格子预算。
// 把 50 套到矩阵上，会让「6 模型 × 10 题」这种最常见配置莫名少一行。
const MatrixMaxCells = 60

// MatrixQuery 是矩阵的筛选条件。
//
// 与 ArtifactListQuery 一样跟随 source 口径，但有意**不**接受分页：
// 矩阵是看板，分页没有意义（翻到第 2 页看到半个矩阵只会让人困惑）。
type MatrixQuery struct {
	Model  string
	CaseID *int64
	Status string
	Kind   string
	Source string
	Since  *time.Time
	Until  *time.Time
	// GroupID 限定单个分组（前端的分组墙点进来时用）。
	GroupID *int64
}

// MatrixCell 是矩阵中的一个单元格（领域层）。
type MatrixCell struct {
	CaseID     int64
	CaseName   string
	Model      string
	State      string
	ArtifactID int64
	Verdict    string
	ErrorKind  string
	CreatedAt  *time.Time
}

// MatrixAxisCase 是矩阵的行轴。
type MatrixAxisCase struct {
	ID   int64
	Name string
	Kind string
}

// Matrix 是矩阵结果。
type Matrix struct {
	Cases     []MatrixAxisCase
	Models    []string
	Cells     []MatrixCell
	Total     int
	Truncated bool
}

// 矩阵单元格的四态取值。
const (
	// MatrixStatePassed 表示判分通过。
	MatrixStatePassed = "passed"
	// MatrixStateIncorrect 表示调用成功但答错（模型变笨的直接信号）。
	MatrixStateIncorrect = "incorrect"
	// MatrixStateError 表示调用没跑通（网络/上游/超时/拦截）。
	MatrixStateError = "error"
	// MatrixStateEmpty 表示没有产出。
	MatrixStateEmpty = "empty"
)

// MatrixStateFor 把一条产出（或「没有产出」）折算成单元格状态。
//
// 这是矩阵唯一的判定入口，优先级刻意如此：
//  1. 先看 status —— 调用都没跑通时，verdict 说什么都不算数。
//     verdict 可能是 unknown（没判过），但 status=failed 的格子
//     必须显示成 error，否则用户会以为「只是没判定」而非「挂了」。
//  2. 再看 verdict —— ok 才算 passed，其余（degraded/generated/unknown）
//     一律 incorrect。这里对 generated 也判 incorrect 是有意的：
//     模型没按格式回答、直接自由发挥，就是没通过这道题。
//
// 没有产出（artifactID=0）返回 empty。
func MatrixStateFor(status, verdict string, artifactID int64) string {
	if artifactID <= 0 {
		return MatrixStateEmpty
	}
	if status != iqbenchmark.ArtifactStatusSuccess {
		return MatrixStateError
	}
	if verdict == iqbenchmark.VerdictOK {
		return MatrixStatePassed
	}
	return MatrixStateIncorrect
}

// BuildMatrix 从产出列表合成矩阵。
//
// 输入是「按 created_at DESC 排好序」的产出（仓储层就是这么返回的），
// 因此每个单元格取**第一次遇到**的那条即为最新。
//
// 坐标轴由数据本身决定而不是由配置决定：题目轴取出现过的 case，
// 模型轴取出现过的 model。这样矩阵永远与筛选结果自洽 ——
// 用配置生成轴会出现「轴上有一行但全是空格子」的观感问题。
func BuildMatrix(items []iqbenchmark.Artifact, maxCells int) *Matrix {
	if maxCells <= 0 {
		maxCells = MatrixMaxCells
	}

	caseOrder := make([]int64, 0, 8)
	caseMeta := make(map[int64]MatrixAxisCase, 8)
	modelOrder := make([]string, 0, 8)
	modelSeen := make(map[string]struct{}, 8)
	// 已占用的单元格：key = caseID + "\x00" + model。
	occupied := make(map[string]struct{}, len(items))
	cells := make([]MatrixCell, 0, len(items))
	// dropped 记录「因为触顶而没被放进矩阵」的格子数。
	//
	// 用它判截断而不是用 len(Cases)*len(Models) 反推：
	// 触顶 break 之后轴只会包含已处理的那几道题，
	// 用轴相乘会算出一个恰好等于上限的数，于是 100 格被截到 60 格
	// 这种情况反而报「没截断」。这个 bug 一犯，用户就会以为
	// 矩阵是完整的，少掉的模型根本不会被发现。
	dropped := 0

	for i := range items {
		it := items[i]
		if it.CaseID <= 0 || strings.TrimSpace(it.Model) == "" {
			continue
		}
		if _, seen := caseMeta[it.CaseID]; !seen {
			caseOrder = append(caseOrder, it.CaseID)
			name, kind := matrixCaseMeta(it.CaseSnapshot, it.CaseID)
			caseMeta[it.CaseID] = MatrixAxisCase{ID: it.CaseID, Name: name, Kind: kind}
		}
		if _, seen := modelSeen[it.Model]; !seen {
			modelSeen[it.Model] = struct{}{}
			modelOrder = append(modelOrder, it.Model)
		}

		// 同一格只保留最新一条（列表已按时间倒序）。
		key := matrixCellKey(it.CaseID, it.Model)
		if _, dup := occupied[key]; dup {
			continue
		}
		occupied[key] = struct{}{}

		if len(cells) >= maxCells {
			dropped++
			continue
		}

		created := it.CreatedAt
		cells = append(cells, MatrixCell{
			CaseID:     it.CaseID,
			CaseName:   caseMeta[it.CaseID].Name,
			Model:      it.Model,
			State:      MatrixStateFor(it.Status, it.Verdict, it.ID),
			ArtifactID: it.ID,
			Verdict:    it.Verdict,
			ErrorKind:  it.ErrorKind,
			CreatedAt:  &created,
		})
	}

	out := &Matrix{
		Cases:     make([]MatrixAxisCase, 0, len(caseOrder)),
		Models:    modelOrder,
		Cells:     cells,
		Total:     len(cells),
		Truncated: dropped > 0,
	}
	for _, id := range caseOrder {
		out.Cases = append(out.Cases, caseMeta[id])
	}
	return out
}

func matrixCellKey(caseID int64, model string) string {
	return strconv.FormatInt(caseID, 10) + "\x00" + model
}

// matrixCaseMeta 从快照里取题名与类型，缺失时给可辨识的兜底。
func matrixCaseMeta(snapshot map[string]any, caseID int64) (string, string) {
	name := ""
	kind := ""
	if snapshot != nil {
		if v, ok := snapshot["name"].(string); ok {
			name = strings.TrimSpace(v)
		}
		if v, ok := snapshot["kind"].(string); ok {
			kind = strings.TrimSpace(v)
		}
	}
	if name == "" {
		// 不给空名字：矩阵行轴上出现空白行会让人以为数据坏了。
		name = fmt.Sprintf("#%d", caseID)
	}
	return name, kind
}

// GetMatrix 拉取矩阵数据。
//
// 复用 ListArtifacts 的查询路径（含分组白名单与审核口径），
// 不另写一套 SQL —— 否则矩阵与列表会慢慢漂移成两个真相。
//
// 取数上限是 maxCells 的两倍：同一格可能有多个模型/多条历史，
// 多取一些才能在去重后仍填满矩阵；上限仍然存在，避免被恶意宽筛拖垮。
func (s *GalleryService) GetMatrix(ctx context.Context, q MatrixQuery) (*Matrix, error) {
	exposed := s.exposure(ctx).ExposeGroupIDs()
	if len(exposed) == 0 {
		// 白名单为空：空矩阵，不查库（与列表、总览同一口径）。
		return &Matrix{Cases: []MatrixAxisCase{}, Models: []string{}, Cells: []MatrixCell{}}, nil
	}

	groupIDs := exposed
	if q.GroupID != nil {
		// 请求点了某个分组：必须仍在白名单内，否则收紧不回退。
		if !containsInt64(exposed, *q.GroupID) {
			return &Matrix{Cases: []MatrixAxisCase{}, Models: []string{}, Cells: []MatrixCell{}}, nil
		}
		groupIDs = []int64{*q.GroupID}
	}

	result, err := s.Store.ListArtifacts(ctx, iqbenchmark.ArtifactListFilter{
		GroupIDs:         groupIDs,
		Model:            trimSpace(q.Model),
		CaseID:           q.CaseID,
		Status:           trimSpace(q.Status),
		Kind:             trimSpace(q.Kind),
		Source:           iqbenchmark.SourceFilterToRepo(q.Source),
		Since:            q.Since,
		Until:            q.Until,
		ModerationStatus: iqbenchmark.ModerationApproved,
		Page:             1,
		PageSize:         MatrixMaxCells * 2,
	})
	if err != nil {
		return nil, err
	}
	return BuildMatrix(result.Items, MatrixMaxCells), nil
}

func containsInt64(list []int64, want int64) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
