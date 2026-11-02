// Package repository 实现 iqbenchmark 的数据访问层。
//
// 设计取舍：
//   - 单行 CRUD 走 Ent（复用项目的事务上下文与拦截器）；
//   - 并发敏感的领取与多维聚合走原生 SQL，确保 FOR UPDATE SKIP LOCKED
//     与部分索引能真正被命中，避免 Ent 模板代码挡住索引选择。
//
// 本包只被 internal/iqbenchmark 内部引用，不进入 internal/service 或
// internal/handler，因此不触发 depguard 的 service↛repository 约束。
package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/iqbenchmarkartifact"
	"github.com/Wei-Shaw/sub2api/ent/iqbenchmarkcase"
	"github.com/Wei-Shaw/sub2api/ent/iqbenchmarkrun"
	"github.com/Wei-Shaw/sub2api/ent/iqbenchmarktask"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/lib/pq"
)

// Repository 汇总本模块的全部数据访问能力。
type Repository struct {
	client *dbent.Client
	db     *sql.DB
}

// New 构造仓储。client 用于单行 CRUD，db 用于原生 SQL（租约、聚合）。
func New(client *dbent.Client, db *sql.DB) *Repository {
	return &Repository{client: client, db: db}
}

func (r *Repository) client0() *dbent.Client {
	return r.client
}

// isTaskDuplicateEnabled 判断是否命中了部分唯一索引（同组合仅一个启用中任务）。
// 只把目标索引的冲突翻译成业务错误，其他唯一冲突保持原样。
func isTaskDuplicateEnabled(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return string(pqErr.Code) == "23505" && pqErr.Constraint == "uq_iq_task_group_model_enabled"
	}
	return false
}

// ---------- Case ----------

// CaseCreateInput、CaseUpdateInput 等入参统一由主包定义，
// 这里只做别名，确保仓储与 service 两侧不会出现字段漂移。
type (
	CaseCreateInput     = iqbenchmark.CaseCreateInput
	CaseUpdateInput     = iqbenchmark.CaseUpdateInput
	TaskCreateInput     = iqbenchmark.TaskCreateInput
	TaskUpdateInput     = iqbenchmark.TaskUpdateInput
	RunCreateInput      = iqbenchmark.RunCreateInput
	RunCounts           = iqbenchmark.RunCounts
	ArtifactCreateInput = iqbenchmark.ArtifactCreateInput
	PurgedArtifact      = iqbenchmark.PurgedArtifact
	OverviewCounts      = iqbenchmark.OverviewCounts
	OverviewBucketRow   = iqbenchmark.OverviewBucketRow
)

// CreateCase 新建题目。
func (r *Repository) CreateCase(ctx context.Context, in CaseCreateInput) (*iqbenchmark.Case, error) {
	params := in.Params
	if params == nil {
		params = map[string]any{}
	}
	builder := r.client0().IQBenchmarkCase.Create().
		SetName(in.Name).
		SetKind(in.Kind).
		SetPrompt(in.Prompt).
		SetParams(params).
		SetExpectKind(in.ExpectKind).
		SetScoring(defaultString(in.Scoring, iqbenchmark.ScoringManual)).
		SetLlmJudgeEnabled(in.LLMJudgeEnabled).
		SetBuiltin(in.Builtin).
		SetEnabled(in.Enabled).
		SetSortOrder(in.SortOrder)
	if strings.TrimSpace(in.SystemPrompt) != "" {
		builder = builder.SetSystemPrompt(in.SystemPrompt)
	}
	created, err := builder.Save(ctx)
	if err != nil {
		return nil, err
	}
	return caseFromEntity(created), nil
}

// UpdateCase 全量更新题目。内置题的 builtin 标记不可改（保持库中值）。
func (r *Repository) UpdateCase(ctx context.Context, id int64, in CaseUpdateInput) (*iqbenchmark.Case, error) {
	params := in.Params
	if params == nil {
		params = map[string]any{}
	}
	builder := r.client0().IQBenchmarkCase.UpdateOneID(id).
		SetName(in.Name).
		SetKind(in.Kind).
		SetPrompt(in.Prompt).
		SetParams(params).
		SetExpectKind(in.ExpectKind).
		SetScoring(defaultString(in.Scoring, iqbenchmark.ScoringManual)).
		SetLlmJudgeEnabled(in.LLMJudgeEnabled).
		SetEnabled(in.Enabled).
		SetSortOrder(in.SortOrder)
	if strings.TrimSpace(in.SystemPrompt) == "" {
		builder = builder.ClearSystemPrompt()
	} else {
		builder = builder.SetSystemPrompt(in.SystemPrompt)
	}
	updated, err := builder.Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, iqbenchmark.ErrCaseNotFound
		}
		return nil, err
	}
	return caseFromEntity(updated), nil
}

