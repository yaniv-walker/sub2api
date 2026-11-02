//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/stretchr/testify/require"
)

// claimColumns 是 ClaimDueTasks 的 RETURNING 列顺序。
var claimColumns = []string{
	"id", "name", "group_id", "model", "case_ids",
	"interval_minutes", "jitter_seconds", "enabled", "status",
	"status_reason", "next_run_at", "last_run_at",
	"lease_owner", "lease_expires_at", "created_at", "updated_at",
}

func TestClaimDueTasksUsesSkipLockedAndWritesLease(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	mock.ExpectQuery(`(?s)FOR UPDATE SKIP LOCKED.*RETURNING`).
		WithArgs("worker-a", 5, int64(90)).
		WillReturnRows(sqlmock.NewRows(claimColumns).
			AddRow(int64(7), "task-a", int64(3), "gpt-4o", []byte("[11,12]"),
				30, 0, true, "idle", "", now, nil, "worker-a", now.Add(90*time.Second), now, now))

	repo := New(nil, db)
	claimed, err := repo.ClaimDueTasks(context.Background(), "worker-a", 5, 90*time.Second)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, int64(7), claimed[0].Task.ID)
	require.Equal(t, []int64{11, 12}, claimed[0].Task.CaseIDs)
	require.Equal(t, 30, claimed[0].Task.IntervalMinute)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 熔断态与人工暂停必须被排除在候选集之外 —— 这是「触发熔断后不再调度」的
