package repository

import (
	"context"
	"log/slog"

	"github.com/Wei-Shaw/sub2api/ent"
)

// Repositories 聚合所有 Repository
type Repositories struct {
	BalanceSnapshot *BalanceSnapshotRepository
	ErrorRecord     *ErrorRecordRepository
	logger          *slog.Logger
}

// NewRepositories 创建 Repository 集合
func NewRepositories(client *ent.Client, logger *slog.Logger) *Repositories {
	if logger == nil {
		logger = slog.Default()
	}

	return &Repositories{
		BalanceSnapshot: NewBalanceSnapshotRepository(client),
		ErrorRecord:     NewErrorRecordRepository(client),
		logger:          logger.With("component", "repositories"),
	}
}

// WithTransaction 在事务中执行操作
func (r *Repositories) WithTransaction(ctx context.Context, fn func(tx *ent.Tx) error) error {
	tx, err := r.BalanceSnapshot.client.Tx(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	if err := fn(tx); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			r.logger.Error("failed to rollback transaction", "error", rerr)
		}
		return err
	}

	return tx.Commit()
}
