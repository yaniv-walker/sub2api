package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func checkinMockRule(mock sqlmock.Sqlmock, c service.CheckinConfig, enabled bool) {
	raw, _ := json.Marshal(c)
	mock.ExpectQuery("SELECT version,config,effective_at,enabled FROM checkin_rules").WillReturnRows(sqlmock.NewRows([]string{"version", "config", "effective_at", "enabled"}).AddRow(c.Version, raw, c.EffectiveAt, enabled))
	mock.ExpectQuery("SELECT enabled FROM checkin_rules").WillReturnRows(sqlmock.NewRows([]string{"enabled"}).AddRow(enabled))
}
func TestCheckinRetryReturnsOriginalEvenWhenDisabled(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	now := time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)
	c := service.DefaultCheckinConfig()
	c.Version = 3
	c.EffectiveAt = now.Add(-time.Hour)
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	checkinMockRule(mock, c, false)
	mock.ExpectQuery("SELECT "+"id,user_id,business_date").WithArgs(int64(5), "2026-10-13").WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "business_date", "reward_type", "reward_amount", "balance_after", "rule_version", "claimed_at", "bonus"}).AddRow(99, 5, "2026-10-13", "daily", "0.15", "-1.85", 2, now, []byte(`{"enabled":false}`)))
	mock.ExpectRollback()
	calls := 0
	got, err := NewCheckinRepository(db).Claim(context.Background(), 5, now, func(service.CheckinConfig, service.CheckinInputs) (service.CheckinDraw, error) {
		calls++
		return service.CheckinDraw{}, nil
	})
	require.NoError(t, err)
	require.False(t, got.Created)
	require.Equal(t, int64(99), got.Record.ID)
	require.Equal(t, 0, calls)
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestCheckinSaveVersionConflictRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	now := time.Now()
	c := service.DefaultCheckinConfig()
	c.Version = 4
	c.EffectiveAt = now
	raw, _ := json.Marshal(c)
	mock.ExpectBegin()
	mock.ExpectExec("SELECT pg_advisory_xact_lock").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("SELECT version,config,effective_at,enabled FROM checkin_rules").WillReturnRows(sqlmock.NewRows([]string{"version", "config", "effective_at", "enabled"}).AddRow(4, raw, now, false))
	mock.ExpectRollback()
	_, err = NewCheckinRepository(db).SaveConfig(context.Background(), c, 3, 1, now)
	require.ErrorIs(t, err, service.ErrCheckinVersion)
	require.NoError(t, mock.ExpectationsWereMet())
}
func TestCheckinMissingRulesAreDisabledDefaults(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT version,config,effective_at,enabled FROM checkin_rules").WillReturnError(sql.ErrNoRows)
	c, err := NewCheckinRepository(db).GetConfig(context.Background(), time.Now(), false)
	require.NoError(t, err)
	require.False(t, c.Enabled)
	require.Equal(t, int64(0), c.Version)
	require.NoError(t, mock.ExpectationsWereMet())
}
