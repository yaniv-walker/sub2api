package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/enttest"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBalanceSnapshotRepository_Create(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	repo := NewBalanceSnapshotRepository(client)
	ctx := context.Background()

	snapshot, err := repo.Create(ctx, "sub2api", 1, 100.50, "CNY", time.Now())
	require.NoError(t, err)
	assert.NotNil(t, snapshot)
	assert.Equal(t, "sub2api", snapshot.UpstreamType)
	assert.Equal(t, int64(1), snapshot.AccountID)
	assert.Equal(t, 100.50, snapshot.Balance)
	assert.Equal(t, "CNY", snapshot.Currency)
}

func TestBalanceSnapshotRepository_GetLatestByAccount(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	repo := NewBalanceSnapshotRepository(client)
	ctx := context.Background()

	// Create multiple snapshots
	now := time.Now()
	_, _ = repo.Create(ctx, "sub2api", 1, 100.0, "CNY", now.Add(-2*time.Hour))
	_, _ = repo.Create(ctx, "sub2api", 1, 90.0, "CNY", now.Add(-1*time.Hour))
	latest, _ := repo.Create(ctx, "sub2api", 1, 85.0, "CNY", now)

	// Get latest
	result, err := repo.GetLatestByAccount(ctx, "sub2api", 1)
	require.NoError(t, err)
	assert.Equal(t, latest.ID, result.ID)
	assert.Equal(t, 85.0, result.Balance)
}

func TestBalanceSnapshotRepository_GetByAccountTimeRange(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	repo := NewBalanceSnapshotRepository(client)
	ctx := context.Background()

	now := time.Now()
	startTime := now.Add(-3 * time.Hour)
	endTime := now

	// Create snapshots
	_, _ = repo.Create(ctx, "sub2api", 1, 100.0, "CNY", now.Add(-4*time.Hour)) // outside range
	_, _ = repo.Create(ctx, "sub2api", 1, 90.0, "CNY", now.Add(-2*time.Hour))  // in range
	_, _ = repo.Create(ctx, "sub2api", 1, 85.0, "CNY", now.Add(-1*time.Hour))  // in range

	// Query time range
	snapshots, err := repo.GetByAccountTimeRange(ctx, "sub2api", 1, startTime, endTime)
	require.NoError(t, err)
	assert.Len(t, snapshots, 2)
}

func TestBalanceSnapshotRepository_DeleteOlderThan(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	repo := NewBalanceSnapshotRepository(client)
	ctx := context.Background()

	now := time.Now()
	cutoff := now.Add(-24 * time.Hour)

	// Create old and new snapshots
	_, _ = repo.Create(ctx, "sub2api", 1, 100.0, "CNY", now.Add(-48*time.Hour)) // old
	_, _ = repo.Create(ctx, "sub2api", 1, 90.0, "CNY", now.Add(-12*time.Hour))  // new

	// Delete old
	deleted, err := repo.DeleteOlderThan(ctx, cutoff)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)

	// Verify
	count, err := repo.CountByAccount(ctx, "sub2api", 1)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestErrorRecordRepository_Create(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	repo := NewErrorRecordRepository(client)
	ctx := context.Background()

	httpStatus := 500
	record, err := repo.Create(ctx, "sub2api", 1, "server_error", "Internal server error", &httpStatus, time.Now())
	require.NoError(t, err)
	assert.NotNil(t, record)
	assert.Equal(t, "sub2api", record.UpstreamType)
	assert.Equal(t, int64(1), record.AccountID)
	assert.Equal(t, "server_error", record.ErrorType)
}

func TestErrorRecordRepository_GetErrorStatsByAccount(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	repo := NewErrorRecordRepository(client)
	ctx := context.Background()

	now := time.Now()
	startTime := now.Add(-24 * time.Hour)

	// Create various error types
	_, _ = repo.Create(ctx, "sub2api", 1, "network", "Timeout", nil, now.Add(-1*time.Hour))
	_, _ = repo.Create(ctx, "sub2api", 1, "network", "Connection refused", nil, now.Add(-2*time.Hour))
	_, _ = repo.Create(ctx, "sub2api", 1, "auth", "Invalid token", nil, now.Add(-3*time.Hour))

	// Get stats
	stats, err := repo.GetErrorStatsByAccount(ctx, "sub2api", 1, startTime, now)
	require.NoError(t, err)
	assert.Equal(t, 2, stats["network"])
	assert.Equal(t, 1, stats["auth"])
}

func TestErrorRecordRepository_DeleteByAccount(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&cache=shared&_fk=1")
	defer client.Close()

	repo := NewErrorRecordRepository(client)
	ctx := context.Background()

	// Create records for different accounts
	_, _ = repo.Create(ctx, "sub2api", 1, "network", "Error", nil, time.Now())
	_, _ = repo.Create(ctx, "sub2api", 2, "network", "Error", nil, time.Now())

	// Delete account 1's records
	deleted, err := repo.DeleteByAccount(ctx, "sub2api", 1)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)

	// Verify account 2's records still exist
	count, err := repo.CountByAccountTimeRange(ctx, "sub2api", 2, time.Now().Add(-1*time.Hour), time.Now().Add(1*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}
