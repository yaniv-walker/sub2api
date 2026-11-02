package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/iqbenchmarkusertest"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// CreateUserTest 写入一条自测请求。
func (r *Repository) CreateUserTest(ctx context.Context, in iqbenchmark.UserTestCreateInput) (*iqbenchmark.UserTest, error) {
	created, err := r.client0().IQBenchmarkUserTest.Create().
		SetUserID(in.UserID).
		SetAPIKeyID(in.APIKeyID).
		SetCaseID(in.CaseID).
		SetGroupID(in.GroupID).
		SetModel(in.Model).
		SetStatus(iqbenchmark.UserTestStatusPending).
		SetEstimatedCost(in.EstimatedCost).
		SetDedupeKey(in.DedupeKey).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return userTestFromEntity(created), nil
}

// GetUserTest 读取单条自测记录。
func (r *Repository) GetUserTest(ctx context.Context, id int64) (*iqbenchmark.UserTest, error) {
	entity, err := r.client0().IQBenchmarkUserTest.Get(ctx, id)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, iqbenchmark.ErrUserTestNotFound
		}
		return nil, err
	}
	return userTestFromEntity(entity), nil
}

// ListUserTestsByUser 分页读取某用户的自测历史（倒序）。
func (r *Repository) ListUserTestsByUser(ctx context.Context, userID int64, page, pageSize int) ([]iqbenchmark.UserTest, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	query := r.client0().IQBenchmarkUserTest.Query().
		Where(iqbenchmarkusertest.UserIDEQ(userID))
	total, err := query.Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	entities, err := query.
		Order(dbent.Desc(iqbenchmarkusertest.FieldCreatedAt), dbent.Desc(iqbenchmarkusertest.FieldID)).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]iqbenchmark.UserTest, 0, len(entities))
	for _, entity := range entities {
		out = append(out, *userTestFromEntity(entity))
	}
	return out, int64(total), nil
}

// FinishUserTest 落终态。终态一旦写入不再回改（除 actual_cost 由成本回采补写）。
func (r *Repository) FinishUserTest(ctx context.Context, id int64, status, errorCode, errorSummary string, artifactID int64, actualCost float64) error {
	builder := r.client0().IQBenchmarkUserTest.UpdateOneID(id).
		SetStatus(status).
		SetActualCost(actualCost).
		SetUpdatedAt(time.Now())
	if errorCode != "" {
		builder = builder.SetErrorCode(errorCode)
	}
	if errorSummary != "" {
		builder = builder.SetErrorSummary(errorSummary)
	}
	if artifactID > 0 {
		builder = builder.SetArtifactID(artifactID)
	}
	return builder.Exec(ctx)
}

// UserTestQuota 一次返回「当日次数 / 在途数 / 当日成本」。
//
// 刻意用一条 SQL 取三个计数而不是三次往返：
// 预检要三者同时成立，分开查会让判定基于不同时刻的状态，
// 从而在并发提交下漏放或误拒。
//
// 当日成本取 GREATEST(estimated_cost, actual_cost) 累加：
// 已完成的用实际值，未完成的用预估值兜底，避免「先跑后扣」期间预算被绕过。
func (r *Repository) UserTestQuota(ctx context.Context, userID int64, since time.Time) (iqbenchmark.UserTestQuotaSnapshot, error) {
	var out iqbenchmark.UserTestQuotaSnapshot
	var todayCost sql.NullFloat64
	err := r.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE created_at >= $2),
			COUNT(*) FILTER (WHERE status IN ('pending', 'running')),
			COALESCE(SUM(GREATEST(estimated_cost, actual_cost)) FILTER (WHERE created_at >= $2), 0)
		FROM iq_benchmark_user_tests
		WHERE user_id = $1
	`, userID, since).Scan(&out.TodayCount, &out.InFlightCount, &todayCost)
	if err != nil {
		return out, err
	}
	if todayCost.Valid {
		out.TodayCost = todayCost.Float64
	}
	return out, nil
}

// CountUserTestDedupe 统计去重窗口内的相同请求数。
//
// 只统计「未明确失败」的记录：一个已经失败（且没有产出）的相同请求
// 不应阻止用户重试 —— 否则用户会被自己的一次瞬时故障锁住 10 分钟。
func (r *Repository) CountUserTestDedupe(ctx context.Context, dedupeKey string, since time.Time) (int, error) {
	count, err := r.client0().IQBenchmarkUserTest.Query().
		Where(
			iqbenchmarkusertest.DedupeKeyEQ(dedupeKey),
			iqbenchmarkusertest.CreatedAtGTE(since),
			iqbenchmarkusertest.StatusIn(
				iqbenchmark.UserTestStatusPending,
				iqbenchmark.UserTestStatusRunning,
				iqbenchmark.UserTestStatusSuccess,
			),
		).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("iqbenchmark: count user test dedupe: %w", err)
	}
	return count, nil
}

// userTestFromEntity 把 Ent 实体映射为领域对象。
func userTestFromEntity(e *dbent.IQBenchmarkUserTest) *iqbenchmark.UserTest {
	if e == nil {
		return nil
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	return &iqbenchmark.UserTest{
		ID:            e.ID,
		UserID:        e.UserID,
		APIKeyID:      e.APIKeyID,
		CaseID:        e.CaseID,
		GroupID:       e.GroupID,
		Model:         e.Model,
		ArtifactID:    derefInt64(e.ArtifactID),
		Status:        e.Status,
		ErrorCode:     str(e.ErrorCode),
		ErrorSummary:  str(e.ErrorSummary),
		EstimatedCost: e.EstimatedCost,
		ActualCost:    e.ActualCost,
		CreatedAt:     e.CreatedAt,
		UpdatedAt:     e.UpdatedAt,
	}
}
