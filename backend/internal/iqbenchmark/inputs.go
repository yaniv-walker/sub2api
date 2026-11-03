package iqbenchmark

import "time"

// 本文件集中定义仓储入参与聚合结构，供 repository 与 service 两侧共用，
// 避免 service 反向依赖 repository（保持依赖方向单一）。

// CaseCreateInput 是新建题目的入参。
type CaseCreateInput struct {
	Name         string
	Kind         string
	Prompt       string
	SystemPrompt string
	Params       map[string]any
	ExpectKind   string
	// Expected 是 logic 类题目的参考答案；空表示本题不可判分。
	Expected        string
	Scoring         string
	LLMJudgeEnabled bool
	Builtin         bool
	Enabled         bool
	AllowUserTest   bool
	SortOrder       int
}

// CaseUpdateInput 是全量更新题目的入参（builtin 不可通过此处修改）。
type CaseUpdateInput struct {
	Name         string
	Kind         string
	Prompt       string
	SystemPrompt string
	Params       map[string]any
	ExpectKind   string
	// Expected 传空串表示「改为不可判分」，仓库层会清成 NULL。
	Expected        string
	Scoring         string
	LLMJudgeEnabled bool
	Enabled         bool
	AllowUserTest   bool
	SortOrder       int
}

// TaskCreateInput 是新建任务的入参。
type TaskCreateInput struct {
	Name            string
	GroupID         int64
	Model           string
	CaseIDs         []int64
	IntervalMinutes int
	JitterSeconds   int
	Enabled         bool
	NextRunAt       *time.Time
}

// TaskUpdateInput 是全量更新任务业务字段的入参。
type TaskUpdateInput struct {
	Name            string
	GroupID         int64
	Model           string
	CaseIDs         []int64
	IntervalMinutes int
	JitterSeconds   int
	Enabled         bool
	NextRunAt       *time.Time
}

// RunCreateInput 是新建执行记录的入参。
type RunCreateInput struct {
	TaskID           int64
	GroupID          int64
	Model            string
	Trigger          string
	StartedAt        time.Time
	InternalUserID   int64
	InternalAPIKeyID int64
}

// RunCounts 是 run 结项时的汇总计数。
type RunCounts struct {
	Success         int
	Failed          int
	Timeout         int
	Blocked         int
	TotalDurationMS int64
	// LogicTotal / LogicPassed 是判定口径的计数，与上面四项（调用口径）并行。
	// 纯生成模式（只出图）下 LogicTotal 为 0。
	LogicTotal  int
	LogicPassed int
}

// ArtifactCreateInput 是写入一条产出的入参。
type ArtifactCreateInput struct {
	// RunID / TaskID 传 0 表示「无」（写入 NULL），用于用户自测产出。
	RunID            int64
	TaskID           int64
	CaseID           int64
	CaseSnapshot     map[string]any
	GroupID          int64
	Model            string
	RequestParams    map[string]any
	RenderKind       string
	ImageURL         string
	Content          string
	ContentTruncated bool
	ContentSHA256    string
	UnsafeFlags      []string
	RawResponseRef   string
	DurationMS       int
	InputTokens      int
	OutputTokens     int
	Cost             float64
	Status           string
	// Verdict / RenderVerdict / ErrorKind 见 types.go 的常量说明。
	// 空值由服务层按 Status 兜底推导，保证永不落空。
	Verdict       string
	RenderVerdict string
	ErrorKind     string
	ErrorCode     string
	ErrorSummary  string
	BlobKey       string
	// Source 为 scheduled / user_test；空值按 scheduled 处理（定时链路的默认）。
	Source string
	// TriggeredByUserID 传 0 表示 NULL（定时链路）。
	TriggeredByUserID int64
	// ModerationStatus 空值交由数据库默认（approved）。
	ModerationStatus string
}

// PurgedArtifact 是被清理的产出的最小信息。
type PurgedArtifact struct {
	ID      int64
	RunID   int64
	BlobKey string
}

// OverviewCounts 是作品展总览的计数部分。
type OverviewCounts struct {
	TotalArtifacts int64
	ImageArtifacts int64
	TextArtifacts  int64
	CaseCount      int64
	TaskCount      int64
	RunningTasks   int64
}

// OverviewBucketRow 是「分组 × 模型」聚合的原始行（分组名由上层补齐）。
type OverviewBucketRow struct {
	GroupID      int64
	Model        string
	Total        int64
	SuccessCount int64
	FailedCount  int64
	AvgDurationM float64
	LastRunAt    *time.Time
}
