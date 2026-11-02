// Package service 实现 iqbenchmark 的业务逻辑（题目、任务、执行、产出、熔断）。
//
// 分层约定：
//   - 本包只依赖 internal/iqbenchmark 的类型与窄接口，不直接依赖 Ent 或 *sql.DB；
//   - 数据访问通过 Store 接口注入（由 repository 子包实现），
//     这样业务逻辑可在单测里用内存实现替换；
//   - 不 import internal/repository，避免触发 depguard 的 service↛repository 约束。
package service

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/iqbenchmark"
)

// 默认与边界常量（与 repository 层的 CHECK 约束保持一致）。
const (
	MinIntervalMinutes = 15
	MaxIntervalMinutes = 10080
	// minEffectiveIntervalSeconds 是最小实际间隔（15 分钟）。
	// 抖动不得把它压到更低：interval*60 - jitter >= 900。
	minEffectiveIntervalSeconds = 900

	defaultListPageSize = 20
	// MaxPublicPageSize 是公开列表单页上限：超过直接 400，不静默截断。
	MaxPublicPageSize = 50
)

// ErrCasePageSizeTooLarge 等错误由 handler 层映射为 HTTP 状态码。
var (
	ErrInvalidKind        = errors.New("iqbenchmark: invalid case kind")
	ErrInvalidExpectKind  = errors.New("iqbenchmark: invalid expect kind")
	ErrInvalidScoring     = errors.New("iqbenchmark: invalid scoring mode")
	ErrEmptyName          = errors.New("iqbenchmark: name must not be empty")
	ErrEmptyPrompt        = errors.New("iqbenchmark: prompt must not be empty")
	ErrKindExpectMismatch = errors.New("iqbenchmark: case kind and expect kind do not match")
	ErrIntervalTooSmall   = errors.New("iqbenchmark: interval_minutes is below the minimum")
	ErrIntervalTooLarge   = errors.New("iqbenchmark: interval_minutes is above the maximum")
	ErrJitterNegative     = errors.New("iqbenchmark: jitter_seconds must not be negative")
	ErrJitterTooLarge     = errors.New("iqbenchmark: jitter_seconds reduces the minimum interval below 900 seconds")
)

// Store 是本模块需要的全部数据访问能力。
//
// 用一个接口覆盖四类实体，是为了让 service 层只依赖一个注入点；
// 实现由 repository 子包提供，单测可用内存实现替换。
type Store interface {
	// Case
	CreateCase(ctx context.Context, in CaseCreateInput) (*iqbenchmark.Case, error)
	UpdateCase(ctx context.Context, id int64, in CaseUpdateInput) (*iqbenchmark.Case, error)
	GetCase(ctx context.Context, id int64) (*iqbenchmark.Case, error)
	ListCases(ctx context.Context, enabledOnly bool) ([]iqbenchmark.Case, error)
	ListCasesByIDs(ctx context.Context, ids []int64) (map[int64]iqbenchmark.Case, error)
	DeleteCase(ctx context.Context, id int64) error
	CountEnabledTasksReferencingCase(ctx context.Context, caseID int64) (int, error)
	CountTasksReferencingCase(ctx context.Context, caseID int64) (int, error)
	RemoveCaseFromDisabledTasks(ctx context.Context, caseID int64) error

	// Task
	CreateTask(ctx context.Context, in TaskCreateInput) (*iqbenchmark.Task, error)
	UpdateTask(ctx context.Context, id int64, in TaskUpdateInput) (*iqbenchmark.Task, error)
	GetTask(ctx context.Context, id int64) (*iqbenchmark.Task, error)
	ListTasks(ctx context.Context) ([]iqbenchmark.Task, error)
	DeleteTask(ctx context.Context, id int64) error
	SetTaskEnabled(ctx context.Context, id int64, enabled bool) error
	SetTaskStatus(ctx context.Context, id int64, status, reason string, nextRunAt *time.Time) error
	SetTaskNextRunAt(ctx context.Context, id int64, next time.Time) error
	MarkTaskRunFinished(ctx context.Context, id int64, finishedAt time.Time, status string, nextRunAt *time.Time) error

	// Run
	CreateRun(ctx context.Context, in RunCreateInput) (*iqbenchmark.Run, error)
	FinishRun(ctx context.Context, id int64, finishedAt time.Time, status string, counts RunCounts) error
	GetRun(ctx context.Context, id int64) (*iqbenchmark.Run, error)
	ListRunsByTask(ctx context.Context, taskID int64, page, pageSize int) ([]iqbenchmark.Run, int64, error)
	SetRunCost(ctx context.Context, id int64, cost float64, inputTokens, outputTokens int64, collectedAt time.Time) error
	DailyCost(ctx context.Context, apiKeyID int64, since time.Time) (float64, error)

	// Artifact
	CreateArtifact(ctx context.Context, in ArtifactCreateInput) (*iqbenchmark.Artifact, error)
	GetArtifact(ctx context.Context, id int64) (*iqbenchmark.Artifact, error)
	ListArtifacts(ctx context.Context, f iqbenchmark.ArtifactListFilter) (*iqbenchmark.ArtifactListResult, error)
	ListArtifactsByRun(ctx context.Context, runID int64) ([]iqbenchmark.Artifact, error)
	ScoreArtifact(ctx context.Context, id int64, score float64, scoredBy int64, scoredAt time.Time) error
	// ModerateArtifact 写入审核结论。reviewedBy 为 0 表示系统判定。
	ModerateArtifact(ctx context.Context, id int64, status string, reviewedBy int64, reviewedAt time.Time) error
	SoftDeleteArtifact(ctx context.Context, id int64) (string, error)
	CountLiveArtifacts(ctx context.Context) (int64, error)
	PurgeOlderThan(ctx context.Context, cutoff time.Time, limit int) ([]PurgedArtifact, error)
	PurgeBeyondLimit(ctx context.Context, keep int, limit int) ([]PurgedArtifact, error)
	PurgeRunsWithoutArtifacts(ctx context.Context) (int64, error)
	HardDeleteArtifactsSoftDeletedBefore(ctx context.Context, cutoff time.Time) (int64, error)
	HardDeleteRunsSoftDeletedBefore(ctx context.Context, cutoff time.Time) (int64, error)

	// Overview
	// GetOverviewCounts / GetOverviewBuckets 的 source 参数为空表示全量口径。
	// 统计口径必须由仓储层强制执行（而不是让每个调用方各自过滤），
	// 否则漏一处就会让「按来源统计」静默失真。
	GetOverviewCounts(ctx context.Context, source string) (OverviewCounts, error)
	GetOverviewBuckets(ctx context.Context, limit int, source string) ([]OverviewBucketRow, error)

	// UserTest
	// 用户自测的请求侧状态机（产出仍写 artifacts）。
	CreateUserTest(ctx context.Context, in iqbenchmark.UserTestCreateInput) (*iqbenchmark.UserTest, error)
	GetUserTest(ctx context.Context, id int64) (*iqbenchmark.UserTest, error)
	ListUserTestsByUser(ctx context.Context, userID int64, page, pageSize int) ([]iqbenchmark.UserTest, int64, error)
	// FinishUserTest 落终态；artifactID 为 0 表示本次没有产出。
	FinishUserTest(ctx context.Context, id int64, status, errorCode, errorSummary string, artifactID int64, actualCost float64) error
	// UserTestQuota 一次返回当日次数/在途数/当日成本，用于发起前预检。
	UserTestQuota(ctx context.Context, userID int64, since time.Time) (iqbenchmark.UserTestQuotaSnapshot, error)
	// CountUserTestDedupe 统计去重窗口内的相同请求数。
	CountUserTestDedupe(ctx context.Context, dedupeKey string, since time.Time) (int, error)
}

