package repository

import (
	"context"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/model"
	"github.com/stretchr/testify/require"
)

func TestBalanceAlertRepositoryMissingRuleDefaultsOff(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer client.Close()
	mock.ExpectQuery(`SELECT config,state FROM upstream_monitor_balance_alerts`).WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"config", "state"}))
	rule, err := NewBalanceAlertRepository(client).Get(context.Background(), 42)
	require.NoError(t, err)
	require.False(t, rule.Enabled)
	require.Equal(t, "10", rule.Threshold)
	require.Empty(t, rule.Recipients)
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestBalanceAlertRepositoryBusySaveDoesNotReportSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer client.Close()
	mock.ExpectExec(`(?s)INSERT INTO upstream_monitor_balance_alerts`).WithArgs(int64(42), false, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 0))
	err = NewBalanceAlertRepository(client).Save(context.Background(), 42, model.DefaultBalanceAlertConfig())
	require.ErrorIs(t, err, ErrAlertBusy)
	require.NoError(t, mock.ExpectationsWereMet())
}
