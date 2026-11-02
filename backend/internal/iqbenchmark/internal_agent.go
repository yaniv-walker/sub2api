package iqbenchmark

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"golang.org/x/crypto/bcrypt"
)

// InternalAgentEmail 是内部虚拟账号的固定邮箱。
//
// 使用 RFC 2606 保留 TLD `.invalid`：该域名永不可路由到真实收件箱，
// 因此该账号不可能被注册流程或邮件找回流程触及。
//
// 注意：`service.isReservedEmail()` 目前只拦截 4 个既有合成域
// （LinuxDo / OIDC / DingTalk / WeChat），**不包含** `.invalid`，
// 因此不得依赖它作为防护手段；隔离的唯一权威标识是 notes 前缀。
const InternalAgentEmail = "iq-benchmark@iq-benchmark.invalid"

// InternalAgentNotesPrefix 是内部账号的唯一权威隔离标识。
//
// 除本文件外，任何代码都不得硬编码该前缀或上面的邮箱用于身份判定 ——
// 判定必须统一走 IsInternalAgent。
const InternalAgentNotesPrefix = "iq-benchmark:internal-agent"

// InternalAgentNotes 是写入 users.notes 的完整内容。
const InternalAgentNotes = InternalAgentNotesPrefix + " | 模型智商检测专用账号，请勿登录或删除"

// InternalAgentKeyName 是内部专用 API Key 的名称。
const InternalAgentKeyName = "iq-benchmark-agent"

// InternalAgentInitLockID 是内部账号初始化用的咨询锁键。
const InternalAgentInitLockID int64 = 7126034518820030001

// ErrInternalAgentMissing 表示内部账号尚未初始化（功能未就绪）。
var ErrInternalAgentMissing = errors.New("iqbenchmark: internal agent is not initialized")

// IsInternalAgent 判断给定用户是否为内部检测账号。
//
// 这是全模块唯一的判定入口：多处硬编码 email 或 notes 前缀会让隔离在
// 任一处漏改时静默失效，因此把判定收敛到一个函数，并由静态检查保证
// 前缀字面量不外泄使用。
//
// 对 nil 输入安全返回 false（调用方无需先判空）。
func IsInternalAgent(user *service.User) bool {
	if user == nil {
		return false
	}
	return IsInternalAgentNotes(user.Notes)
}

// IsInternalAgentNotes 判断一段 notes 文本是否属于内部账号。
// 供管理端只拿到 notes 字符串时使用（例如用户列表 DTO）。
func IsInternalAgentNotes(notes string) bool {
	return strings.HasPrefix(strings.TrimSpace(notes), InternalAgentNotesPrefix)
}

// InternalAgent 是解析出来的内部账号及其专用 Key。
type InternalAgent struct {
	User *service.User
	Key  *service.APIKey
}

// UserID 返回内部账号 ID。
func (a *InternalAgent) UserID() int64 {
	if a == nil || a.User == nil {
		return 0
	}
	return a.User.ID
}

// KeyID 返回专用 Key ID。
func (a *InternalAgent) KeyID() int64 {
	if a == nil || a.Key == nil {
		return 0
	}
	return a.Key.ID
}

// KeyString 返回专用 Key 的明文，供网关调用时放入 Authorization。
//
// 注意：这是本模块唯一读取 Key 明文的入口。上游 `api_key.key` 是明文列，
// 因此这里不做解密；返回值**绝不可**写日志或落库。
func (a *InternalAgent) KeyString() string {
	if a == nil || a.Key == nil {
		return ""
	}
	return a.Key.Key
}

// Ready 表示账号与 Key 均已就绪（可以发起检测调用）。
func (a *InternalAgent) Ready() bool {
	return a != nil && a.User != nil && a.Key != nil
}

// Active 表示内部账号当前仍为启用状态。
//
// 每次执行前必须校验：内容审核的自动封号逻辑会把用户置为 StatusDisabled，
// 若不校验会持续用被禁用的账号发起调用并静默失败。
func (a *InternalAgent) Active() bool {
	return a != nil && a.User != nil && a.User.Status == service.StatusActive
}

