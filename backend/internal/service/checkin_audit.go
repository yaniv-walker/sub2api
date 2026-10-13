package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func validateCheckinDateRange(from, to string) error {
	for _, date := range []string{from, to} {
		if date != "" {
			parsed, err := time.Parse("2006-01-02", date)
			if err != nil || parsed.Format("2006-01-02") != date {
				return infraerrors.BadRequest("CHECKIN_QUERY_INVALID", "dates must use YYYY-MM-DD")
			}
		}
	}
	if from != "" && to != "" && from > to {
		return infraerrors.BadRequest("CHECKIN_QUERY_INVALID", "date range is reversed")
	}
	return nil
}

func (s *CheckinService) RewardHistory(ctx context.Context, filter CheckinHistoryFilter) (*CheckinHistory, error) {
	if filter.UserID < 0 || filter.Cursor < 0 || filter.Limit < 1 || filter.Limit > 100 || (filter.RewardType != "" && filter.RewardType != "daily" && filter.RewardType != "cycle") {
		return nil, infraerrors.BadRequest("CHECKIN_QUERY_INVALID", "invalid reward filters")
	}
	if err := validateCheckinDateRange(filter.DateFrom, filter.DateTo); err != nil {
		return nil, err
	}
	return s.repo.RewardHistory(ctx, filter)
}

func (s *CheckinService) Budgets(ctx context.Context, from, to string, limit int) (*CheckinBudgets, error) {
	if limit < 1 || limit > 100 {
		return nil, infraerrors.BadRequest("CHECKIN_QUERY_INVALID", "invalid budget limit")
	}
	if err := validateCheckinDateRange(from, to); err != nil {
		return nil, err
	}
	return s.repo.Budgets(ctx, from, to, limit)
}