// GetCase 按 ID 读取题目。
func (r *Repository) GetCase(ctx context.Context, id int64) (*iqbenchmark.Case, error) {
	entity, err := r.client0().IQBenchmarkCase.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, iqbenchmark.ErrCaseNotFound
		}
		return nil, err
	}
	return caseFromEntity(entity), nil
}

// ListCases 按 sort_order、id 升序读取题目（可选仅启用）。
func (r *Repository) ListCases(ctx context.Context, enabledOnly bool) ([]iqbenchmark.Case, error) {
	query := r.client0().IQBenchmarkCase.Query()
	if enabledOnly {
		query = query.Where(iqbenchmarkcase.EnabledEQ(true))
	}
	entities, err := query.Order(dbent.Asc(iqbenchmarkcase.FieldSortOrder), dbent.Asc(iqbenchmarkcase.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]iqbenchmark.Case, 0, len(entities))
	for _, entity := range entities {
		out = append(out, *caseFromEntity(entity))
	}
	return out, nil
}

// ListCasesByIDs 按给定 ID 集合读取题目，返回以 ID 为键的映射。
// 缺失的 ID 不会出现在映射中（调用方据此跳过已失效题目）。
func (r *Repository) ListCasesByIDs(ctx context.Context, ids []int64) (map[int64]iqbenchmark.Case, error) {
	out := make(map[int64]iqbenchmark.Case, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	entities, err := r.client0().IQBenchmarkCase.Query().
		Where(iqbenchmarkcase.IDIn(ids...)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, entity := range entities {
		out[entity.ID] = *caseFromEntity(entity)
	}
	return out, nil
}

// DeleteCase 删除题目。内置题由 SQL 层直接兜底拒绝（WHERE builtin = false），
// 避免 service 层判断遗漏时误删。
func (r *Repository) DeleteCase(ctx context.Context, id int64) error {
	affected, err := r.client0().IQBenchmarkCase.Delete().
		Where(iqbenchmarkcase.ID(id), iqbenchmarkcase.BuiltinEQ(false)).
		Exec(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		// 区分「不存在」与「内置题不可删」，便于上层给出准确错误码。
		exists, err := r.client0().IQBenchmarkCase.Query().Where(iqbenchmarkcase.ID(id)).Exist(ctx)
		if err != nil {
			return err
		}
		if exists {
			return iqbenchmark.ErrCaseBuiltinUndeletable
		}
		return iqbenchmark.ErrCaseNotFound
	}
	return nil
}

// CountEnabledTasksReferencingCase 统计引用该题目的启用中任务数。
func (r *Repository) CountEnabledTasksReferencingCase(ctx context.Context, caseID int64) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM iq_benchmark_tasks
		WHERE enabled = TRUE AND case_ids @> to_jsonb(ARRAY[$1]::bigint[])
	`, caseID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// RemoveCaseFromDisabledTasks 从所有禁用任务的 case_ids 中剔除该题目。
func (r *Repository) RemoveCaseFromDisabledTasks(ctx context.Context, caseID int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE iq_benchmark_tasks
		SET case_ids = COALESCE((
			SELECT jsonb_agg(value)
			FROM jsonb_array_elements(case_ids) AS value
			WHERE value <> to_jsonb($1::bigint)
		), '[]'::jsonb)
		WHERE enabled = FALSE AND case_ids @> to_jsonb(ARRAY[$1]::bigint[])
	`, caseID)
	return err
}

