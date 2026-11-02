package iqbenchmark

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// SettingKeyIQBenchmark 是本功能在主 settings 表中的配置键。
const SettingKeyIQBenchmark = "iq_benchmark_settings"

// settingsCacheTTL 是进程内配置缓存的有效期。
//
// 60 秒与既有 panel_rate_limit_settings 的做法一致：既避免每次请求读库，
// 又保证管理员改完配置后不需要重启服务。
const settingsCacheTTL = 60 * time.Second

// 默认值（同时作为配置校验的边界基准）。
const (
	defaultIntervalMinMinutes    = 15
	defaultIntervalMaxMinutes    = 10080
	defaultModelTimeoutSeconds   = 300
	defaultMaxArtifactBytes      = int64(8 << 20) // 8 MiB
	defaultMaxArtifactsPerRun    = 20
	defaultMaxTaskConcurrency    = 4
	defaultRetentionDays         = 30
	defaultMaxArtifacts          = 5000
	defaultCleanupIntervalMinute = 60

	// 硬上限：即使管理员填了更大的值也不允许超过，防止误配置放大风险。
	hardMaxArtifactsPerRun = 100
	hardMaxConcurrency     = 32
	hardMaxArtifactBytes   = int64(64 << 20) // 64 MiB

	// 用户自测默认值。
	defaultUserTestDailyLimit    = 5
	defaultUserTestMaxConcurrent = 1
	// 硬上限：日次数上限压到 200，避免管理员误填 100000 让单用户打爆网关。
	hardMaxUserTestDailyLimit = 200
	// 自测并发上限压到 4：自测是用户点出来的，不需要更高。
	hardMaxUserTestConcurrency = 4
)

// RateLimitTierSettings 是单档限流配置。
type RateLimitTierSettings struct {
	// Limit 是每分钟允许的请求数；0 表示不限制。
	Limit int `json:"limit"`
}

// IQGalleryRateLimitSettings 是作品展只读接口的限流配置。
//
// 两档叠加：heavy 请求同时消耗 read 与 heavy，避免只打重接口绕过轻档。
type IQGalleryRateLimitSettings struct {
	// ReadLimitPerMinute 用于列表与聚合类接口。
	ReadLimitPerMinute int `json:"read_limit_per_minute"`
	// HeavyLimitPerMinute 用于详情等重负载接口。
	HeavyLimitPerMinute int `json:"heavy_limit_per_minute"`
	// MaxConcurrent 是单用户在途重负载请求上限。
	MaxConcurrent int `json:"max_concurrent"`
}

// DefaultIQGalleryRateLimitSettings 返回默认限流配置。
func DefaultIQGalleryRateLimitSettings() IQGalleryRateLimitSettings {
	return IQGalleryRateLimitSettings{
		ReadLimitPerMinute:  120,
		HeavyLimitPerMinute: 60,
		MaxConcurrent:       defaultMaxTaskConcurrency,
	}
}

// Normalize 把缺省或越界的值夹回合法区间。
func (s IQGalleryRateLimitSettings) Normalize() IQGalleryRateLimitSettings {
	if s.ReadLimitPerMinute <= 0 {
		s.ReadLimitPerMinute = DefaultIQGalleryRateLimitSettings().ReadLimitPerMinute
	}
	if s.HeavyLimitPerMinute <= 0 {
		s.HeavyLimitPerMinute = DefaultIQGalleryRateLimitSettings().HeavyLimitPerMinute
	}
	if s.MaxConcurrent <= 0 {
		s.MaxConcurrent = defaultMaxTaskConcurrency
	}
	if s.MaxConcurrent > hardMaxConcurrency {
		s.MaxConcurrent = hardMaxConcurrency
	}
	return s
}

