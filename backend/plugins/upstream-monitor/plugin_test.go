package upstreammonitor

import (
	"context"
	"regexp"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestRunMigrationsCreatesAnalyticsTablesAndIndexes(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectExec(`(?s)CREATE TABLE IF NOT EXISTS upstream_monitor_secrets`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM upstream_monitor_secrets WHERE id = 1`)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectExec(`(?s)CREATE TABLE IF NOT EXISTS upstream_monitor_upstreams`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE upstream_monitor_upstreams ADD COLUMN IF NOT EXISTS access_token TEXT NULL`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE upstream_monitor_upstreams ADD COLUMN IF NOT EXISTS personal_access_token TEXT NULL`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE upstream_monitor_upstreams ADD COLUMN IF NOT EXISTS passkey TEXT NULL`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE upstream_monitor_upstreams ADD COLUMN IF NOT EXISTS quota_divider DOUBLE PRECISION NOT NULL DEFAULT 500000`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`ALTER TABLE upstream_monitor_upstreams ALTER COLUMN quota_divider SET DEFAULT 500000`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)CREATE TABLE IF NOT EXISTS upstream_balance_snapshots`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)CREATE INDEX IF NOT EXISTS upstreambalancesnapshot_upstream_type_account_id_snapshot_at`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)CREATE INDEX IF NOT EXISTS upstreambalancesnapshot_upstream_type_snapshot_at`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)CREATE TABLE IF NOT EXISTS upstream_error_records`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)CREATE INDEX IF NOT EXISTS upstreamerrorrecord_upstream_type_account_id_occurred_at`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`(?s)CREATE INDEX IF NOT EXISTS upstreamerrorrecord_upstream_type_error_type_occurred_at`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = (&Plugin{entClient: client}).runMigrations(context.Background())
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRunMigrationsAddsContextToDatabaseErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectExec(`(?s)CREATE TABLE IF NOT EXISTS upstream_monitor_secrets`).
		WillReturnError(assertionError("database unavailable"))

	err = (&Plugin{entClient: client}).runMigrations(context.Background())
	require.EqualError(t, err, "create upstream monitor secret table: database unavailable")
	require.NoError(t, mock.ExpectationsWereMet())
}

type assertionError string

func (e assertionError) Error() string { return string(e) }