// CountTasksReferencingCase 统计引用该题目的任务总数（含禁用），供复制/删除提示。
func (r *Repository) CountTasksReferencingCase(ctx context.Context, caseID int64) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM iq_benchmark_tasks
		WHERE case_ids @> to_jsonb(ARRAY[$1]::bigint[])
	`, caseID).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ---------- Task ----------

// CreateTask 新建任务。同组合已有启用中任务时返回 ErrTaskDuplicateEnabled。
func (r *Repository) CreateTask(ctx context.Context, in TaskCreateInput) (*iqbenchmark.Task, error) {
	caseIDs := in.CaseIDs
	if caseIDs == nil {
		caseIDs = []int64{}
	}
	builder := r.client0().IQBenchmarkTask.Create().
		SetName(in.Name).
		SetGroupID(in.GroupID).
		SetModel(in.Model).
		SetCaseIds(caseIDs).
		SetIntervalMinutes(in.IntervalMinutes).
		SetJitterSeconds(in.JitterSeconds).
		SetEnabled(in.Enabled)
	if in.NextRunAt != nil {
		builder = builder.SetNextRunAt(*in.NextRunAt)
	}
	created, err := builder.Save(ctx)
	if err != nil {
		if isTaskDuplicateEnabled(err) {
			return nil, iqbenchmark.ErrTaskDuplicateEnabled
		}
		return nil, err
	}
	return taskFromEntity(created), nil
}

// UpdateTask 全量更新任务业务字段。不改 status 与租约字段。
func (r *Repository) UpdateTask(ctx context.Context, id int64, in TaskUpdateInput) (*iqbenchmark.Task, error) {
	caseIDs := in.CaseIDs
	if caseIDs == nil {
		caseIDs = []int64{}
	}
	builder := r.client0().IQBenchmarkTask.UpdateOneID(id).
		SetName(in.Name).
		SetGroupID(in.GroupID).
		SetModel(in.Model).
		SetCaseIds(caseIDs).
		SetIntervalMinutes(in.IntervalMinutes).
		SetJitterSeconds(in.JitterSeconds).
		SetEnabled(in.Enabled)
	if in.NextRunAt == nil {
		builder = builder.ClearNextRunAt()
	} else {
		builder = builder.SetNextRunAt(*in.NextRunAt)
	}
	updated, err := builder.Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, iqbenchmark.ErrTaskNotFound
		}
		if isTaskDuplicateEnabled(err) {
			return nil, iqbenchmark.ErrTaskDuplicateEnabled
		}
		return nil, err
	}
	return taskFromEntity(updated), nil
}

// GetTask 按 ID 读取任务。
func (r *Repository) GetTask(ctx context.Context, id int64) (*iqbenchmark.Task, error) {
	entity, err := r.client0().IQBenchmarkTask.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, iqbenchmark.ErrTaskNotFound
		}
		return nil, err
	}
	return taskFromEntity(entity), nil
}

// ListTasks 读取全部任务（按 ID 升序）。
func (r *Repository) ListTasks(ctx context.Context) ([]iqbenchmark.Task, error) {
	entities, err := r.client0().IQBenchmarkTask.Query().
		Order(dbent.Asc(iqbenchmarktask.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]iqbenchmark.Task, 0, len(entities))
	for _, entity := range entities {
		out = append(out, *taskFromEntity(entity))
	}
	return out, nil
}

// DeleteTask 删除任务。历史 run 与产出保留（它们不依赖任务行存在）。
func (r *Repository) DeleteTask(ctx context.Context, id int64) error {
	affected, err := r.client0().IQBenchmarkTask.Delete().Where(iqbenchmarktask.ID(id)).Exec(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return iqbenchmark.ErrTaskNotFound
	}
	return nil
}

// SetTaskEnabled 启用/禁用任务。禁用时同时清空下次执行时间与租约。
func (r *Repository) SetTaskEnabled(ctx context.Context, id int64, enabled bool) error {
	builder := r.client0().IQBenchmarkTask.UpdateOneID(id).SetEnabled(enabled)
	if !enabled {
		builder = builder.
			ClearNextRunAt().
			ClearLeaseOwner().
			ClearLeaseExpiresAt()
	}
	if _, err := builder.Save(ctx); err != nil {
		if dbent.IsNotFound(err) {
			return iqbenchmark.ErrTaskNotFound
		}
		if isTaskDuplicateEnabled(err) {
			return iqbenchmark.ErrTaskDuplicateEnabled
		}
		return err
	}
	return nil
}

// SetTaskStatus 更新任务运行状态与原因；nextRunAt 为 nil 表示停止调度。
func (r *Repository) SetTaskStatus(ctx context.Context, id int64, status, reason string, nextRunAt *time.Time) error {
	builder := r.client0().IQBenchmarkTask.UpdateOneID(id).
		SetStatus(status).
		SetNillableStatusReason(nillableString(reason))
	if nextRunAt == nil {
		builder = builder.ClearNextRunAt()
	} else {
		builder = builder.SetNextRunAt(*nextRunAt)
	}
	if _, err := builder.Save(ctx); err != nil {
		if dbent.IsNotFound(err) {
			return iqbenchmark.ErrTaskNotFound
		}
		return err
	}
	return nil
}

// SetTaskNextRunAt 仅推进下次执行时间。
func (r *Repository) SetTaskNextRunAt(ctx context.Context, id int64, next time.Time) error {
	_, err := r.client0().IQBenchmarkTask.UpdateOneID(id).SetNextRunAt(next).Save(ctx)
	return err
}

// MarkTaskRunFinished 记录一次执行完成：更新 last_run_at、状态并重排下次执行时间。
func (r *Repository) MarkTaskRunFinished(ctx context.Context, id int64, finishedAt time.Time, status string, nextRunAt *time.Time) error {
	builder := r.client0().IQBenchmarkTask.UpdateOneID(id).
		SetLastRunAt(finishedAt).
		SetStatus(status).
		ClearStatusReason().
		ClearLeaseOwner().
		ClearLeaseExpiresAt()
	if nextRunAt == nil {
		builder = builder.ClearNextRunAt()
	} else {
		builder = builder.SetNextRunAt(*nextRunAt)
	}
	_, err := builder.Save(ctx)
	return err
}

// ---------- Run ----------

// CreateRun 新建 run（初始状态 running）。
func (r *Repository) CreateRun(ctx context.Context, in RunCreateInput) (*iqbenchmark.Run, error) {
	created, err := r.client0().IQBenchmarkRun.Create().
		SetTaskID(in.TaskID).
		SetGroupID(in.GroupID).
		SetModel(in.Model).
		SetTrigger(in.Trigger).
		SetStartedAt(in.StartedAt).
		SetInternalUserID(in.InternalUserID).
		SetInternalAPIKeyID(in.InternalAPIKeyID).
		SetStatus(iqbenchmark.RunStatusRunning).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return runFromEntity(created), nil
}

// FinishRun 结项 run：写入汇总计数、耗时与终态。
// 成本与 token 由 SetRunCost 单独写回（usage_logs 异步落库，需事后回采）。
func (r *Repository) FinishRun(ctx context.Context, id int64, finishedAt time.Time, status string, counts RunCounts) error {
	if strings.TrimSpace(status) == "" {
		status = iqbenchmark.RunStatusFailed
	}
	_, err := r.client0().IQBenchmarkRun.UpdateOneID(id).
		SetFinishedAt(finishedAt).
		SetStatus(status).
		SetSuccessCount(counts.Success).
		SetFailedCount(counts.Failed).
		SetTimeoutCount(counts.Timeout).
		SetBlockedCount(counts.Blocked).
		SetTotalDurationMs(counts.TotalDurationMS).
		Save(ctx)
	return err
}

// GetRun 按 ID 读取 run。
func (r *Repository) GetRun(ctx context.Context, id int64) (*iqbenchmark.Run, error) {
	entity, err := r.client0().IQBenchmarkRun.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, iqbenchmark.ErrRunNotFound
		}
		return nil, err
	}
	return runFromEntity(entity), nil
}

// ListRunsByTask 按任务分页读取 run。
func (r *Repository) ListRunsByTask(ctx context.Context, taskID int64, page, pageSize int) ([]iqbenchmark.Run, int64, error) {
	query := r.client0().IQBenchmarkRun.Query().
		Where(iqbenchmarkrun.TaskID(taskID), iqbenchmarkrun.DeletedAtIsNil())
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	entities, err := query.
		Order(dbent.Desc(iqbenchmarkrun.FieldCreatedAt), dbent.Desc(iqbenchmarkrun.FieldID)).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]iqbenchmark.Run, 0, len(entities))
	for _, entity := range entities {
		out = append(out, *runFromEntity(entity))
	}
	return out, int64(total), nil
}

// ListRunsNeedingCostCollection 读取需要回采成本的 run（usage_logs 异步落库）。
func (r *Repository) ListRunsNeedingCostCollection(ctx context.Context, olderThan time.Time, limit int) ([]iqbenchmark.Run, error) {
	entities, err := r.client0().IQBenchmarkRun.Query().
		Where(
			iqbenchmarkrun.CostCollectedAtIsNil(),
			iqbenchmarkrun.DeletedAtIsNil(),
			iqbenchmarkrun.StartedAtLT(olderThan),
		).
		Order(dbent.Asc(iqbenchmarkrun.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]iqbenchmark.Run, 0, len(entities))
	for _, entity := range entities {
		out = append(out, *runFromEntity(entity))
	}
	return out, nil
}

// SetRunCost 写回 run 的成本与 token 汇总，并标记已回采。
func (r *Repository) SetRunCost(ctx context.Context, id int64, cost float64, inputTokens, outputTokens int64, collectedAt time.Time) error {
	_, err := r.client0().IQBenchmarkRun.UpdateOneID(id).
		SetTotalCost(cost).
		SetTotalInputTokens(inputTokens).
		SetTotalOutputTokens(outputTokens).
		SetCostCollectedAt(collectedAt).
		Save(ctx)
	return err
}

// DailyCost 统计内部账号在 [since, now) 区间产生的检测成本（按专用 Key 过滤）。
func (r *Repository) DailyCost(ctx context.Context, apiKeyID int64, since time.Time) (float64, error) {
	var cost float64
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(actual_cost), 0)
		FROM usage_logs
		WHERE api_key_id = $1 AND created_at >= $2
	`, apiKeyID, since).Scan(&cost)
	if err != nil {
		return 0, err
	}
	return cost, nil
}

