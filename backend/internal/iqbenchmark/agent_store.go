package iqbenchmark

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// AgentUserStore 用上游 service.UserRepository 实现内部账号的读写。
//
// 依赖的是**上游在 service 包中声明的接口**（service.UserRepository），
// 而不是 internal/repository 的具体实现：
//   - 这样本模块不 import internal/repository，符合分层约定；
//   - 上游接口零改动（不新增方法），实现由既有 userRepository 提供。
//
// 已知取舍：上游没有「按 notes 前缀查询」的能力，因此必须分页扫描。
// 内部账号是全站唯一且最早创建的用户之一，正常第一页即命中。
type AgentUserStore struct {
	Repo service.UserRepository
	// PageSize 是扫描页大小（默认 200）。
	PageSize int
	// MaxPages 是扫描上限（默认 50 页 ≈ 1 万用户），防止异常数据下无限翻页。
	MaxPages int
}

// NewAgentUserStore 构造内部账号用户存储。
func NewAgentUserStore(repo service.UserRepository) *AgentUserStore {
	return &AgentUserStore{Repo: repo, PageSize: 200, MaxPages: 50}
}

var _ InternalAgentUserStore = (*AgentUserStore)(nil)

// FindByNotesPrefix 按 notes 前缀查找内部账号；不存在返回 (nil, nil)。
func (s *AgentUserStore) FindByNotesPrefix(ctx context.Context, prefix string) (*service.User, error) {
	if s == nil || s.Repo == nil {
		return nil, errors.New("iqbenchmark: user repository is not available")
	}
	pageSize := s.PageSize
	if pageSize <= 0 {
		pageSize = 200
	}
	maxPages := s.MaxPages
	if maxPages <= 0 {
		maxPages = 50
	}
	for page := 1; page <= maxPages; page++ {
		users, result, err := s.Repo.List(ctx, pagination.PaginationParams{
			Page:      page,
			PageSize:  pageSize,
			SortOrder: pagination.SortOrderAsc,
		})
		if err != nil {
			return nil, fmt.Errorf("iqbenchmark: list users: %w", err)
		}
		for i := range users {
			if IsInternalAgentNotes(users[i].Notes) {
				found := users[i]
				return &found, nil
			}
		}
		if result == nil || page >= result.Pages || len(users) == 0 {
			return nil, nil
		}
	}
	return nil, nil
}

// Create 创建内部账号。
//
// 刻意走 Repo.Create（而非 CreateWithEmailAliasGuard）：后者带注册路径的
// 收件箱别名锁，内部账号用不到，且会引入额外的锁等待。
// 邮箱重复时由数据库唯一索引报错，调用方可据此判定「已被并发创建」。
func (s *AgentUserStore) Create(ctx context.Context, user *service.User) error {
	if s == nil || s.Repo == nil {
		return errors.New("iqbenchmark: user repository is not available")
	}
	if user == nil {
		return errors.New("iqbenchmark: user must not be nil")
	}
	return s.Repo.Create(ctx, user)
}

// AgentKeyStore 用上游 service.APIKeyRepository 实现内部专用 Key 的读写。
type AgentKeyStore struct {
	Repo service.APIKeyRepository
}

// NewAgentKeyStore 构造内部 Key 存储。
func NewAgentKeyStore(repo service.APIKeyRepository) *AgentKeyStore {
	return &AgentKeyStore{Repo: repo}
}

var _ InternalAgentKeyStore = (*AgentKeyStore)(nil)

// ListByUser 列出某用户的全部 Key（分页取尽）。
func (s *AgentKeyStore) ListByUser(ctx context.Context, userID int64) ([]service.APIKey, error) {
	if s == nil || s.Repo == nil {
		return nil, errors.New("iqbenchmark: api key repository is not available")
	}
	const pageSize = 100
	const maxPages = 10
	found := make([]service.APIKey, 0, 4)
	for page := 1; page <= maxPages; page++ {
		keys, result, err := s.Repo.ListByUserID(ctx, userID, pagination.PaginationParams{
			Page:      page,
			PageSize:  pageSize,
			SortOrder: pagination.SortOrderAsc,
		}, service.APIKeyListFilters{})
		if err != nil {
			return nil, fmt.Errorf("iqbenchmark: list api keys: %w", err)
		}
		found = append(found, keys...)
		if result == nil || page >= result.Pages || len(keys) == 0 {
			break
		}
	}
	return found, nil
}

// Create 创建内部专用 Key 并返回含明文的记录。
//
// 与上游 APIKeyService.Create 的差别：这里不校验「用户能否绑定该分组」，
// 因为内部账号的 AllowedGroups 在创建时就已按目标分组写好（见 createInternalUser）。
func (s *AgentKeyStore) Create(ctx context.Context, userID int64, name string, groupID int64) (*service.APIKey, error) {
	if s == nil || s.Repo == nil {
		return nil, errors.New("iqbenchmark: api key repository is not available")
	}
	key, err := generateAPIKey()
	if err != nil {
		return nil, fmt.Errorf("iqbenchmark: generate api key: %w", err)
	}
	record := &service.APIKey{
		UserID: userID,
		Name:   name,
		Key:    key,
		Status: service.StatusActive,
	}
	if groupID > 0 {
		gid := groupID
		record.GroupID = &gid
	}
	if err := s.Repo.Create(ctx, record); err != nil {
		return nil, fmt.Errorf("iqbenchmark: create api key: %w", err)
	}
	return record, nil
}

// FindByID 读取单个 Key（用于校验复用到的 Key 是否仍有效）。
func (s *AgentKeyStore) FindByID(ctx context.Context, id int64) (*service.APIKey, error) {
	if s == nil || s.Repo == nil {
		return nil, errors.New("iqbenchmark: api key repository is not available")
	}
	return s.Repo.GetByID(ctx, id)
}

// PickInternalKey 在 Key 列表里挑选内部专用 Key。
//
// 只按名称判定，不要求分组匹配：管理员可能为内部账号改绑分组，
// 按名称识别可避免改绑后旧 Key 变成孤儿、又新建一把重复 Key。
//
// 跳过非 active 的 Key：`ValidateKey` 会拒绝 disabled/过期 Key，
// 选中它等于让检测流量必然 401。导出以便 internal_agent.go 复用同一判定，
// 避免「读路径」和「建路径」对同一个 Key 得出不同结论。
func PickInternalKey(keys []service.APIKey, name string) *service.APIKey {
	for i := range keys {
		if !keys[i].IsActive() {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(keys[i].Name), name) {
			return &keys[i]
		}
	}
	return nil
}

// apiKeyPrefix 是内部 Key 的前缀，与上游默认值保持一致（`sk-`）。
//
// 不读 cfg.Default.APIKeyPrefix：内部账号的 Key 只需能被网关识别，
// 与用户可见的自定义前缀无关；固定前缀也便于运维肉眼区分。
const apiKeyPrefix = "sk-"

// generateAPIKey 生成 32 字节随机的 API Key（64 位十六进制 + 前缀）。
//
// 复用上游同一套生成规则（随机源、长度、编码），但独立实现，
// 以避免依赖上游 *APIKeyService 的构造（它需要 cfg 与多个仓储）。
func generateAPIKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return apiKeyPrefix + hex.EncodeToString(raw), nil
}
