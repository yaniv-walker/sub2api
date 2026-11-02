package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
}

// NewGalleryService 构造作品展服务。
func NewGalleryService(store Store, logger *slog.Logger) *GalleryService {
	if logger == nil {
		logger = slog.Default()
	}
	return &GalleryService{Store: store, Logger: logger, Now: time.Now}
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
	if err := validateCase(in.Name, in.Prompt, in.Kind, in.ExpectKind, in.Scoring); err != nil {
		return nil, err
	}
	return s.Store.CreateCase(ctx, in)
}

// UpdateCase 全量更新题目。
func (s *GalleryService) UpdateCase(ctx context.Context, id int64, in CaseUpdateInput) (*iqbenchmark.Case, error) {
	in.Name = trimSpace(in.Name)
	in.Prompt = trimSpace(in.Prompt)
	if err := validateCase(in.Name, in.Prompt, in.Kind, in.ExpectKind, in.Scoring); err != nil {
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
func validateCase(name, prompt, kind, expectKind, scoring string) error {
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
	return s.Store.UpdateTask(ctx, id, in)
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
		switch task.Status {
		case iqbenchmark.TaskStatusQuotaExhausted,
			iqbenchmark.TaskStatusBudgetExhausted,
			iqbenchmark.TaskStatusAccountDisabled,
			iqbenchmark.TaskStatusPausedByAdmin:
			next := s.now()
			if err := s.Store.SetTaskStatus(ctx, id, iqbenchmark.TaskStatusIdle, "", &next); err != nil {
				return nil, err
			}
		case iqbenchmark.TaskStatusIdle:
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
type ArtifactListQuery struct {
	Model    string
	CaseID   *int64
	Status   string
	Kind     string
	Since    *time.Time
	Until    *time.Time
	Page     int
	PageSize int
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
		Since:    q.Since,
		Until:    q.Until,
		Page:     page,
		PageSize: pageSize,
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
func (s *GalleryService) GetOverview(ctx context.Context, bucketLimit int) (*iqbenchmark.Overview, error) {
	if bucketLimit <= 0 {
		bucketLimit = 50
	}
	counts, err := s.Store.GetOverviewCounts(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Store.GetOverviewBuckets(ctx, bucketLimit)
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