// UsageSnapshotByRequestIDs 按 request_id 汇总用量，用于 run 成本回采。
type UsageSnapshotByRequestIDs struct {
	Cost         float64
	InputTokens  int64
	OutputTokens int64
	Matched      int64
}

// SumUsageByRequestIDs 按 request_id 集合汇总 usage_logs。
// 同时用 api_key_id 兜底过滤，避免不同租户的同名 request_id 互相污染。
func (r *Repository) SumUsageByRequestIDs(ctx context.Context, apiKeyID int64, requestIDs []string) (UsageSnapshotByRequestIDs, error) {
	var out UsageSnapshotByRequestIDs
	if len(requestIDs) == 0 {
		return out, nil
	}
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(actual_cost), 0),
		       COALESCE(SUM(input_tokens), 0),
		       COALESCE(SUM(output_tokens), 0),
		       COUNT(*)
		FROM usage_logs
		WHERE api_key_id = $1 AND request_id = ANY($2)
	`, apiKeyID, pq.Array(requestIDs)).Scan(&out.Cost, &out.InputTokens, &out.OutputTokens, &out.Matched)
	if err != nil {
		return out, err
	}
	return out, nil
}

// ---------- Artifact ----------

// CreateArtifact 写入一条产出。失败/超时/被拦截的产出同样写入。
func (r *Repository) CreateArtifact(ctx context.Context, in ArtifactCreateInput) (*iqbenchmark.Artifact, error) {
	snapshot := in.CaseSnapshot
	if snapshot == nil {
		snapshot = map[string]any{}
	}
	params := in.RequestParams
	if params == nil {
		params = map[string]any{}
	}
	flags := in.UnsafeFlags
	if flags == nil {
		flags = []string{}
	}
	builder := r.client0().IQBenchmarkArtifact.Create().
		SetRunID(in.RunID).
		SetTaskID(in.TaskID).
		SetCaseID(in.CaseID).
		SetCaseSnapshot(snapshot).
		SetGroupID(in.GroupID).
		SetModel(in.Model).
		SetRequestParams(params).
		SetRenderKind(in.RenderKind).
		SetContentTruncated(in.ContentTruncated).
		SetUnsafeFlags(flags).
		SetDurationMs(in.DurationMS).
		SetInputTokens(in.InputTokens).
		SetOutputTokens(in.OutputTokens).
		SetCost(in.Cost).
		SetStatus(in.Status)
	if strings.TrimSpace(in.ImageURL) != "" {
		builder = builder.SetImageURL(in.ImageURL)
	}
	if in.Content != "" {
		builder = builder.SetContent(in.Content)
	}
	if strings.TrimSpace(in.ContentSHA256) != "" {
		builder = builder.SetContentSha256(in.ContentSHA256)
	}
	if strings.TrimSpace(in.RawResponseRef) != "" {
		builder = builder.SetRawResponseRef(in.RawResponseRef)
	}
	if strings.TrimSpace(in.BlobKey) != "" {
		builder = builder.SetBlobKey(in.BlobKey)
	}
	if strings.TrimSpace(in.ErrorCode) != "" {
		builder = builder.SetErrorCode(in.ErrorCode)
	}
	if strings.TrimSpace(in.ErrorSummary) != "" {
		builder = builder.SetErrorSummary(in.ErrorSummary)
	}
	created, err := builder.Save(ctx)
	if err != nil {
		return nil, err
	}
	return artifactFromEntity(created), nil
}

// GetArtifact 读取单条产出（仅未删除）。
func (r *Repository) GetArtifact(ctx context.Context, id int64) (*iqbenchmark.Artifact, error) {
	entity, err := r.client0().IQBenchmarkArtifact.Query().
		Where(iqbenchmarkartifact.ID(id), iqbenchmarkartifact.DeletedAtIsNil()).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, iqbenchmark.ErrArtifactNotFound
		}
		return nil, err
	}
	return artifactFromEntity(entity), nil
}

// ListArtifacts 按筛选条件分页读取产出。
//
// 稳定分页由 (created_at DESC, id DESC) 保证：created_at 完全相同的行
// 也不会在多次请求间换序。
func (r *Repository) ListArtifacts(ctx context.Context, f iqbenchmark.ArtifactListFilter) (*iqbenchmark.ArtifactListResult, error) {
	page := f.Page
	if page < 1 {
		page = 1
	}
	pageSize := f.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	query := r.client0().IQBenchmarkArtifact.Query().Where(iqbenchmarkartifact.DeletedAtIsNil())
	if f.GroupID != nil {
		query = query.Where(iqbenchmarkartifact.GroupIDEQ(*f.GroupID))
	}
	if strings.TrimSpace(f.Model) != "" {
		query = query.Where(iqbenchmarkartifact.ModelEQ(f.Model))
	}
	if f.CaseID != nil {
		query = query.Where(iqbenchmarkartifact.CaseIDEQ(*f.CaseID))
	}
	if strings.TrimSpace(f.Status) != "" {
		query = query.Where(iqbenchmarkartifact.StatusEQ(f.Status))
	}
	if strings.TrimSpace(f.Kind) != "" {
		// 图片页签 vs 题目问答页签：按渲染形态归类。
		if strings.TrimSpace(f.Kind) == iqbenchmark.RenderKindImageURL {
			query = query.Where(iqbenchmarkartifact.RenderKindEQ(iqbenchmark.RenderKindImageURL))
		} else {
			query = query.Where(iqbenchmarkartifact.RenderKindNEQ(iqbenchmark.RenderKindImageURL))
		}
	}
	if f.Since != nil {
		query = query.Where(iqbenchmarkartifact.CreatedAtGTE(*f.Since))
	}
	if f.Until != nil {
		query = query.Where(iqbenchmarkartifact.CreatedAtLT(*f.Until))
	}
	total, err := query.Count(ctx)
	if err != nil {
		return nil, err
	}
	entities, err := query.
		Order(dbent.Desc(iqbenchmarkartifact.FieldCreatedAt), dbent.Desc(iqbenchmarkartifact.FieldID)).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]iqbenchmark.Artifact, 0, len(entities))
	for _, entity := range entities {
		items = append(items, *artifactFromEntity(entity))
	}
	return &iqbenchmark.ArtifactListResult{
		Items:    items,
		Total:    int64(total),
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// ListArtifactsByRun 读取某次 run 的全部产出（含失败项，供重跑失败项使用）。
func (r *Repository) ListArtifactsByRun(ctx context.Context, runID int64) ([]iqbenchmark.Artifact, error) {
	entities, err := r.client0().IQBenchmarkArtifact.Query().
		Where(iqbenchmarkartifact.RunID(runID), iqbenchmarkartifact.DeletedAtIsNil()).
		Order(dbent.Asc(iqbenchmarkartifact.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]iqbenchmark.Artifact, 0, len(entities))
	for _, entity := range entities {
		out = append(out, *artifactFromEntity(entity))
	}
	return out, nil
}

// ScoreArtifact 写入人工评分。
func (r *Repository) ScoreArtifact(ctx context.Context, id int64, score float64, scoredBy int64, scoredAt time.Time) error {
	affected, err := r.client0().IQBenchmarkArtifact.Update().
		Where(iqbenchmarkartifact.ID(id), iqbenchmarkartifact.DeletedAtIsNil()).
		SetScore(score).
		SetScoredBy(scoredBy).
		SetScoredAt(scoredAt).
		Save(ctx)
	if err != nil {
		return err
	}
	if affected == 0 {
		return iqbenchmark.ErrArtifactNotFound
	}
	return nil
}

// SoftDeleteArtifact 软删除单条产出，返回其对象存储键（供清理 blob 使用）。
func (r *Repository) SoftDeleteArtifact(ctx context.Context, id int64) (string, error) {
	var blobKey sql.NullString
	err := r.db.QueryRowContext(ctx, `
		UPDATE iq_benchmark_artifacts
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING blob_key
	`, id).Scan(&blobKey)
	if errors.Is(err, sql.ErrNoRows) {
		return "", iqbenchmark.ErrArtifactNotFound
	}
	if err != nil {
		return "", err
	}
	return blobKey.String, nil
}

// CountLiveArtifacts 统计未删除的产出总数（容量守卫与清理使用）。
func (r *Repository) CountLiveArtifacts(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM iq_benchmark_artifacts WHERE deleted_at IS NULL`).Scan(&count)
	return count, err
}