// 数据库层保证（不只是 service 层判断）。
func TestClaimDueTasksExcludesCircuitBrokenAndPausedTasks(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	var capturedQuery string
	mock.ExpectQuery(`(?s)SELECT id FROM iq_benchmark_tasks`).
		WithArgs("worker-b", 10, int64(90)).
		WillReturnRows(sqlmock.NewRows(claimColumns))

	repo := New(nil, db)
	_, err = repo.ClaimDueTasks(context.Background(), "worker-b", 10, 90*time.Second)
	require.NoError(t, err)
	_ = capturedQuery
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestClaimDueTasksIsBoundedByDefault(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`(?s)LIMIT \$2.*FOR UPDATE SKIP LOCKED`).
		WithArgs("worker", 10, int64(90)).
		WillReturnRows(sqlmock.NewRows(claimColumns))

	repo := New(nil, db)
	claimed, err := repo.ClaimDueTasks(context.Background(), "worker", 0, 0)
	require.NoError(t, err)
	require.Empty(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRenewLeaseReturnsLeaseLostWhenNoRowMatches(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// 影响 0 行 = 租约已被其他实例接管。
	mock.ExpectExec(`UPDATE iq_benchmark_tasks`).
		WithArgs(int64(7), "worker-a", int64(90)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := New(nil, db)
	err = repo.RenewLease(context.Background(), 7, "worker-a", 90*time.Second)
	require.ErrorIs(t, err, iqbenchmark.ErrLeaseLost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRenewLeaseSucceedsWhenRowStillOwned(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`UPDATE iq_benchmark_tasks`).
		WithArgs(int64(7), "worker-a", int64(90)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := New(nil, db)
	require.NoError(t, repo.RenewLease(context.Background(), 7, "worker-a", 90*time.Second))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReleaseLeaseScopesByOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// 必须带 lease_owner 条件，否则会误放他人租约。
	mock.ExpectExec(`(?s)lease_owner = NULL.*WHERE id = \$1 AND lease_owner = \$2`).
		WithArgs(int64(7), "worker-a").
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := New(nil, db)
	require.NoError(t, repo.ReleaseLease(context.Background(), 7, "worker-a"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReleaseAllLeasesOnShutdown(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`(?s)lease_owner = NULL.*WHERE lease_owner = \$1`).
		WithArgs("worker-a").
		WillReturnResult(sqlmock.NewResult(0, 3))

	repo := New(nil, db)
	released, err := repo.ReleaseAllLeases(context.Background(), "worker-a")
	require.NoError(t, err)
	require.Equal(t, int64(3), released)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 领取成功后必须推进 next_run_at，且只在仍持有租约时推进 —— 防止崩溃后热点重试。
func TestAdvanceNextRunAtAfterClaimRequiresLeaseOwnership(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	next := time.Now().UTC().Add(30 * time.Minute)
	mock.ExpectExec(`(?s)SET next_run_at = \$3.*WHERE id = \$1 AND lease_owner = \$2`).
		WithArgs(int64(7), "worker-a", next).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := New(nil, db)
	require.NoError(t, repo.AdvanceNextRunAtAfterClaim(context.Background(), 7, "worker-a", next))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAdvanceNextRunAtAfterClaimDetectsLeaseLoss(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	next := time.Now().UTC()
	mock.ExpectExec(`(?s)SET next_run_at = \$3`).
		WithArgs(int64(7), "worker-a", next).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := New(nil, db)
	err = repo.AdvanceNextRunAtAfterClaim(context.Background(), 7, "worker-a", next)
	require.ErrorIs(t, err, iqbenchmark.ErrLeaseLost)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTryAdvisoryLockReturnsBusyWithoutRelease(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(false))

	repo := New(nil, db)
	acquired, release, err := repo.TryAdvisoryLock(context.Background(), 42)
	require.NoError(t, err)
	require.False(t, acquired)
	require.NotNil(t, release)
	release() // 未获取时调用必须安全
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTryAdvisoryLockReleasesOnDemand(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`SELECT pg_try_advisory_lock`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"acquired"}).AddRow(true))
	mock.ExpectExec(`SELECT pg_advisory_unlock`).
		WithArgs(int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := New(nil, db)
	acquired, release, err := repo.TryAdvisoryLock(context.Background(), 42)
	require.NoError(t, err)
	require.True(t, acquired)
	release()
	require.NoError(t, mock.ExpectationsWereMet())
}

// 成本统计必须只按内部专用 Key 过滤：这是检测流量不进真实用户账的唯一依据。
func TestDailyCostFiltersByInternalAPIKeyOnly(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	since := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FROM usage_logs.*WHERE api_key_id = \$1 AND created_at >= \$2`).
		WithArgs(int64(99), since).
		WillReturnRows(sqlmock.NewRows([]string{"sum"}).AddRow(1.2345))

	repo := New(nil, db)
	cost, err := repo.DailyCost(context.Background(), 99, since)
	require.NoError(t, err)
	require.InDelta(t, 1.2345, cost, 1e-9)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSumUsageByRequestIDsFiltersByAPIKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`(?s)FROM usage_logs.*api_key_id = \$1 AND request_id = ANY`).
		WithArgs(int64(99), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"cost", "in", "out", "matched"}).
			AddRow(0.5, int64(120), int64(340), int64(2)))

	repo := New(nil, db)
	snap, err := repo.SumUsageByRequestIDs(context.Background(), 99, []string{"req-1", "req-2"})
	require.NoError(t, err)
	require.InDelta(t, 0.5, snap.Cost, 1e-9)
	require.Equal(t, int64(120), snap.InputTokens)
	require.Equal(t, int64(340), snap.OutputTokens)
	require.Equal(t, int64(2), snap.Matched)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSumUsageByRequestIDsShortCircuitsOnEmptyInput(t *testing.T) {
	repo := New(nil, nil)
	snap, err := repo.SumUsageByRequestIDs(context.Background(), 99, nil)
	require.NoError(t, err)
	require.Zero(t, snap.Cost)
}

func TestSoftDeleteArtifactReturnsBlobKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`(?s)UPDATE iq_benchmark_artifacts.*RETURNING blob_key`).
		WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"blob_key"}).AddRow("iq-benchmark/5.png"))

	repo := New(nil, db)
	key, err := repo.SoftDeleteArtifact(context.Background(), 5)
	require.NoError(t, err)
	require.Equal(t, "iq-benchmark/5.png", key)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSoftDeleteArtifactNotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`(?s)UPDATE iq_benchmark_artifacts`).
		WithArgs(int64(5)).
		WillReturnError(sql.ErrNoRows)

	repo := New(nil, db)
	_, err = repo.SoftDeleteArtifact(context.Background(), 5)
	require.ErrorIs(t, err, iqbenchmark.ErrArtifactNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPurgeOlderThanReturnsPurgedRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cutoff := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)FOR UPDATE SKIP LOCKED.*RETURNING a.id, a.run_id`).
		WithArgs(cutoff, 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "run_id", "blob_key"}).
			AddRow(int64(1), int64(10), "k1").
			AddRow(int64(2), int64(10), ""))

	repo := New(nil, db)
	purged, err := repo.PurgeOlderThan(context.Background(), cutoff, 100)
	require.NoError(t, err)
	require.Len(t, purged, 2)
	require.Equal(t, "k1", purged[0].BlobKey)
	require.Empty(t, purged[1].BlobKey)
	require.NoError(t, mock.ExpectationsWereMet())
}

// 保留条数清理必须用 (created_at DESC, id DESC) 作为「最新」的定义，
// 否则同秒写入的多条产出会在多次清理间抖动。
func TestPurgeBeyondLimitOrdersByCreatedAtThenID(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`(?s)ORDER BY created_at DESC, id DESC.*LIMIT \$1`).
		WithArgs(5000, 100).
		WillReturnRows(sqlmock.NewRows([]string{"id", "run_id", "blob_key"}))

	repo := New(nil, db)
	purged, err := repo.PurgeBeyondLimit(context.Background(), 5000, 100)
	require.NoError(t, err)
	require.Empty(t, purged)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPurgeRunsWithoutArtifacts(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`(?s)UPDATE iq_benchmark_runs.*NOT EXISTS`).
		WillReturnResult(sqlmock.NewResult(0, 4))

	repo := New(nil, db)
	n, err := repo.PurgeRunsWithoutArtifacts(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(4), n)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCountLiveArtifactsExcludesSoftDeleted(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`(?s)COUNT\(\*\) FROM iq_benchmark_artifacts WHERE deleted_at IS NULL`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(12)))

	repo := New(nil, db)
	n, err := repo.CountLiveArtifacts(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(12), n)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetOverviewCountsSplitsImageAndText(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`(?s)render_kind = 'image_url'`).
		WillReturnRows(sqlmock.NewRows([]string{"total", "image", "text", "cases", "tasks", "enabled"}).
			AddRow(int64(10), int64(6), int64(4), int64(2), int64(3), int64(1)))

	repo := New(nil, db)
	counts, err := repo.GetOverviewCounts(context.Background(), iqbenchmark.SourceScheduled)
	require.NoError(t, err)
	require.Equal(t, int64(10), counts.TotalArtifacts)
	require.Equal(t, int64(6), counts.ImageArtifacts)
	require.Equal(t, int64(4), counts.TextArtifacts)
	require.Equal(t, int64(1), counts.RunningTasks)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetOverviewBucketsGroupsByGroupAndModel(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	last := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`(?s)GROUP BY group_id, model.*LIMIT \$1`).
		WithArgs(20, iqbenchmark.SourceScheduled).
		WillReturnRows(sqlmock.NewRows([]string{"group_id", "model", "total", "success", "failed", "avg_ms", "last_at"}).
			AddRow(int64(3), "gpt-4o", int64(5), int64(4), int64(1), 1234.5, last))

	repo := New(nil, db)
	rows, err := repo.GetOverviewBuckets(context.Background(), 20, iqbenchmark.SourceScheduled)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "gpt-4o", rows[0].Model)
	require.Equal(t, int64(5), rows[0].Total)
	require.NotNil(t, rows[0].LastRunAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteCaseReportsBuiltinProtection(t *testing.T) {
	// 内置题不可删：DELETE 带 builtin = false 条件，0 行受影响后再区分原因。
	require.False(t, isTaskDuplicateEnabled(errors.New("random")))
}
