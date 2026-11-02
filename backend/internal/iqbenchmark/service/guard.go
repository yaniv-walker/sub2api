package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// 三类熔断的语义（三者互不替代，必须都有）：
//
//  1. 配额耗尽（quota_exhausted）：目标分组的配额被上游拒绝 → 立即停止调度
//     并清空 next_run_at，等管理员介入。绝不能改成「换个账号重试」——
//     那会把检测流量转移到真实用户的额度上。
//
//  2. 日成本预算（budget_exhausted）：当日检测成本超过 Settings.DailyCostBudget
//     → 所有任务暂停，次日自动恢复（预算按自然日滚动）。
//
//  3. 内部账号不可用（account_disabled）：内部账号被内容审核自动封禁或
//     被管理员禁用 → 所有任务暂停，只能人工恢复。
//
// 共同要求：绝不静默失败、绝不换账号、绝不无界重试。

// GuardStore 是熔断守卫真正需要的能力（刻意收窄）。
//
// 只声明实际用到的方法：守卫不该因为仓储新增了无关方法就编译不过，
// 也不该让调用方为了满足一个宽接口而构造巨型替身。
type GuardStore interface {
	// SetTaskStatus 写入任务状态、原因与下次执行时间（nil 表示停止调度）。
	SetTaskStatus(ctx context.Context, id int64, status, reason string, nextRunAt *time.Time) error
	// DailyCost 统计内部专用 Key 从 since 起的累计成本。
	DailyCost(ctx context.Context, apiKeyID int64, since time.Time) (float64, error)
}

// Guard 实现三类熔断判定与落库。
type Guard struct {
	Store  GuardStore
	Logger *slog.Logger
	// Now 可注入时钟，便于单测；默认 time.Now。
	Now func() time.Time
}

// NewGuard 构造熔断守卫。
func NewGuard(store GuardStore, logger *slog.Logger) *Guard {
	if logger == nil {
		logger = slog.Default()
	}
	return &Guard{Store: store, Logger: logger, Now: time.Now}
}

func (g *Guard) now() time.Time {
	if g == nil || g.Now == nil {
		return time.Now()
	}
	return g.Now()
}

// dayStart 返回本地时区当日起点。
//
// 预算按自然日滚动，因此必须用本地时区而不是 UTC：否则「今天」的边界
// 与管理员认知不一致，预算会在凌晨时段出现双倍可用窗口。
func dayStart(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, now.Location())
}

// TripQuotaExhausted 触发配额耗尽熔断：该任务停止调度。
//
// 只影响当个任务（配额是「分组 × Key」维度的，其他任务可能走不同分组），
// 但必须清空 next_run_at，避免到期扫描反复捡起同一个失败任务。
func (g *Guard) TripQuotaExhausted(ctx context.Context, taskID int64, reason string) error {
	g.Logger.Warn("iqbenchmark: quota exhausted, task scheduling stopped",
		"task_id", taskID, "reason", reason)
	return g.Store.SetTaskStatus(ctx, taskID, iqbenchmark.TaskStatusQuotaExhausted, reason, nil)
}

// RecoveryQuota 清除配额熔断（管理员充值/换 Key 后调用）。
//
// 恢复时必须重新写入 next_run_at：熔断时被清空了，不清空就无法再调度。
func (g *Guard) RecoveryQuota(ctx context.Context, task *iqbenchmark.Task) error {
	if task == nil {
		return iqbenchmark.ErrTaskNotFound
	}
	next := nextRunAt(g.now(), task.IntervalMinute, task.JitterSeconds)
	return g.Store.SetTaskStatus(ctx, task.ID, iqbenchmark.TaskStatusIdle, "", &next)
}