// PurgeOlderThan 软删早于 cutoff 的产出，返回被删的行数与其中的对象存储键。
func (r *Repository) PurgeOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]PurgedArtifact, error) {
	rows, err := r.db.QueryContext(ctx, `
		WITH candidates AS (
			SELECT id FROM iq_benchmark_artifacts
			WHERE deleted_at IS NULL AND created_at < $1
			ORDER BY created_at ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE iq_benchmark_artifacts a
		SET deleted_at = NOW(), updated_at = NOW()
		FROM candidates c
		WHERE a.id = c.id
		RETURNING a.id, a.run_id, COALESCE(a.blob_key, '')
	`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPurged(rows)
}

// PurgeBeyondLimit 保留最新 keep 条，软删其余（按 created_at DESC, id DESC）。
func (r *Repository) PurgeBeyondLimit(ctx context.Context, keep int, limit int) ([]PurgedArtifact, error) {
	if keep < 0 {
		keep = 0
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH candidates AS (
			SELECT id FROM iq_benchmark_artifacts
			WHERE deleted_at IS NULL
			  AND id NOT IN (
				SELECT id FROM iq_benchmark_artifacts
				WHERE deleted_at IS NULL
				ORDER BY created_at DESC, id DESC
				LIMIT $1
			  )
			ORDER BY created_at ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE iq_benchmark_artifacts a
		SET deleted_at = NOW(), updated_at = NOW()
		FROM candidates c
		WHERE a.id = c.id
		RETURNING a.id, a.run_id, COALESCE(a.blob_key, '')
	`, keep, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanPurged(rows)
}

func scanPurged(rows *sql.Rows) ([]PurgedArtifact, error) {
	out := make([]PurgedArtifact, 0, 64)
	for rows.Next() {
		var item PurgedArtifact
		if err := rows.Scan(&item.ID, &item.RunID, &item.BlobKey); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// PurgeRunsWithoutArtifacts 软删没有存活产出的 run。
func (r *Repository) PurgeRunsWithoutArtifacts(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE iq_benchmark_runs r
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE r.deleted_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM iq_benchmark_artifacts a
			WHERE a.run_id = r.id AND a.deleted_at IS NULL
		  )
	`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// HardDeleteArtifactsSoftDeletedBefore 物理删除软删超过保留期的产出行。
func (r *Repository) HardDeleteArtifactsSoftDeletedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM iq_benchmark_artifacts WHERE deleted_at IS NOT NULL AND deleted_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// HardDeleteRunsSoftDeletedBefore 物理删除软删超过保留期的 run 行。
func (r *Repository) HardDeleteRunsSoftDeletedBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM iq_benchmark_runs WHERE deleted_at IS NOT NULL AND deleted_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------- Overview ----------

// GetOverviewCounts 读取总览计数。
func (r *Repository) GetOverviewCounts(ctx context.Context) (OverviewCounts, error) {
	var out OverviewCounts
	err := r.db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM iq_benchmark_artifacts WHERE deleted_at IS NULL),
			(SELECT COUNT(*) FROM iq_benchmark_artifacts WHERE deleted_at IS NULL AND render_kind = 'image_url'),
			(SELECT COUNT(*) FROM iq_benchmark_artifacts WHERE deleted_at IS NULL AND render_kind <> 'image_url'),
			(SELECT COUNT(*) FROM iq_benchmark_cases WHERE enabled = TRUE),
			(SELECT COUNT(*) FROM iq_benchmark_tasks),
			(SELECT COUNT(*) FROM iq_benchmark_tasks WHERE enabled = TRUE)
	`).Scan(&out.TotalArtifacts, &out.ImageArtifacts, &out.TextArtifacts, &out.CaseCount, &out.TaskCount, &out.RunningTasks)
	if err != nil {
		return out, err
	}
	return out, nil
}

// GetOverviewBuckets 按「分组 × 模型」聚合产出。
func (r *Repository) GetOverviewBuckets(ctx context.Context, limit int) ([]OverviewBucketRow, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT group_id,
		       model,
		       COUNT(*) AS total,
		       COUNT(*) FILTER (WHERE status = 'success') AS success_count,
		       COUNT(*) FILTER (WHERE status <> 'success') AS failed_count,
		       COALESCE(AVG(duration_ms), 0) AS avg_duration_ms,
		       MAX(created_at) AS last_run_at
		FROM iq_benchmark_artifacts
		WHERE deleted_at IS NULL
		GROUP BY group_id, model
		ORDER BY MAX(created_at) DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]OverviewBucketRow, 0, limit)
	for rows.Next() {
		var item OverviewBucketRow
		var lastRunAt sql.NullTime
		if err := rows.Scan(&item.GroupID, &item.Model, &item.Total, &item.SuccessCount,
			&item.FailedCount, &item.AvgDurationM, &lastRunAt); err != nil {
			return nil, err
		}
		if lastRunAt.Valid {
			t := lastRunAt.Time
			item.LastRunAt = &t
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------- 映射 ----------

func caseFromEntity(e *dbent.IQBenchmarkCase) *iqbenchmark.Case {
	if e == nil {
		return nil
	}
	params := e.Params
	if params == nil {
		params = map[string]any{}
	}
	systemPrompt := ""
	if e.SystemPrompt != nil {
		systemPrompt = *e.SystemPrompt
	}
	return &iqbenchmark.Case{
		ID:              e.ID,
		Name:            e.Name,
		Kind:            e.Kind,
		Prompt:          e.Prompt,
		SystemPrompt:    systemPrompt,
		Params:          params,
		ExpectKind:      e.ExpectKind,
		Scoring:         e.Scoring,
		LLMJudgeEnabled: e.LlmJudgeEnabled,
		Builtin:         e.Builtin,
		Enabled:         e.Enabled,
		SortOrder:       e.SortOrder,
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
	}
}

func taskFromEntity(e *dbent.IQBenchmarkTask) *iqbenchmark.Task {
	if e == nil {
		return nil
	}
	caseIDs := e.CaseIds
	if caseIDs == nil {
		caseIDs = []int64{}
	}
	statusReason := ""
	if e.StatusReason != nil {
		statusReason = *e.StatusReason
	}
	leaseOwner := ""
	if e.LeaseOwner != nil {
		leaseOwner = *e.LeaseOwner
	}
	return &iqbenchmark.Task{
		ID:             e.ID,
		Name:           e.Name,
		GroupID:        e.GroupID,
		Model:          e.Model,
		CaseIDs:        caseIDs,
		IntervalMinute: e.IntervalMinutes,
		JitterSeconds:  e.JitterSeconds,
		Enabled:        e.Enabled,
		Status:         e.Status,
		StatusReason:   statusReason,
		NextRunAt:      e.NextRunAt,
		LastRunAt:      e.LastRunAt,
		LeaseOwner:     leaseOwner,
		LeaseExpiresAt: e.LeaseExpiresAt,
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      e.UpdatedAt,
	}
}

func runFromEntity(e *dbent.IQBenchmarkRun) *iqbenchmark.Run {
	if e == nil {
		return nil
	}
	return &iqbenchmark.Run{
		ID:                e.ID,
		TaskID:            e.TaskID,
		GroupID:           e.GroupID,
		Model:             e.Model,
		Trigger:           e.Trigger,
		StartedAt:         e.StartedAt,
		FinishedAt:        e.FinishedAt,
		SuccessCount:      e.SuccessCount,
		FailedCount:       e.FailedCount,
		TimeoutCount:      e.TimeoutCount,
		BlockedCount:      e.BlockedCount,
		TotalDurationMS:   e.TotalDurationMs,
		TotalCost:         e.TotalCost,
		TotalInputTokens:  e.TotalInputTokens,
		TotalOutputTokens: e.TotalOutputTokens,
		InternalUserID:    e.InternalUserID,
		InternalAPIKeyID:  e.InternalAPIKeyID,
		Status:            e.Status,
		CostCollectedAt:   e.CostCollectedAt,
		CreatedAt:         e.CreatedAt,
	}
}

func artifactFromEntity(e *dbent.IQBenchmarkArtifact) *iqbenchmark.Artifact {
	if e == nil {
		return nil
	}
	snapshot := e.CaseSnapshot
	if snapshot == nil {
		snapshot = map[string]any{}
	}
	params := e.RequestParams
	if params == nil {
		params = map[string]any{}
	}
	flags := e.UnsafeFlags
	if flags == nil {
		flags = []string{}
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	return &iqbenchmark.Artifact{
		ID:               e.ID,
		RunID:            e.RunID,
		TaskID:           e.TaskID,
		CaseID:           e.CaseID,
		CaseSnapshot:     snapshot,
		GroupID:          e.GroupID,
		Model:            e.Model,
		RequestParams:    params,
		RenderKind:       e.RenderKind,
		ImageURL:         str(e.ImageURL),
		Content:          str(e.Content),
		ContentTruncated: e.ContentTruncated,
		ContentSHA256:    str(e.ContentSha256),
		UnsafeFlags:      flags,
		RawResponseRef:   str(e.RawResponseRef),
		DurationMS:       e.DurationMs,
		InputTokens:      e.InputTokens,
		OutputTokens:     e.OutputTokens,
		Cost:             e.Cost,
		Status:           e.Status,
		ErrorCode:        str(e.ErrorCode),
		ErrorSummary:     str(e.ErrorSummary),
		Score:            e.Score,
		ScoredBy:         e.ScoredBy,
		ScoredAt:         e.ScoredAt,
		BlobKey:          str(e.BlobKey),
		CreatedAt:        e.CreatedAt,
	}
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func nillableString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}
