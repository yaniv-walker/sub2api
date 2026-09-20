package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/upstreamerrorrecord"
)

// ErrorRecordRepository 处理错误记录的数据库操作
type ErrorRecordRepository struct {
	client *ent.Client
}

// NewErrorRecordRepository 创建新的错误记录仓库
func NewErrorRecordRepository(client *ent.Client) *ErrorRecordRepository {
	return &ErrorRecordRepository{client: client}
}

// Create 创建新的错误记录
func (r *ErrorRecordRepository) Create(ctx context.Context, upstreamType string, accountID int64, errorType, errorMessage string, httpStatus *int, occurredAt time.Time) (*ent.UpstreamErrorRecord, error) {
	create := r.client.UpstreamErrorRecord.
		Create().
		SetUpstreamType(upstreamType).
		SetAccountID(accountID).
		SetErrorType(errorType).
		SetOccurredAt(occurredAt)

	if errorMessage != "" {
		create.SetErrorMessage(errorMessage)
	}

	if httpStatus != nil {
		create.SetHTTPStatus(*httpStatus)
	}

	return create.Save(ctx)
}

// GetByAccountTimeRange 获取指定账号在时间范围内的错误记录
func (r *ErrorRecordRepository) GetByAccountTimeRange(ctx context.Context, upstreamType string, accountID int64, startTime, endTime time.Time) ([]*ent.UpstreamErrorRecord, error) {
	return r.client.UpstreamErrorRecord.
		Query().
		Where(
			upstreamerrorrecord.UpstreamType(upstreamType),
			upstreamerrorrecord.AccountID(accountID),
			upstreamerrorrecord.OccurredAtGTE(startTime),
			upstreamerrorrecord.OccurredAtLTE(endTime),
		).
		Order(ent.Desc(upstreamerrorrecord.FieldOccurredAt)).
		All(ctx)
}

// CountByAccountTimeRange 统计指定账号在时间范围内的错误数量
func (r *ErrorRecordRepository) CountByAccountTimeRange(ctx context.Context, upstreamType string, accountID int64, startTime, endTime time.Time) (int, error) {
	return r.client.UpstreamErrorRecord.
		Query().
		Where(
			upstreamerrorrecord.UpstreamType(upstreamType),
			upstreamerrorrecord.AccountID(accountID),
			upstreamerrorrecord.OccurredAtGTE(startTime),
			upstreamerrorrecord.OccurredAtLTE(endTime),
		).
		Count(ctx)
}

// GetByErrorType 获取指定错误类型的记录
func (r *ErrorRecordRepository) GetByErrorType(ctx context.Context, upstreamType, errorType string, limit int) ([]*ent.UpstreamErrorRecord, error) {
	query := r.client.UpstreamErrorRecord.
		Query().
		Where(
			upstreamerrorrecord.UpstreamType(upstreamType),
			upstreamerrorrecord.ErrorType(errorType),
		).
		Order(ent.Desc(upstreamerrorrecord.FieldOccurredAt))

	if limit > 0 {
		query = query.Limit(limit)
	}

	return query.All(ctx)
}

// GetRecentErrors 获取最近的错误记录
func (r *ErrorRecordRepository) GetRecentErrors(ctx context.Context, upstreamType string, limit int) ([]*ent.UpstreamErrorRecord, error) {
	return r.client.UpstreamErrorRecord.
		Query().
		Where(upstreamerrorrecord.UpstreamType(upstreamType)).
		Order(ent.Desc(upstreamerrorrecord.FieldOccurredAt)).
		Limit(limit).
		All(ctx)
}

// DeleteOlderThan 删除早于指定时间的错误记录
func (r *ErrorRecordRepository) DeleteOlderThan(ctx context.Context, before time.Time) (int, error) {
	return r.client.UpstreamErrorRecord.
		Delete().
		Where(upstreamerrorrecord.OccurredAtLT(before)).
		Exec(ctx)
}

// DeleteByAccount 删除指定账号的所有错误记录
func (r *ErrorRecordRepository) DeleteByAccount(ctx context.Context, upstreamType string, accountID int64) (int, error) {
	return r.client.UpstreamErrorRecord.
		Delete().
		Where(
			upstreamerrorrecord.UpstreamType(upstreamType),
			upstreamerrorrecord.AccountID(accountID),
		).
		Exec(ctx)
}

// GetErrorStatsByAccount 获取指定账号的错误统计
func (r *ErrorRecordRepository) GetErrorStatsByAccount(ctx context.Context, upstreamType string, accountID int64, startTime, endTime time.Time) (map[string]int, error) {
	records, err := r.GetByAccountTimeRange(ctx, upstreamType, accountID, startTime, endTime)
	if err != nil {
		return nil, err
	}

	stats := make(map[string]int)
	for _, record := range records {
		stats[record.ErrorType]++
	}

	return stats, nil
}
