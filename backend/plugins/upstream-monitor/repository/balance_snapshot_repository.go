package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/upstreambalancesnapshot"
)

// BalanceSnapshotRepository 处理余额快照的数据库操作
type BalanceSnapshotRepository struct {
	client *ent.Client
}

// NewBalanceSnapshotRepository 创建新的余额快照仓库
func NewBalanceSnapshotRepository(client *ent.Client) *BalanceSnapshotRepository {
	return &BalanceSnapshotRepository{client: client}
}

// Create 创建新的余额快照
func (r *BalanceSnapshotRepository) Create(ctx context.Context, upstreamType string, accountID int64, balance float64, currency string, snapshotAt time.Time) (*ent.UpstreamBalanceSnapshot, error) {
	return r.client.UpstreamBalanceSnapshot.
		Create().
		SetUpstreamType(upstreamType).
		SetAccountID(accountID).
		SetBalance(balance).
		SetCurrency(currency).
		SetSnapshotAt(snapshotAt).
		Save(ctx)
}

// GetLatestByAccount 获取指定账号的最新余额快照
func (r *BalanceSnapshotRepository) GetLatestByAccount(ctx context.Context, upstreamType string, accountID int64) (*ent.UpstreamBalanceSnapshot, error) {
	return r.client.UpstreamBalanceSnapshot.
		Query().
		Where(
			upstreambalancesnapshot.UpstreamType(upstreamType),
			upstreambalancesnapshot.AccountID(accountID),
		).
		Order(ent.Desc(upstreambalancesnapshot.FieldSnapshotAt)).
		First(ctx)
}

// GetByAccountTimeRange 获取指定账号在时间范围内的余额快照
func (r *BalanceSnapshotRepository) GetByAccountTimeRange(ctx context.Context, upstreamType string, accountID int64, startTime, endTime time.Time) ([]*ent.UpstreamBalanceSnapshot, error) {
	return r.client.UpstreamBalanceSnapshot.
		Query().
		Where(
			upstreambalancesnapshot.UpstreamType(upstreamType),
			upstreambalancesnapshot.AccountID(accountID),
			upstreambalancesnapshot.SnapshotAtGTE(startTime),
			upstreambalancesnapshot.SnapshotAtLTE(endTime),
		).
		Order(ent.Asc(upstreambalancesnapshot.FieldSnapshotAt)).
		All(ctx)
}

// GetByAccountsTimeRange reads historical representatives of one shared balance.
func (r *BalanceSnapshotRepository) GetByAccountsTimeRange(ctx context.Context, upstreamType string, accountIDs []int64, startTime, endTime time.Time) ([]*ent.UpstreamBalanceSnapshot, error) {
	if len(accountIDs) == 0 {
		return []*ent.UpstreamBalanceSnapshot{}, nil
	}
	return r.client.UpstreamBalanceSnapshot.Query().Where(
		upstreambalancesnapshot.UpstreamType(upstreamType),
		upstreambalancesnapshot.AccountIDIn(accountIDs...),
		upstreambalancesnapshot.SnapshotAtGTE(startTime),
		upstreambalancesnapshot.SnapshotAtLTE(endTime),
	).Order(ent.Asc(upstreambalancesnapshot.FieldSnapshotAt)).All(ctx)
}

// GetAllByType 获取指定上游类型的所有最新余额快照
func (r *BalanceSnapshotRepository) GetAllByType(ctx context.Context, upstreamType string) ([]*ent.UpstreamBalanceSnapshot, error) {
	// 使用子查询获取每个账号的最新快照
	return r.client.UpstreamBalanceSnapshot.
		Query().
		Where(upstreambalancesnapshot.UpstreamType(upstreamType)).
		Order(ent.Desc(upstreambalancesnapshot.FieldSnapshotAt)).
		All(ctx)
}

// DeleteOlderThan 删除早于指定时间的快照记录
func (r *BalanceSnapshotRepository) DeleteOlderThan(ctx context.Context, before time.Time) (int, error) {
	return r.client.UpstreamBalanceSnapshot.
		Delete().
		Where(upstreambalancesnapshot.SnapshotAtLT(before)).
		Exec(ctx)
}

// DeleteByAccount 删除指定账号的所有快照记录
func (r *BalanceSnapshotRepository) DeleteByAccount(ctx context.Context, upstreamType string, accountID int64) (int, error) {
	return r.client.UpstreamBalanceSnapshot.
		Delete().
		Where(
			upstreambalancesnapshot.UpstreamType(upstreamType),
			upstreambalancesnapshot.AccountID(accountID),
		).
		Exec(ctx)
}

// CountByAccount 统计指定账号的快照数量
func (r *BalanceSnapshotRepository) CountByAccount(ctx context.Context, upstreamType string, accountID int64) (int, error) {
	return r.client.UpstreamBalanceSnapshot.
		Query().
		Where(
			upstreambalancesnapshot.UpstreamType(upstreamType),
			upstreambalancesnapshot.AccountID(accountID),
		).
		Count(ctx)
}
