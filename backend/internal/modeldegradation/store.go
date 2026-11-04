package modeldegradation

import (
	"context"
	"time"
)

// Store 是本模块需要的全部数据访问能力。
//
// 与 iqbenchmark 同样的做法：一个接口覆盖全部实体，实现由 repository 提供，
// 单测用内存实现替换，这样 service 层不必依赖数据库。
type Store interface {
	// ── 题库 ──
	ListTasks(ctx context.Context, enabledOnly bool) ([]Task, error)
	GetTaskByKey(ctx context.Context, taskType, key string) (*Task, error)

	// ── 运行 ──
	CreateRun(ctx context.Context, in RunCreateInput) (*Run, error)
	// InsertItems 批量写入单题结果（发起时先占位，执行中更新）。
	InsertItems(ctx context.Context, runID int64, items []Item) error
	// UpdateItem 更新单题执行结果。
	UpdateItem(ctx context.Context, itemID int64, in ItemResultInput) error
	// FinishRun 落终态并写入聚合判定。
	FinishRun(ctx context.Context, id int64, in RunFinishInput) error
	// CancelRun 把运行标记为 cancelled（仅在 running 时生效，返回是否命中）。
	CancelRun(ctx context.Context, id int64, at time.Time) (bool, error)
	GetRun(ctx context.Context, id int64) (*Run, error)
	GetRunDetail(ctx context.Context, id int64) (*RunDetail, error)
	ListRuns(ctx context.Context, f RunFilter) ([]Run, int64, error)
	GetStats(ctx context.Context, f RunFilter) (Stats, error)

	// ── 可见性 ──
	GetVisibility(ctx context.Context) (*Visibility, error)
	SetGroupHidden(ctx context.Context, groupID int64, hidden bool) error
	SetHostHidden(ctx context.Context, host string, hidden bool) error
	// ListVisibleHosts 返回未命中分组的上游主机及其记录数（供管理页展示）。
	ListUnmatchedHosts(ctx context.Context) ([]HostUsage, error)
}

// RunCreateInput 是新建运行的输入。
//
// 注意这里**没有** APIKey / BaseURL 原值：凭据只在执行期存在，
// 落库的只有脱敏展示形态。这条约束由结构体本身保证 —— 想写也写不进去。
type RunCreateInput struct {
	UserID     int64
	OwnerEmail string

	BaseURLDisplay       string
	ModelID              string
	Protocol             string
	ReasoningEffort      string
	RenderTimeoutSeconds int

	GroupID   int64
	GroupName string
}

// ItemResultInput 是单题执行结果的更新内容。
type ItemResultInput struct {
	Response   string
	Passed     bool
	Verdict    string
	Detail     string
	Artwork    string
	DurationMS int64
}

// RunFinishInput 是落终态时的聚合结果。
type RunFinishInput struct {
	Status        string
	Verdict       string
	LogicPassed   int
	LogicTotal    int
	RenderVerdict string
	ErrorMessage  string
	ErrorKind     string
	DurationMS    int64
	CompletedAt   time.Time
}