// InternalAgentUserStore 是内部账号所需的用户读写能力。
//
// 刻意定义为本模块内的窄接口（只有两个方法），而不是给上游
// UserRepository 增加方法：这样上游接口零改动，实现由本模块的
// repository 子包提供。
type InternalAgentUserStore interface {
	// FindByNotesPrefix 按 notes 前缀查找内部账号；不存在时返回 (nil, nil)。
	FindByNotesPrefix(ctx context.Context, prefix string) (*service.User, error)
	// Create 创建用户。邮箱已存在时返回 ErrInternalAgentAlreadyExists。
	Create(ctx context.Context, user *service.User) error
}

// ErrInternalAgentAlreadyExists 表示内部账号邮箱已被占用（通常是并发创建）。
var ErrInternalAgentAlreadyExists = errors.New("iqbenchmark: internal agent already exists")

// InternalAgentKeyStore 是内部专用 Key 所需的读写能力。
type InternalAgentKeyStore interface {
	// ListByUser 列出某用户的全部未删除 Key。
	ListByUser(ctx context.Context, userID int64) ([]service.APIKey, error)
	// Create 创建 Key 并返回明文（仅内存使用，绝不落日志）。
	Create(ctx context.Context, userID int64, name string, groupID int64) (*service.APIKey, error)
}

// LockAcquirer 提供多实例互斥的咨询锁。
type LockAcquirer interface {
	// Acquire 尝试获取锁；acquired=false 表示其他实例持有。
	Acquire(ctx context.Context) (acquired bool, release func(), err error)
}

// InternalAgentDeps 是内部账号初始化所需的依赖。
type InternalAgentDeps struct {
	Users  InternalAgentUserStore
	Keys   InternalAgentKeyStore
	Locker LockAcquirer
	// GroupExists 校验目标分组是否存在。
	GroupExists func(ctx context.Context, groupID int64) (bool, error)
	Logger      *slog.Logger
}

