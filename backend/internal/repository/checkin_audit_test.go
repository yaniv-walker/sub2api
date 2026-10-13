package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCheckinAuditUsesParameterizedFilters(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	r := NewCheckinRepository(db)
	rows := sqlmock.NewRows([]string{"id", "user_id", "business_date", "reward_type", "reward_amount", "balance_after", "rule_version", "claimed_at", "bonus"}).AddRow(5, 3, "2026-10-13", "cycle", "0.50", "1.00", 1, time.Now(), []byte(`{}`)).AddRow(4, 3, "2026-10-12", "cycle", "0.50", "0.50", 1, time.Now(), []byte(`{}`))
	mock.ExpectQuery("SELECT .* FROM checkin_records").WithArgs(int64(3), int64(0), 2, "2026-10-01", "2026-10-13", "cycle").WillReturnRows(rows)
	result, err := r.RewardHistory(context.Background(), service.CheckinHistoryFilter{UserID: 3, Limit: 1, DateFrom: "2026-10-01", DateTo: "2026-10-13", RewardType: "cycle"})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, int64(5), result.NextCursor)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckinBudgetsReportsReservedAndRemaining(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT business_date::text,limit_usd::text").WithArgs("2026-10-01", "2026-10-13", 30).WillReturnRows(sqlmock.NewRows([]string{"date", "limit", "spent", "reserved", "remaining"}).AddRow("2026-10-13", "100.00", "44.00", "5.00", "51.00"))
	result, err := NewCheckinRepository(db).Budgets(context.Background(), "2026-10-01", "2026-10-13", 30)
	require.NoError(t, err)
	require.Equal(t, "51.00", result.Items[0].RemainingUSD)
	require.NoError(t, mock.ExpectationsWereMet())
}
