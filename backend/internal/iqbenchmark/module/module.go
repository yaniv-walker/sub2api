// Package module 是「模型智商检测作品展」的 Wire 装配层。
//
// 为什么单独一个包：根包 `iqbenchmark` 被 handler / service / repository 广泛
// 引用（它们都需要领域类型），如果 ProviderSet 也放在根包，就会形成
// `iqbenchmark → handler → iqbenchmark` 的导入环。把装配集中在这个只被
// cmd/server 引用的叶子包里，依赖方向保持单向：
//
//	module → handler / service / repository / scheduler / ratelimit → iqbenchmark
//
// 约定（与 internal/securityaudit 一致）：cmd/server/wire.go 只需
// `iqmodule.ProviderSet` 一行接入。
package module

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/wire"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/handler"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/ratelimit"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/repository"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/scheduler"
	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark/service"
	upstreamsvc "github.com/Wei-Shaw/sub2api/internal/service"
)

// ProviderSet 汇总本模块的全部构造器。
//
// 入参全部是 Wire 图中**已存在**的类型（*ent.Client、*sql.DB、*redis.Client、
// service.UserRepository、service.APIKeyRepository、service.SettingRepository、
// *config.Config、*slog.Logger），因此不需要在上游新增任何 provider。
//
// 刻意**不**包含调度器启动动作：调度器必须在 HTTP 服务就绪后由 main.go
// 显式 Start；构造期起 goroutine 会在初始化失败时泄漏。
var ProviderSet = wire.NewSet(
	// 数据层
	repository.New,
	wire.Bind(new(service.Store), new(*repository.Repository)),
	// Guard 只需要「写任务状态 + 查当日成本」两个能力，单独绑定而不是放宽 Store。
	wire.Bind(new(service.GuardStore), new(*repository.Repository)),

	// 内部账号存储（复用上游在 service 包中声明的仓储接口，不改上游）
	iqbenchmark.NewAgentUserStore,
	wire.Bind(new(iqbenchmark.InternalAgentUserStore), new(*iqbenchmark.AgentUserStore)),
	iqbenchmark.NewAgentKeyStore,
	wire.Bind(new(iqbenchmark.InternalAgentKeyStore), new(*iqbenchmark.AgentKeyStore)),
	iqbenchmark.NewAgentLocker,
	// 内部账号初始化用 pg_try_advisory_lock 串行化，避免多实例并发重复建号。
	wire.Bind(new(iqbenchmark.AdvisoryLocker), new(*repository.Repository)),

	// 配置
	ProvideSettingsStore,
	iqbenchmark.NewSettingsProvider,

	// 限流
	ratelimit.NewRedisCounter,
	wire.Bind(new(ratelimit.RedisClient), new(*ratelimit.RedisCounter)),
	ProvideGalleryRateLimiter,

	// 内部账号运行期状态
	iqbenchmark.NewActiveDirectory,

	// 业务服务
	ProvideGalleryService,
	ProvideBlobDeleter,
	service.NewArtifactService,
	service.NewGuard,
	ProvideGatewayClient,
	service.NewExecutor,
	service.NewRunService,

	// 目标解析（分组 + 模型级联下拉 / 任务二次校验）
	ProvideTargetsService,

	// 用户自测
	ProvideKeyResolver,
	ProvideUserTestLimiter,
	ProvideUserTestService,
	ProvideUserTestHandler,

	// 调度
	ProvideScheduler,

	// 运行期生命周期（启动调度器 + 初始化内部账号），构造期无副作用
	ProvideRuntime,

	// HTTP
	ProvideAdminHandler,
	handler.NewUserHandler,
	ProvideHandlers,
)

// ProvideSettingsStore 把上游 SettingRepository 适配为本模块的 SettingsStore。
//
// 上游接口提供 GetValue/Set，本模块需要 Get/Set，因此用一层薄适配转发
// （不改上游接口，也不引入第二套语义）。
func ProvideSettingsStore(repo upstreamsvc.SettingRepository) (iqbenchmark.SettingsStore, error) {
	if repo == nil {
		return nil, fmt.Errorf("iqbenchmark: setting repository is not available")
	}
	return &settingStoreAdapter{repo: repo}, nil
}

type settingStoreAdapter struct {
	repo upstreamsvc.SettingRepository
}