// CaseCreateInput、TaskCreateInput 等直接复用主包的入参类型，
// 不在此处再定义一套同构结构（重复定义会让两侧字段漂移）。
type (
	CaseCreateInput     = iqbenchmark.CaseCreateInput
	CaseUpdateInput     = iqbenchmark.CaseUpdateInput
	TaskCreateInput     = iqbenchmark.TaskCreateInput
	TaskUpdateInput     = iqbenchmark.TaskUpdateInput
	RunCreateInput      = iqbenchmark.RunCreateInput
	RunCounts           = iqbenchmark.RunCounts
	ArtifactCreateInput = iqbenchmark.ArtifactCreateInput
	PurgedArtifact      = iqbenchmark.PurgedArtifact
	OverviewCounts      = iqbenchmark.OverviewCounts
	OverviewBucketRow   = iqbenchmark.OverviewBucketRow

	// 配置类型同样集中在主包，service 侧只做别名，避免两套同构结构漂移。
	Settings                   = iqbenchmark.Settings
	SettingsProvider           = iqbenchmark.SettingsProvider
	IQGalleryRateLimitSettings = iqbenchmark.IQGalleryRateLimitSettings
)

// DefaultSettings 转发主包默认配置。
func DefaultSettings() Settings { return iqbenchmark.DefaultSettings() }

// validateInterval 校验间隔与抖动。
//
// 语义与既有 channel_monitor 一致：
//   - interval 必须在 [15, 10080] 分钟；
//   - jitter >= 0；
//   - interval*60 - jitter >= 900（抖动不得把最小实际间隔压到 15 分钟以下）。
func validateInterval(intervalMinutes, jitterSeconds int) error {
	if intervalMinutes < MinIntervalMinutes {
		return ErrIntervalTooSmall
	}
	if intervalMinutes > MaxIntervalMinutes {
		return ErrIntervalTooLarge
	}
	if jitterSeconds < 0 {
		return ErrJitterNegative
	}
	if intervalMinutes*60-jitterSeconds < minEffectiveIntervalSeconds {
		return ErrJitterTooLarge
	}
	return nil
}

// nextRunAt 计算下一次执行时间。
//
// 抖动在 [-jitter, +jitter] 区间内均匀取样，最终结果夹紧到
// >= now + 900 秒，保证任何配置下都不会出现过于密集的调度。
func nextRunAt(now time.Time, intervalMinutes, jitterSeconds int) time.Time {
	base := now.Add(time.Duration(intervalMinutes) * time.Minute)
	if jitterSeconds <= 0 {
		if earliest := now.Add(minEffectiveIntervalSeconds * time.Second); base.Before(earliest) {
			return earliest
		}
		return base
	}
	span := int64(2*jitterSeconds + 1)
	n, err := rand.Int(rand.Reader, big.NewInt(span))
	offset := int64(0)
	if err == nil {
		offset = n.Int64() - int64(jitterSeconds)
	}
	next := base.Add(time.Duration(offset) * time.Second)
	if earliest := now.Add(minEffectiveIntervalSeconds * time.Second); next.Before(earliest) {
		return earliest
	}
	return next
}