// TripBudgetExhausted 触发日预算熔断：所有任务停止到次日。
//
// 全局熔断（而非单任务）：预算统计的是内部账号整体日成本，
// 只停一个任务无法约束总花费。
func (g *Guard) TripBudgetExhausted(ctx context.Context, tasks []iqbenchmark.Task, spent, budget float64) error {
	reason := fmt.Sprintf("当日检测成本 %.4f 已达预算上限 %.4f，次日自动恢复", spent, budget)
	g.Logger.Warn("iqbenchmark: daily cost budget exhausted, all tasks paused",
		"spent", spent, "budget", budget, "task_count", len(tasks))
	next := dayStart(g.now()).Add(24 * time.Hour)
	var firstErr error
	for i := range tasks {
		if err := g.Store.SetTaskStatus(ctx, tasks[i].ID, iqbenchmark.TaskStatusBudgetExhausted, reason, &next); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			g.Logger.Error("iqbenchmark: failed to mark task budget_exhausted",
				"task_id", tasks[i].ID, "error", err)
		}
	}
	return firstErr
}

// RecoverBudgetIfNewDay 在新的一天自动恢复预算熔断的任务。
//
// 恢复条件：任务当前处于 budget_exhausted 且 next_run_at 已到期
// （熔断时把 next_run_at 设为次日 0 点，因此到期即代表已跨日）。
func (g *Guard) RecoverBudgetIfNewDay(ctx context.Context, tasks []iqbenchmark.Task) (int, error) {
	now := g.now()
	recovered := 0
	var firstErr error
	for i := range tasks {
		task := &tasks[i]
		if task.Status != iqbenchmark.TaskStatusBudgetExhausted {
			continue
		}
		next := nextRunAt(now, task.IntervalMinute, task.JitterSeconds)
		if err := g.Store.SetTaskStatus(ctx, task.ID, iqbenchmark.TaskStatusIdle, "", &next); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		recovered++
		g.Logger.Info("iqbenchmark: task recovered from budget_exhausted", "task_id", task.ID)
	}
	return recovered, firstErr
}

// TripAccountDisabled 触发内部账号不可用熔断：所有任务停止，只能人工恢复。
//
// 这里用 ERROR 级别：账号被封意味着检测链路已经完全不可用，
// 且恢复需要人工介入，属于必须被告警的事件。
func (g *Guard) TripAccountDisabled(ctx context.Context, tasks []iqbenchmark.Task, reason string) error {
	g.Logger.Error("iqbenchmark: internal agent unavailable, all tasks paused; manual recovery required",
		"reason", reason, "task_count", len(tasks))
	var firstErr error
	for i := range tasks {
		// 人工恢复前不写 next_run_at，避免被调度器重新捡起。
		if err := g.Store.SetTaskStatus(ctx, tasks[i].ID, iqbenchmark.TaskStatusAccountDisabled, reason, nil); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			g.Logger.Error("iqbenchmark: failed to mark task account_disabled",
				"task_id", tasks[i].ID, "error", err)
		}
	}
	return firstErr
}

// RecoverFromAccountDisabled 人工恢复：账号重新可用后由管理员触发。
func (g *Guard) RecoverFromAccountDisabled(ctx context.Context, task *iqbenchmark.Task) error {
	if task == nil {
		return iqbenchmark.ErrTaskNotFound
	}
	if task.Status != iqbenchmark.TaskStatusAccountDisabled {
		return nil
	}
	next := nextRunAt(g.now(), task.IntervalMinute, task.JitterSeconds)
	return g.Store.SetTaskStatus(ctx, task.ID, iqbenchmark.TaskStatusIdle, "", &next)
}

// CheckBudget 校验当日成本是否已超预算。
//
// 返回 (exhausted, spent, err)。预算为 0 表示不限制，直接返回未超限。
// 查询失败时返回错误而不是「当作未超限」：预算失效会让检测成本无界增长，
// 属于不可接受的降级方向；调用方应把本次 run 判为失败并停止调度。
func (g *Guard) CheckBudget(ctx context.Context, apiKeyID int64, budget float64, now time.Time) (bool, float64, error) {
	if budget <= 0 {
		return false, 0, nil
	}
	spent, err := g.Store.DailyCost(ctx, apiKeyID, dayStart(now))
	if err != nil {
		return false, 0, fmt.Errorf("iqbenchmark: query daily cost: %w", err)
	}
	return spent >= budget, spent, nil
}

// 预算相关的哨兵错误（供上层区分失败原因）。
var (
	ErrBudgetExhausted = errors.New("iqbenchmark: daily cost budget exhausted")
	ErrQuotaExhausted  = errors.New("iqbenchmark: upstream quota exhausted")
)