func (a *settingStoreAdapter) Get(ctx context.Context, key string) (string, error) {
	return a.repo.GetValue(ctx, key)
}

func (a *settingStoreAdapter) Set(ctx context.Context, key, value string) error {
	return a.repo.Set(ctx, key, value)
}

// ProvideGalleryRateLimiter 构造作品展限流器。
func ProvideGalleryRateLimiter(
	counter *ratelimit.RedisCounter,
	settings *iqbenchmark.SettingsProvider,
	logger *slog.Logger,
) *ratelimit.GalleryRateLimiter {
	return ratelimit.New(counter, ratelimit.SettingsProviderFunc(settings.Get), logger)
}

// ProvideBlobDeleter 提供对象存储删除能力。
//
// 已知限制（有意为之，见 design.md §3.16）：上游的 ImageStorage 具体实现由
// `ImageStorageSettingService` 在运行时按后台设置惰性构建，Wire 图里拿不到
// 那个 `service.ImageStorage` 实例。要注入它必须先给上游加一个导出方法，
// 那会突破「注册一行」的改动边界，因此这里返回 nil，让 ArtifactService
// 走「只软删数据库行 + WARN 日志」的降级路径：
//   - 作品立即从列表消失（用户可见效果正确）；
//   - 对象存储里的文件成为孤儿，由运维按日志清理。
//
// 注：`repository.S3ImageStorage` 已具备 `Delete` 方法（纯新增），
// 将来上游暴露 storage 实例后，把这里换成真实注入即可，业务代码无需改动。
func ProvideBlobDeleter() service.BlobDeleter { return nil }

// ProvideGatewayClient 构造网关自调客户端。
//
// BaseURL 解析顺序：
//  1. settings.internal_gateway_base_url（管理员显式配置）；
//  2. http://127.0.0.1:<cfg.Server.Port>（默认，走本机回环）。
//
// 只允许回环地址或显式配置的 http(s) 地址：一旦指向外部供应商，
// 检测流量就脱离了本服务的计费/审计/限流链路。
func ProvideGatewayClient(
	cfg *config.Config,
	settings *iqbenchmark.SettingsProvider,
	logger *slog.Logger,
) (*service.GatewayClient, error) {
	resolved := settings.Get(context.Background())
	baseURL := resolved.InternalGatewayBaseURL
	if baseURL == "" {
		port := 8080
		if cfg != nil && cfg.Server.Port > 0 {
			port = cfg.Server.Port
		}
		baseURL = fmt.Sprintf("http://127.0.0.1:%d", port)
	}
	timeoutSeconds := resolved.ModelTimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}
	return service.NewGatewayClient(baseURL, time.Duration(timeoutSeconds)*time.Second)
}

// ProvideTargetsService 构造目标解析服务。
//
// 直接接收上游**具体类型**再收窄成本模块的最小接口，而不是用 wire.Bind：
// wire.Bind 要求被绑定的具体类型也由同一个 ProviderSet 提供，而上游的
// GroupRepository / GatewayService 是在上游自己的 wire 图里提供的，
// 本模块的 ProviderSet 看不到它们（wire 会直接报「ProviderSet does not
// include a provider for ...」）。
//
// 收窄发生在函数签名里（参数用最小接口），因此本模块仍然只依赖
// ListActive / GetAvailableModels / InvalidateAvailableModelsCache 三个方法，
// 不会意外耦合上游的完整仓储与网关实现。
func ProvideTargetsService(
	groups upstreamsvc.GroupRepository,
	gateway *upstreamsvc.GatewayService,
) *service.TargetsService {
	var store service.TargetsGroupStore
	if groups != nil {
		store = groups
	}
	var lister service.GatewayModelLister
	if gateway != nil {
		lister = gateway
	}
	return service.NewTargetsService(store, lister)
}

// ProvideGalleryService 构造作品展服务并注入目标解析器与配置。
//
// 单独包一层是为了让 service.NewGalleryService 保持既有签名（否则所有单测
// 调用点都要改），同时保证生产链路一定带上目标校验与分组白名单。
func ProvideGalleryService(
	store service.Store,
	targets *service.TargetsService,
	settings *iqbenchmark.SettingsProvider,
	logger *slog.Logger,
) *service.GalleryService {
	return service.NewGalleryService(store, logger).WithTargets(targets).WithSettings(settings)
}

