package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/ent"
	"log/slog"
	"time"

	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/repository"
)

// ErrorAnalyzer analyzes error patterns for upstream accounts.
type ErrorAnalyzer struct {
	repo   *repository.ErrorRecordRepository
	logger *slog.Logger
}

// RecordsForUpstream includes every associated account, without summing balances.
func (e *ErrorAnalyzer) RecordsForUpstream(ctx context.Context, upstreamType string, accountIDs []int64, start, end time.Time) ([]*ent.UpstreamErrorRecord, error) {
	return e.repo.GetByAccountsTimeRange(ctx, upstreamType, accountIDs, start, end)
}

// NewErrorAnalyzer creates a new error analyzer.
func NewErrorAnalyzer(repo *repository.ErrorRecordRepository, logger *slog.Logger) *ErrorAnalyzer {
	if logger == nil {
		logger = slog.Default()
	}
	return &ErrorAnalyzer{
		repo:   repo,
		logger: logger.With("component", "error_analyzer"),
	}
}

// ErrorStats contains error statistics for an account.
type ErrorStats struct {
	AccountID    int64         `json:"account_id"`
	AccountName  string        `json:"account_name"`
	TotalErrors  int           `json:"total_errors"`
	ErrorRate    float64       `json:"error_rate"`
	CommonErrors []CommonError `json:"common_errors"`
}

// CommonError represents a common error pattern
type CommonError struct {
	ErrorCode string    `json:"error_code"`
	Count     int       `json:"count"`
	LastSeen  time.Time `json:"last_seen"`
}

// GetErrorStats retrieves error statistics for a given account.
func (e *ErrorAnalyzer) GetErrorStats(ctx context.Context, upstreamType string, accountID int64, days int) (*ErrorStats, error) {
	e.logger.Debug("Getting error stats", "account_id", accountID, "days", days)

	startTime := time.Now().AddDate(0, 0, -days)
	endTime := time.Now()

	// Get error count
	totalErrors, err := e.repo.CountByAccountTimeRange(ctx, upstreamType, accountID, startTime, endTime)
	if err != nil {
		e.logger.Error("Failed to count errors", "error", err)
		return nil, err
	}

	// Get error stats by type
	stats, err := e.repo.GetErrorStatsByAccount(ctx, upstreamType, accountID, startTime, endTime)
	if err != nil {
		e.logger.Error("Failed to get error stats", "error", err)
		return nil, err
	}

	// Get recent errors to find last seen times
	recentErrors, err := e.repo.GetByAccountTimeRange(ctx, upstreamType, accountID, startTime, endTime)
	if err != nil {
		e.logger.Error("Failed to get recent errors", "error", err)
		return nil, err
	}

	// Build common errors list
	errorLastSeen := make(map[string]time.Time)
	for _, record := range recentErrors {
		if existing, ok := errorLastSeen[record.ErrorType]; !ok || record.OccurredAt.After(existing) {
			errorLastSeen[record.ErrorType] = record.OccurredAt
		}
	}

	commonErrors := make([]CommonError, 0, len(stats))
	for errorType, count := range stats {
		commonErrors = append(commonErrors, CommonError{
			ErrorCode: errorType,
			Count:     count,
			LastSeen:  errorLastSeen[errorType],
		})
	}

	// Calculate error rate (errors per hour)
	hoursDiff := endTime.Sub(startTime).Hours()
	errorRate := 0.0
	if hoursDiff > 0 {
		errorRate = float64(totalErrors) / hoursDiff
	}

	return &ErrorStats{
		AccountID:    accountID,
		TotalErrors:  totalErrors,
		ErrorRate:    errorRate,
		CommonErrors: commonErrors,
	}, nil
}

// RecordError records a new error for an account.
func (e *ErrorAnalyzer) RecordError(ctx context.Context, upstreamType string, accountID int64, errorType, errorMessage string, httpStatus *int) error {
	e.logger.Debug("Recording error",
		"upstream_type", upstreamType,
		"account_id", accountID,
		"error_type", errorType,
	)

	_, err := e.repo.Create(ctx, upstreamType, accountID, errorType, errorMessage, httpStatus, time.Now())
	if err != nil {
		e.logger.Error("Failed to record error", "error", err)
		return err
	}

	e.logger.Info("Error recorded",
		"upstream_type", upstreamType,
		"account_id", accountID,
		"error_type", errorType,
	)

	return nil
}