// Settings 是本功能的完整配置。
//
// 全部字段存于既有 settings 表（JSON），不改 config.yaml 与 config.Config，
// 与 panel_rate_limit_settings / channel_monitor_v2 的既有做法一致。
type Settings struct {
	// Enabled 是总开关，默认 false：关闭时零副作用（无调度、无账号、无出站）。
	Enabled bool `json:"enabled"`
	// DefaultGroupID 是内部专用 Key 绑定的目标分组。
	DefaultGroupID int64 `json:"default_group_id"`
	// InternalGatewayBaseURL 为空时回退为 http://127.0.0.1:<Server.Port>。
	InternalGatewayBaseURL string `json:"internal_gateway_base_url"`

	IntervalMinMinutes  int `json:"interval_min_minutes"`
	IntervalMaxMinutes  int `json:"interval_max_minutes"`
	ModelTimeoutSeconds int `json:"model_timeout_seconds"`

	MaxArtifactBytes   int64 `json:"max_artifact_bytes"`
	MaxArtifactsPerRun int   `json:"max_artifacts_per_run"`
	MaxTaskConcurrency int   `json:"max_task_concurrency"`

	// DailyCostBudget 是检测日成本上限；0 表示不限制。
	DailyCostBudget float64 `json:"daily_cost_budget"`
	// MinBalanceFloor 是内部账号余额下限；0 表示不限制。
	MinBalanceFloor float64 `json:"min_balance_floor"`

	// ScoringEnabled 控制是否展示人工评分入口。
	ScoringEnabled bool `json:"scoring_enabled"`
	// LLMJudgeEnabled 是 LLM 裁判总开关。首版不实现裁判，字段恒为 false。
	LLMJudgeEnabled bool `json:"llm_judge_enabled"`

	// ExposeCostToUser 控制公开侧是否下发单条成本，默认 false。
	ExposeCostToUser bool `json:"expose_cost_to_user"`
	// ExposeTokensToUser 控制公开侧是否下发 token 数，默认 false。
	ExposeTokensToUser bool `json:"expose_tokens_to_user"`

	RetentionDays          int `json:"retention_days"`
	MaxArtifacts           int `json:"max_artifacts"`
	CleanupIntervalMinutes int `json:"cleanup_interval_minutes"`

	RateLimit IQGalleryRateLimitSettings `json:"rate_limit"`

	// ---- 用户自测（用自己本站 API Key 一键检测）----

	// UserTestEnabled 是自测总开关，默认 false。
	// 关闭时 /iq-gallery/my-tests 等路由不注册（与功能总开关同样的 404 语义）。
	UserTestEnabled bool `json:"user_test_enabled"`
	// UserTestDailyLimit 是单用户每日自测次数上限（按自然日，本地时区）。
	UserTestDailyLimit int `json:"user_test_daily_limit"`
	// UserTestMaxConcurrent 是单用户在途自测请求上限。
	UserTestMaxConcurrent int `json:"user_test_max_concurrent"`
	// UserTestRequireReview 决定自测产出是否先进入待审核（pending）。
	// 默认 true：公开作品展是任何人都能看的页面，先审后发是更安全的默认。
	UserTestRequireReview bool `json:"user_test_require_review"`
	// UserTestDailyCostBudget 是单用户每日自测成本上限（USD）。
	// 0 表示不限制（用户裁决：默认不限制，允许用户自行收紧）。
	UserTestDailyCostBudget float64 `json:"user_test_daily_cost_budget"`
}

// DefaultSettings 返回默认配置：功能关闭、限额保守。
func DefaultSettings() Settings {
	return Settings{
		Enabled:                false,
		DefaultGroupID:         0,
		InternalGatewayBaseURL: "",
		IntervalMinMinutes:     defaultIntervalMinMinutes,
		IntervalMaxMinutes:     defaultIntervalMaxMinutes,
		ModelTimeoutSeconds:    defaultModelTimeoutSeconds,
		MaxArtifactBytes:       defaultMaxArtifactBytes,
		MaxArtifactsPerRun:     defaultMaxArtifactsPerRun,
		MaxTaskConcurrency:     defaultMaxTaskConcurrency,
		DailyCostBudget:        0,
		MinBalanceFloor:        0,
		ScoringEnabled:         true,
		LLMJudgeEnabled:        false,
		ExposeCostToUser:       false,
		ExposeTokensToUser:     false,
		RetentionDays:          defaultRetentionDays,
		MaxArtifacts:           defaultMaxArtifacts,
		CleanupIntervalMinutes: defaultCleanupIntervalMinute,
		RateLimit:              DefaultIQGalleryRateLimitSettings(),

		// 自测默认关闭：与总开关同样的「默认零副作用」原则。
		UserTestEnabled:         false,
		UserTestDailyLimit:      defaultUserTestDailyLimit,
		UserTestMaxConcurrent:   defaultUserTestMaxConcurrent,
		UserTestRequireReview:   true,
		UserTestDailyCostBudget: 0, // 0 = 不限制（用户裁决）
	}
}

