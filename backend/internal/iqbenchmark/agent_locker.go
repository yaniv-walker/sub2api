package iqbenchmark

import (
	"context"
	"fmt"
)

// AgentLocker 用 repository 的咨询锁实现内部账号初始化的多实例互斥。
//
// 把它独立成小类型（而不是让 repository 直接满足 LockAcquirer）：
// repository 的 TryAdvisoryLock 需要传入具体 lockID，而 LockAcquirer 的
// 语义是「获取内部账号初始化的锁」，把 lockID 固定在这里可以避免
// 调用方误传别的锁 ID 造成互斥失效。
type AgentLocker struct {
	repo AdvisoryLocker
}

// AdvisoryLocker 是咨询锁的最小能力。
type AdvisoryLocker interface {
	TryAdvisoryLock(ctx context.Context, lockID int64) (bool, func(), error)
}

// NewAgentLocker 构造内部账号初始化锁。
func NewAgentLocker(repo AdvisoryLocker) (LockAcquirer, error) {
	if repo == nil {
		return nil, fmt.Errorf("iqbenchmark: advisory locker is not available")
	}
	return &AgentLocker{repo: repo}, nil
}

var _ LockAcquirer = (*AgentLocker)(nil)

// Acquire 尝试获取内部账号初始化锁。
//
// 未获取到（另一实例正在初始化）返回 (false, nil, nil)：
// 这不是错误，调用方应改为读取现有状态而不是失败退出。
func (l *AgentLocker) Acquire(ctx context.Context) (bool, func(), error) {
	if l == nil || l.repo == nil {
		// 无锁能力时退化为单实例语义：直接放行。
		return true, func() {}, nil
	}
	return l.repo.TryAdvisoryLock(ctx, InternalAgentInitLockID)
}