// EnsureInternalAgent 幂等初始化内部虚拟账号与专用 API Key。
//
// 幂等策略：
//  1. 以咨询锁串行化（多实例并发启动不会重复创建）；
//  2. 先按 notes 前缀查找账号，命中即复用；
//  3. 再按名称查找 Key，命中即复用；
//  4. 目标分组未配置时**不建 Key**，返回 ErrNoTargetGroup（功能降级为未就绪）。
//
// 失败语义：任何失败都返回错误但不 panic —— 启动路径必须只记录 WARN
// 并把功能标记为「未就绪」，绝不允许阻塞主 API 启动。
func EnsureInternalAgent(ctx context.Context, deps InternalAgentDeps) (*InternalAgent, error) {
	if deps.Users == nil || deps.Keys == nil {
		return nil, errors.New("iqbenchmark: internal agent deps are incomplete")
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	release := func() {}
	if deps.Locker != nil {
		acquired, rel, err := deps.Locker.Acquire(ctx)
		if err != nil {
			return nil, fmt.Errorf("iqbenchmark: acquire init lock: %w", err)
		}
		if !acquired {
			// 另一实例正在初始化：读取现有状态即可，不重复创建。
			logger.Debug("iqbenchmark: internal agent init locked by another instance")
			return loadInternalAgent(ctx, deps, logger)
		}
		release = rel
	}
	defer release()

	agent, err := loadInternalAgent(ctx, deps, logger)
	if err != nil && !errors.Is(err, ErrInternalAgentMissing) {
		return nil, err
	}
	if agent == nil || agent.User == nil {
		created, createErr := createInternalUser(ctx, deps, logger)
		if createErr != nil {
			return nil, createErr
		}
		agent = &InternalAgent{User: created}
	} else if agent.Key == nil {
		// 账号已存在，但 Key 缺失（分组可能在之后才被配置）。
		if err := ensureInternalKey(ctx, deps, agent, logger); err != nil {
			return agent, err
		}
		return agent, nil
	} else {
		return agent, nil
	}
	if err := ensureInternalKey(ctx, deps, agent, logger); err != nil {
		// 账号已建但 Key 未建：返回账号本身并带错误，让调用方降级为未就绪。
		return agent, err
	}
	return agent, nil
}

// loadInternalAgent 按 notes 前缀查找内部账号及其 Key。
func loadInternalAgent(ctx context.Context, deps InternalAgentDeps, logger *slog.Logger) (*InternalAgent, error) {
	user, err := deps.Users.FindByNotesPrefix(ctx, InternalAgentNotesPrefix)
	if err != nil {
		return nil, fmt.Errorf("iqbenchmark: find internal agent: %w", err)
	}
	if user == nil {
		return nil, ErrInternalAgentMissing
	}
	agent := &InternalAgent{User: user}
	keys, err := deps.Keys.ListByUser(ctx, user.ID)
	if err != nil {
		// 列表失败不致命：账号已就绪，至少可以用于状态校验。
		logger.Warn("iqbenchmark: list internal agent keys failed", "user_id", user.ID, "error", err)
		return agent, nil
	}
	// 复用与建路径相同的判定（名称匹配 + active），避免两处结论不一致。
	agent.Key = PickInternalKey(keys, InternalAgentKeyName)
	return agent, nil
}

// createInternalUser 创建内部虚拟账号。
//
// 字段组合固定且不可协商：
//   - role = user（绝不提权）
//   - status = active（ValidateKey 校验 IsActive()，disabled 会直接 401）
//   - password_hash = 一次性随机 bcrypt（明文立即丢弃，绝不写日志）
//   - notes = 前缀 + 说明（唯一权威隔离标识）
//   - signup_source 不设置（保持默认 email，避开既有 CHECK 与 Ent Validate 白名单）
func createInternalUser(ctx context.Context, deps InternalAgentDeps, logger *slog.Logger) (*service.User, error) {
	secret, err := randomSecret(32)
	if err != nil {
		return nil, fmt.Errorf("iqbenchmark: generate internal agent secret: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("iqbenchmark: hash internal agent secret: %w", err)
	}
	// 立刻清除明文引用：本函数之外不再保留任何可用凭据。
	secret = ""
	_ = secret

	user := &service.User{
		Email:        InternalAgentEmail,
		Username:     "IQ Benchmark Agent",
		Notes:        InternalAgentNotes,
		PasswordHash: string(hash),
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		Concurrency:  5,
	}
	if err := deps.Users.Create(ctx, user); err != nil {
		if errors.Is(err, ErrInternalAgentAlreadyExists) {
			// 并发创建：重新读取已存在的账号。
			existing, findErr := deps.Users.FindByNotesPrefix(ctx, InternalAgentNotesPrefix)
			if findErr != nil {
				return nil, findErr
			}
			if existing != nil {
				return existing, nil
			}
		}
		return nil, fmt.Errorf("iqbenchmark: create internal agent: %w", err)
	}
	logger.Info("iqbenchmark: internal agent created", "user_id", user.ID)
	return user, nil
}

// ensureInternalKey 为内部账号创建专用 API Key。
func ensureInternalKey(ctx context.Context, deps InternalAgentDeps, agent *InternalAgent, logger *slog.Logger) error {
	if agent == nil || agent.User == nil {
		return ErrInternalAgentMissing
	}
	keys, err := deps.Keys.ListByUser(ctx, agent.User.ID)
	if err != nil {
		return fmt.Errorf("iqbenchmark: list internal agent keys: %w", err)
	}
	for i := range keys {
		if keys[i].Name == InternalAgentKeyName && keys[i].IsActive() {
			agent.Key = &keys[i]
			return nil
		}
	}
	created, err := deps.Keys.Create(ctx, agent.User.ID, InternalAgentKeyName, 0)
	if err != nil {
		return fmt.Errorf("iqbenchmark: create internal agent key: %w", err)
	}
	agent.Key = created
	logger.Info("iqbenchmark: internal agent key created", "key_id", created.ID)
	return nil
}

// randomSecret 生成 base64url 编码的加密安全随机串。
func randomSecret(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// ActiveDirectory 是内部账号运行时状态的只读缓存。
//
// 存的是最近一次成功初始化的快照，供执行引擎在每次 run 前快速校验
// 账号是否仍为 active，避免为每次校验都查库。
type ActiveDirectory struct {
	mu    sync.RWMutex
	agent *InternalAgent
	ready bool
	// reason 记录未就绪原因（供管理端展示）。
	reason string
}

// NewActiveDirectory 构造一个空的目录。
func NewActiveDirectory() *ActiveDirectory {
	return &ActiveDirectory{}
}

// Set 记录初始化结果。
func (d *ActiveDirectory) Set(agent *InternalAgent, reason string) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.agent = agent
	d.ready = agent != nil && agent.Ready()
	d.reason = reason
}

// Snapshot 返回当前快照与就绪状态。
func (d *ActiveDirectory) Snapshot() (*InternalAgent, bool) {
	if d == nil {
		return nil, false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.agent, d.ready
}

// Reason 返回未就绪原因。
func (d *ActiveDirectory) Reason() string {
	if d == nil {
		return ""
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.reason
}