// Normalize 把越界配置夹回合法区间，并填上缺失的默认值。
//
// 这是「管理员填错不应放大风险」的兜底：即使有人在 JSON 里写了
// max_artifacts_per_run = 100000，实际生效值也会被压回硬上限。
func (s Settings) Normalize() Settings {
	def := DefaultSettings()
	if s.IntervalMinMinutes < defaultIntervalMinMinutes {
		s.IntervalMinMinutes = defaultIntervalMinMinutes
	}
	if s.IntervalMaxMinutes < s.IntervalMinMinutes {
		s.IntervalMaxMinutes = s.IntervalMinMinutes
	}
	if s.IntervalMaxMinutes > defaultIntervalMaxMinutes {
		s.IntervalMaxMinutes = defaultIntervalMaxMinutes
	}
	if s.ModelTimeoutSeconds <= 0 {
		s.ModelTimeoutSeconds = def.ModelTimeoutSeconds
	}
	if s.ModelTimeoutSeconds > 900 {
		s.ModelTimeoutSeconds = 900
	}
	if s.MaxArtifactBytes <= 0 {
		s.MaxArtifactBytes = def.MaxArtifactBytes
	}
	if s.MaxArtifactBytes > hardMaxArtifactBytes {
		s.MaxArtifactBytes = hardMaxArtifactBytes
	}
	if s.MaxArtifactsPerRun <= 0 {
		s.MaxArtifactsPerRun = def.MaxArtifactsPerRun
	}
	if s.MaxArtifactsPerRun > hardMaxArtifactsPerRun {
		s.MaxArtifactsPerRun = hardMaxArtifactsPerRun
	}
	if s.MaxTaskConcurrency <= 0 {
		s.MaxTaskConcurrency = def.MaxTaskConcurrency
	}
	if s.MaxTaskConcurrency > hardMaxConcurrency {
		s.MaxTaskConcurrency = hardMaxConcurrency
	}
	if s.DailyCostBudget < 0 {
		s.DailyCostBudget = 0
	}
	if s.MinBalanceFloor < 0 {
		s.MinBalanceFloor = 0
	}
	if s.RetentionDays < 0 {
		s.RetentionDays = 0
	}
	if s.RetentionDays > 3650 {
		s.RetentionDays = 3650
	}
	if s.MaxArtifacts < 0 {
		s.MaxArtifacts = 0
	}
	if s.CleanupIntervalMinutes <= 0 {
		s.CleanupIntervalMinutes = def.CleanupIntervalMinutes
	}
	if s.CleanupIntervalMinutes < 5 {
		s.CleanupIntervalMinutes = 5
	}
	s.RateLimit = s.RateLimit.Normalize()
	// 首版不实现 LLM 裁判：无论配置怎么写，执行侧都不启用。
	s.LLMJudgeEnabled = false

	// 自测：0 在 DailyLimit / MaxConcurrent 上不是「不限制」而是「没填」，
	// 因此回落默认值（与 DailyCostBudget 的 0=不限制语义刻意区分）。
	if s.UserTestDailyLimit <= 0 {
		s.UserTestDailyLimit = def.UserTestDailyLimit
	}
	if s.UserTestDailyLimit > hardMaxUserTestDailyLimit {
		s.UserTestDailyLimit = hardMaxUserTestDailyLimit
	}
	if s.UserTestMaxConcurrent <= 0 {
		s.UserTestMaxConcurrent = def.UserTestMaxConcurrent
	}
	if s.UserTestMaxConcurrent > hardMaxUserTestConcurrency {
		s.UserTestMaxConcurrent = hardMaxUserTestConcurrency
	}
	if s.UserTestDailyCostBudget < 0 {
		s.UserTestDailyCostBudget = 0
	}
	return s
}