// ProvideScheduler 构造任务调度器（构造期不启动 goroutine）。
func ProvideScheduler(
	repo *repository.Repository,
	runner *service.RunService,
	guard *service.Guard,
	settings *iqbenchmark.SettingsProvider,
	agent *iqbenchmark.ActiveDirectory,
	artifacts *service.ArtifactService,
	targets *service.TargetsService,
	logger *slog.Logger,
) *scheduler.Scheduler {
	return scheduler.New(scheduler.Config{
		Store:    repo,
		Runner:   runner,
		Guard:    guard,
		Settings: settings,
		Agent:    agent,
		Cleaner:  artifacts,
		Targets:  targets,
		Logger:   logger,
	})
}

// ProvideAdminHandler 构造管理侧 handler 并注入目标解析器。
//
// 单独包一层，让 handler.NewAdminHandler 保持既有签名（既有单测不必改）。
func ProvideAdminHandler(
	gallery *service.GalleryService,
	run *service.RunService,
	artifact *service.ArtifactService,
	settings *iqbenchmark.SettingsProvider,
	agent *iqbenchmark.ActiveDirectory,
	targets *service.TargetsService,
) *handler.AdminHandler {
	return handler.NewAdminHandler(gallery, run, artifact, settings, agent).WithTargets(targets)
}

// ProvideHandlers 组装本模块的 handler 集合（供 routes 层使用）。
func ProvideHandlers(
	admin *handler.AdminHandler,
	user *handler.UserHandler,
	userTest *handler.UserTestHandler,
	limiter *ratelimit.GalleryRateLimiter,
	settings *iqbenchmark.SettingsProvider,
) *handler.Handlers {
	return &handler.Handlers{
		Admin:    admin,
		User:     user,
		UserTest: userTest,
		Limiter:  limiter,
		IsEnabled: func() bool {
			return settings != nil && settings.IsEnabled(context.Background())
		},
		IsUserTestEnabled: func() bool {
			if settings == nil {
				return false
			}
			resolved := settings.Get(context.Background())
			return resolved.Enabled && resolved.UserTestEnabled
		},
	}
}

// ProvideKeyResolver 把上游 *service.APIKeyService 收窄为本模块的 KeyResolver。
//
// 与 ProvideTargetsService 同样的理由：不用 wire.Bind（那要求具体类型
// 也由本 ProviderSet 提供），而是在函数签名里收窄成本模块的三个方法。
// 上游因此不需要任何改动，本模块也只依赖这三个已导出的能力。
func ProvideKeyResolver(apiKeys *upstreamsvc.APIKeyService) service.KeyResolver {
	if apiKeys == nil {
		return nil
	}
	return apiKeys
}

// ProvideUserTestLimiter 把限流器收窄为自测所需的能力。
func ProvideUserTestLimiter(limiter *ratelimit.GalleryRateLimiter) service.UserTestLimiter {
	if limiter == nil {
		return nil
	}
	return limiter
}

// ProvideUserTestService 构造自测服务。
//
// executor 由 Wire 注入与 RunService 共享的同一实例 —— 两条链路
// 复用同一份执行代码正是本设计要保证的事。
func ProvideUserTestService(
	store service.Store,
	executor *service.Executor,
	keys service.KeyResolver,
	targets *service.TargetsService,
	settings *iqbenchmark.SettingsProvider,
	limiter service.UserTestLimiter,
	logger *slog.Logger,
) *service.UserTestService {
	return service.NewUserTestService(store, executor, keys, targets, settings, limiter, logger)
}

// ProvideUserTestHandler 构造自测 handler。
//
// Audit 刻意留空（nil）：上游的审核入口 checkSecurityAuditBeforeSubmit 是
// 上游 handler 包内的方法，依赖上游中间件写入的 gin.Context 状态。
// 在本模块内无法安全地伪造那套上下文，因此这里**显式不启用审核**，
// 并在 design/verification 中记为已知限制（自测题目的 prompt 由管理员录入，
// 已经过一次人工审核，这是当前的兜底理由）。
func ProvideUserTestHandler(
	tests *service.UserTestService,
	settings *iqbenchmark.SettingsProvider,
) *handler.UserTestHandler {
	return &handler.UserTestHandler{Tests: tests, Settings: settings}
}
