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
	service.NewGalleryService,
	ProvideBlobDeleter,
	service.NewArtifactService,
	service.NewGuard,
	ProvideGatewayClient,
	service.NewRunService,

	// 调度
	ProvideScheduler,

	// HTTP
	handler.NewAdminHandler,
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
	return service.NewGatewayClient(baseURL, "", time.Duration(timeoutSeconds)*time.Second)
}

// ProvideScheduler 构造任务调度器（构造期不启动 goroutine）。
func ProvideScheduler(
	repo *repository.Repository,
	runner *service.RunService,
	guard *service.Guard,
	settings *iqbenchmark.SettingsProvider,
	agent *iqbenchmark.ActiveDirectory,
	artifacts *service.ArtifactService,
	logger *slog.Logger,
) *scheduler.Scheduler {
	return scheduler.New(scheduler.Config{
		Store:    repo,
		Runner:   runner,
		Guard:    guard,
		Settings: settings,
		Agent:    agent,
		Cleaner:  artifacts,
		Logger:   logger,
	})
}

// ProvideHandlers 组装本模块的 handler 集合（供 routes 层使用）。
func ProvideHandlers(
	admin *handler.AdminHandler,
	user *handler.UserHandler,
	limiter *ratelimit.GalleryRateLimiter,
	settings *iqbenchmark.SettingsProvider,
) *handler.Handlers {
	return &handler.Handlers{
		Admin:   admin,
		User:    user,
		Limiter: limiter,
		IsEnabled: func() bool {
			return settings != nil && settings.IsEnabled(context.Background())
		},
	}
}
