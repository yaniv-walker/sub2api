package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// ClaimedTask 是被本实例领取到的到期任务。
//
// 领取以「写入租约」为原子动作：同一行不会被两个实例同时领到，
// 因为候选集在同一个 UPDATE 里用 FOR UPDATE SKIP LOCKED 锁定。
type ClaimedTask struct {
	Task     iqbenchmark.Task
	LeaseTTL time.Duration
}

// ClaimDueTasks 领取到期任务并写入租约。
//
// 关键点：
//  1. `FOR UPDATE SKIP LOCKED` 保证跨实例互斥（数据库级，不依赖应用层协调）；
//  2. 只领 `enabled = true` 且 `status` 为可调度状态（idle/running）的任务 ——
//     熔断态（quota_exhausted / budget_exhausted / account_disabled）与
//     人工暂停（paused_by_admin）一律不领；
//  3. 租约已过期或从未领取的任务才可领（原持有者崩溃后可被接管）。
func (r *Repository) ClaimDueTasks(ctx context.Context, workerID string, limit int, lease time.Duration) ([]ClaimedTask, error) {
	if limit <= 0 {
		limit = 10
	}
	leaseSeconds := int64(lease / time.Second)
	if leaseSeconds < 1 {
		leaseSeconds = 90
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH candidates AS (
			SELECT id FROM iq_benchmark_tasks
			WHERE enabled = TRUE
			  AND status IN ('idle', 'running')
			  AND next_run_at IS NOT NULL
			  AND next_run_at <= NOW()
			  AND (lease_expires_at IS NULL OR lease_expires_at < NOW())
			ORDER BY next_run_at ASC, id ASC
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE iq_benchmark_tasks t
		SET lease_owner = $1,
		    lease_expires_at = NOW() + ($3 * INTERVAL '1 second'),
		    updated_at = NOW()
		FROM candidates c
		WHERE t.id = c.id
		RETURNING t.id, t.name, t.group_id, t.model, t.case_ids,
		          t.interval_minutes, t.jitter_seconds, t.enabled, t.status,
		          COALESCE(t.status_reason, ''), t.next_run_at, t.last_run_at,
		          COALESCE(t.lease_owner, ''), t.lease_expires_at,
		          t.created_at, t.updated_at
	`, workerID, limit, leaseSeconds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]ClaimedTask, 0, limit)
	for rows.Next() {
		var (
			task       iqbenchmark.Task
			nextRunAt  sql.NullTime
			lastRunAt  sql.NullTime
			leaseUntil sql.NullTime
			caseIDs    []byte
		)
		if err := rows.Scan(
			&task.ID, &task.Name, &task.GroupID, &task.Model, &caseIDs,
			&task.IntervalMinute, &task.JitterSeconds, &task.Enabled, &task.Status,
			&task.StatusReason, &nextRunAt, &lastRunAt,
			&task.LeaseOwner, &leaseUntil, &task.CreatedAt, &task.UpdatedAt,
		); err != nil {
			return nil, err
		}
		ids, err := decodeInt64Array(caseIDs)
		if err != nil {
			return nil, err
		}
		task.CaseIDs = ids
		if nextRunAt.Valid {
			t := nextRunAt.Time
			task.NextRunAt = &t
		}
		if lastRunAt.Valid {
			t := lastRunAt.Time
			task.LastRunAt = &t
		}
		if leaseUntil.Valid {
			t := leaseUntil.Time
			task.LeaseExpiresAt = &t
		}
		out = append(out, ClaimedTask{Task: task, LeaseTTL: lease})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// RenewLease 续期租约。
//
// 以「任务 ID + 租约持有者」为更新条件：影响行数为 0 表示租约已被其他实例
// 接管，调用方必须立即中止本轮且不写入任何产出。
func (r *Repository) RenewLease(ctx context.Context, taskID int64, workerID string, lease time.Duration) error {
	leaseSeconds := int64(lease / time.Second)
	if leaseSeconds < 1 {
		leaseSeconds = 90
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE iq_benchmark_tasks
		SET lease_expires_at = NOW() + ($3 * INTERVAL '1 second'), updated_at = NOW()
		WHERE id = $1 AND lease_owner = $2
	`, taskID, workerID, leaseSeconds)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return iqbenchmark.ErrLeaseLost
	}
	return nil
}

// ReleaseLease 释放本实例持有的租约（正常结束或 Shutdown 时调用）。
func (r *Repository) ReleaseLease(ctx context.Context, taskID int64, workerID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE iq_benchmark_tasks
		SET lease_owner = NULL, lease_expires_at = NULL, updated_at = NOW()
		WHERE id = $1 AND lease_owner = $2
	`, taskID, workerID)
	return err
}

// ReleaseAllLeases 释放本实例持有的全部租约（Shutdown 时使用）。
func (r *Repository) ReleaseAllLeases(ctx context.Context, workerID string) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE iq_benchmark_tasks
		SET lease_owner = NULL, lease_expires_at = NULL, updated_at = NOW()
		WHERE lease_owner = $1
	`, workerID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// AdvanceNextRunAtAfterClaim 在领取成功后立刻推进下次执行时间。
//
// 这样即使本轮执行崩溃，任务也不会在同一时刻被反复重试；
// 只更新「仍是本实例持有租约」的行，避免覆盖已接管的记录。
func (r *Repository) AdvanceNextRunAtAfterClaim(ctx context.Context, taskID int64, workerID string, next time.Time) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE iq_benchmark_tasks
		SET next_run_at = $3, status = 'running', updated_at = NOW()
		WHERE id = $1 AND lease_owner = $2
	`, taskID, workerID, next)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return iqbenchmark.ErrLeaseLost
	}
	return nil
}

// TryAdvisoryLock 尝试获取会话级咨询锁（清理任务与内部账号初始化使用）。
//
// 与既有 scheduler_outbox / migrations runner 的做法一致：
// 用一个稳定的 int64 键 + 多实例互斥，避免重复执行。
// 返回的 release 函数必须被调用（幂等，可 defer）。
func (r *Repository) TryAdvisoryLock(ctx context.Context, lockID int64) (bool, func(), error) {
	var acquired bool
	if err := r.db.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, lockID).Scan(&acquired); err != nil {
		return false, func() {}, err
	}
	if !acquired {
		return false, func() {}, nil
	}
	release := func() {
		// 用独立的超时上下文释放，避免调用方 ctx 已取消导致锁泄漏。
		relCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = r.db.ExecContext(relCtx, `SELECT pg_advisory_unlock($1)`, lockID)
	}
	return true, release, nil
}

// ErrNilDatabase 在仓储未被正确注入时返回，避免 nil panic。
var ErrNilDatabase = errors.New("iqbenchmark: nil database")

func decodeInt64Array(raw []byte) ([]int64, error) {
	if len(raw) == 0 {
		return []int64{}, nil
	}
	ids, err := iqbenchmark.DecodeCaseIDs(raw)
	if err != nil {
		return nil, err
	}
	return ids, nil
}
