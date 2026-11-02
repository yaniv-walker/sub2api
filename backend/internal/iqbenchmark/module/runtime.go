// Package module 的运行期生命周期装配。
//
// 为什么需要这个文件：`ProviderSet` 里的构造器只负责「建对象」，
// 不负责「启动」。调度器与内部账号都属于**必须有明确启动/停止点**的组件：
//
//   - 调度器若在构造期起 goroutine，Wire 初始化失败时会泄漏一个永不停止的循环；
//   - 内部账号的创建要访问上游 User/APIKey 仓储，属于 IO，不能在构造期做。
//
// 因此把二者收敛到 `Runtime`：由 `cmd/server/main.go` 在 HTTP 服务就绪后
// 调一次 `Start`，在进程退出时由既有的 `provideCleanup` 调 `Shutdown`。
package module

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/scheduler"
)

// Runtime 持有本功能的运行期组件，并保证 Start/Shutdown 幂等、有界。
type Runtime struct {
	Scheduler *scheduler.Scheduler
	Settings  *iqbenchmark.SettingsProvider
	Agent     *iqbenchmark.ActiveDirectory

	deps   iqbenchmark.InternalAgentDeps
	logger *slog.Logger

	mu      sync.Mutex
	started bool
	// stopAgent 用于在 Shutdown 时停止内部账号的后台重试（当前为 nil）。
	stopAgent func()
}

// ProvideRuntime 汇总运行期依赖。构造期不做任何 IO、不起任何 goroutine。
func ProvideRuntime(
	sch *scheduler.Scheduler,
	settings *iqbenchmark.SettingsProvider,
	agent *iqbenchmark.ActiveDirectory,
	userStore *iqbenchmark.AgentUserStore,
	keyStore *iqbenchmark.AgentKeyStore,
	locker iqbenchmark.LockAcquirer,
	logger *slog.Logger,
) *Runtime {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runtime{
		Scheduler: sch,
		Settings:  settings,
		Agent:     agent,
		logger:    logger,
		deps: iqbenchmark.InternalAgentDeps{
			Users:  userStore,
			Keys:   keyStore,
			Locker: locker,
			// GroupExists 仅在需要校验目标分组时使用；当前建 Key 走
			// DefaultGroupID（默认 0 = 未指定），因此此处无需上游分组仓储。
			// 留 nil 表示「不校验」，与 settings.DefaultGroupID 的语义一致。
			GroupExists: nil,
			Logger:      logger,
		},
	}
}

// Start 初始化内部账号并启动调度器。
//
// 失败语义（硬性要求）：**绝不阻塞主 API 启动**。
//   - 功能未启用 → 直接返回 nil，不创建账号、不启动调度器；
//   - 内部账号初始化失败 → 记 WARN，把 ActiveDirectory 置为未就绪，
//     调度器仍会启动但每轮都会走 account_disabled 熔断（不产生任何上游调用）；
//   - 调度器启动失败 → 记 WARN 并返回错误，由调用方决定是否降级。
//
// 幂等：重复调用只有第一次生效。
func (r *Runtime) Start(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return nil
	}
	r.started = true
	r.mu.Unlock()

	if r.Settings == nil {
		r.logger.Info("iqbenchmark: runtime not started, settings provider missing")
		return nil
	}
	if !r.Settings.IsEnabled(ctx) {
		// 默认关闭：不建账号、不起调度器、不产生任何出站请求。
		r.logger.Info("iqbenchmark: runtime not started, feature is disabled")
		return nil
	}

	r.initAgent(ctx)

	if r.Scheduler == nil {
		r.logger.Warn("iqbenchmark: scheduler unavailable, tasks will not run")
		return nil
	}
	if err := r.Scheduler.Start(ctx); err != nil {
		return fmt.Errorf("iqbenchmark: start scheduler: %w", err)
	}
	return nil
}

// initAgent 幂等初始化内部账号并把结果写入 ActiveDirectory。
//
// 无论成功与否都不返回错误上抛给启动路径：内部账号失败只应让功能
// 「未就绪」，不应让整个服务起不来。
func (r *Runtime) initAgent(ctx context.Context) {
	if r.Agent == nil {
		return
	}
	// 内部账号创建涉及上游仓储，给一个独立的短超时，避免拖慢启动。
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	agent, err := iqbenchmark.EnsureInternalAgent(initCtx, r.deps)
	if err != nil {
		// 账号可能已建但 Key 缺失（例如目标分组尚未配置）：
		// 这种情况下也要把账号登记进去，才能让熔断报告出准确原因。
		if agent != nil && agent.User != nil {
			r.Agent.Set(agent, err.Error())
			r.logger.Warn("iqbenchmark: internal agent partially ready",
				"user_id", agent.User.ID, "error", err)
			return
		}
		r.Agent.Set(nil, err.Error())
		r.logger.Warn("iqbenchmark: internal agent initialization failed, feature stays unavailable",
			"error", err)
		return
	}
	if agent == nil || agent.User == nil {
		r.Agent.Set(nil, "internal agent missing after initialization")
		r.logger.Warn("iqbenchmark: internal agent missing after initialization")
		return
	}
	if agent.Key == nil {
		// 账号在、Key 不在：不能检测（没有 Key 就无法走网关），
		// 但仍登记账号，便于管理员在后台看到「已建账号、缺 Key」的明确状态。
		r.Agent.Set(agent, "internal agent has no api key")
		r.logger.Warn("iqbenchmark: internal agent has no api key, feature stays unavailable",
			"user_id", agent.User.ID)
		return
	}
	r.Agent.Set(agent, "")
	r.logger.Info("iqbenchmark: internal agent ready",
		"user_id", agent.User.ID, "api_key_id", agent.Key.ID)
}

// Shutdown 有界停止调度器。
func (r *Runtime) Shutdown(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	started := r.started
	stop := r.stopAgent
	r.stopAgent = nil
	r.mu.Unlock()

	if stop != nil {
		stop()
	}
	if !started || r.Scheduler == nil {
		return nil
	}
	if err := r.Scheduler.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}
