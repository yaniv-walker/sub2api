package iqbenchmark

import "time"

// UserTest 是一次「用户用自己本站 API Key 发起的自测」的业务记录。
//
// 它**不是**产出：产出的真相只有一个地方（iq_benchmark_artifacts）。
// 本结构只承载请求侧的状态机（配额、并发、去重、审核流转锚点），
// 与 artifacts 用 ArtifactID 单向关联 —— 即使本表被整表清空，
// 作品展的读路径也完全不受影响。
type UserTest struct {
	ID       int64
	UserID   int64
	APIKeyID int64
	CaseID   int64
	GroupID  int64
	Model    string
	// ArtifactID 为 0 表示尚未产出（失败/被拦截/仍在跑）。
	ArtifactID int64

	Status       string
	ErrorCode    string
	ErrorSummary string

	EstimatedCost float64
	ActualCost    float64

	CreatedAt time.Time
	UpdatedAt time.Time
}

// UserTestCreateInput 是创建一条自测记录所需的入参。
type UserTestCreateInput struct {
	UserID        int64
	APIKeyID      int64
	CaseID        int64
	GroupID       int64
	Model         string
	EstimatedCost float64
	DedupeKey     string
}

// UserTestQuotaSnapshot 是「当前占用」的一次快照，用于发起前的配额预检。
//
// 刻意做成一次查询返回三个计数（而不是三次 COUNT）：
// 预检需要三者同时成立，分开查会让判定基于不同时刻的状态。
type UserTestQuotaSnapshot struct {
	// TodayCount 是当日已发起的次数（含失败与被拒）。
	TodayCount int
	// InFlightCount 是当前 pending/running 的数量。
	InFlightCount int
	// TodayCost 是当日已发生成本（estimated 与 actual 的较大值累加）。
	TodayCost float64
}

// UserTestSubmitInput 是提交自测的完整入参（handler 已做完审核与 Key 校验）。
type UserTestSubmitInput struct {
	UserID   int64
	APIKeyID int64
	CaseID   int64
	GroupID  int64
	Model    string
	// GatewayKey 是用户自己 Key 的明文，只在内存中透传给本次执行。
	GatewayKey string
	// RequireReview 决定产出初始审核状态（来自设置）。
	RequireReview bool
	// EstimatedCost 是本题的预估成本，用于日预算预检。
	EstimatedCost float64
}
