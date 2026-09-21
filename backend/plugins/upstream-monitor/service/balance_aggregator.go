package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/repository"
)

// BalanceAggregator aggregates balance information from multiple upstream accounts.
type BalanceAggregator struct {
	fetcher       *UpstreamInfoFetcher
	snapshotRepo  *repository.BalanceSnapshotRepository
	logger        *slog.Logger
	saveSnapshots bool
}

// NewBalanceAggregator creates a new balance aggregator.
func NewBalanceAggregator(
	fetcher *UpstreamInfoFetcher,
	snapshotRepo *repository.BalanceSnapshotRepository,
	logger *slog.Logger,
) *BalanceAggregator {
	if logger == nil {
		logger = slog.Default()
	}
	return &BalanceAggregator{
		fetcher:       fetcher,
		snapshotRepo:  snapshotRepo,
		logger:        logger.With("component", "balance_aggregator"),
		saveSnapshots: true, // Enable snapshot saving by default
	}
}

// AggregatedBalance contains aggregated balance information.
type AggregatedBalance struct {
	TotalBalance float64                 `json:"total_balance"`
	ByType       map[string]float64      `json:"by_type"`
	ByAccount    map[int64]*UpstreamInfo `json:"by_account"`
	Errors       map[int64]string        `json:"errors,omitempty"`
}

// AccountInfo represents basic account information for fetching.
type AccountInfo struct {
	ID           int64
	UpstreamType string
	BaseURL      string
	ApiKey       string
}

// Aggregate fetches and aggregates balance information from multiple accounts.
func (a *BalanceAggregator) Aggregate(ctx context.Context, accounts []AccountInfo) *AggregatedBalance {
	a.logger.Info("Aggregating balance", "account_count", len(accounts))

	result := &AggregatedBalance{
		TotalBalance: 0,
		ByType:       make(map[string]float64),
		ByAccount:    make(map[int64]*UpstreamInfo),
		Errors:       make(map[int64]string),
	}

	if len(accounts) == 0 {
		return result
	}

	// Fetch balance concurrently
	var wg sync.WaitGroup
	var mu sync.Mutex
	snapshotTime := time.Now()

	for _, acc := range accounts {
		wg.Add(1)
		go func(account AccountInfo) {
			defer wg.Done()

			info, err := a.fetcher.FetchInfo(account.ID, account.UpstreamType, account.BaseURL, account.ApiKey)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				a.logger.Error("Failed to fetch upstream info",
					"account_id", account.ID,
					"error", err,
				)
				result.Errors[account.ID] = err.Error()
				return
			}

			result.ByAccount[account.ID] = info
			result.TotalBalance += info.Balance
			result.ByType[info.Type] += info.Balance

			// Save balance snapshot if enabled
			if a.saveSnapshots {
				if err := a.saveSnapshot(ctx, account.UpstreamType, account.ID, info.Balance, snapshotTime); err != nil {
					a.logger.Error("Failed to save balance snapshot",
						"account_id", account.ID,
						"error", err,
					)
					// Don't fail the aggregation on snapshot error
				}
			}
		}(acc)
	}

	wg.Wait()

	a.logger.Info("Balance aggregation completed",
		"total_balance", result.TotalBalance,
		"success_count", len(result.ByAccount),
		"error_count", len(result.Errors),
	)

	return result
}

// saveSnapshot saves a balance snapshot to the database
func (a *BalanceAggregator) saveSnapshot(ctx context.Context, upstreamType string, accountID int64, balance float64, snapshotTime time.Time) error {
	_, err := a.snapshotRepo.Create(ctx, upstreamType, accountID, balance, "CNY", snapshotTime)
	if err != nil {
		return err
	}

	a.logger.Debug("Balance snapshot saved",
		"upstream_type", upstreamType,
		"account_id", accountID,
		"balance", balance,
	)

	return nil
}