// MarshalJSON 保证落库前已归一化，避免非法值被写进 settings 表。
func (s Settings) MarshalJSON() ([]byte, error) {
	type alias Settings
	return json.Marshal(alias(s.Normalize()))
}

// SettingsStore 是读取/写入配置的最小能力（由既有 settings 表实现）。
type SettingsStore interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string) error
}

// SettingsProvider 提供带缓存的配置读取。
type SettingsProvider struct {
	store  SettingsStore
	logger *slog.Logger

	mu        sync.RWMutex
	cached    Settings
	expiresAt time.Time
	loaded    bool
}

// NewSettingsProvider 构造配置提供者。
func NewSettingsProvider(store SettingsStore, logger *slog.Logger) *SettingsProvider {
	if logger == nil {
		logger = slog.Default()
	}
	return &SettingsProvider{store: store, logger: logger}
}

// Get 返回当前配置（带 60 秒进程内缓存）。
//
// 读取失败时沿用上一次的有效值（冷启动时用默认值），绝不因配置读取
// 失败而拒绝全部请求或放大限制。
func (p *SettingsProvider) Get(ctx context.Context) Settings {
	if p == nil {
		return DefaultSettings()
	}
	p.mu.RLock()
	if p.loaded && time.Now().Before(p.expiresAt) {
		cached := p.cached
		p.mu.RUnlock()
		return cached
	}
	p.mu.RUnlock()

	settings, ok := p.load(ctx)

	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	if !ok {
		// 读取失败：沿用旧值（有则用，无则默认），并短暂缓存以打散重试。
		if !p.loaded {
			p.cached = DefaultSettings()
			p.loaded = true
		}
		p.expiresAt = now.Add(settingsCacheTTL / 6)
		return p.cached
	}
	p.cached = settings
	p.loaded = true
	p.expiresAt = now.Add(settingsCacheTTL)
	return p.cached
}

// load 从 settings 表读取并解析配置。
func (p *SettingsProvider) load(ctx context.Context) (Settings, bool) {
	if p.store == nil {
		return DefaultSettings(), false
	}
	raw, err := p.store.Get(ctx, SettingKeyIQBenchmark)
	if err != nil {
		p.logger.Warn("iqbenchmark: read settings failed, keeping previous value", "error", err)
		return DefaultSettings(), false
	}
	if strings.TrimSpace(raw) == "" {
		return DefaultSettings(), true
	}
	var settings Settings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		p.logger.Warn("iqbenchmark: settings payload is malformed, using defaults", "error", err)
		return DefaultSettings(), true
	}
	return settings.Normalize(), true
}

// Update 写入配置并立即失效缓存（管理员改开关后即时生效）。
func (p *SettingsProvider) Update(ctx context.Context, settings Settings) (Settings, error) {
	normalized := settings.Normalize()
	payload, err := json.Marshal(normalized)
	if err != nil {
		return Settings{}, err
	}
	if p.store == nil {
		return Settings{}, ErrFeatureDisabled
	}
	if err := p.store.Set(ctx, SettingKeyIQBenchmark, string(payload)); err != nil {
		return Settings{}, err
	}
	p.Invalidate()
	return normalized, nil
}

// Invalidate 立即失效缓存，使下一次读取穿透到数据库。
func (p *SettingsProvider) Invalidate() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loaded = false
	p.expiresAt = time.Time{}
}

// IsEnabled 是便捷判断：功能是否启用。
func (p *SettingsProvider) IsEnabled(ctx context.Context) bool {
	return p.Get(ctx).Enabled
}
