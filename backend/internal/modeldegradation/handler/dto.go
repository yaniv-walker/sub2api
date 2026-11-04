package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/modeldegradation"
)

// 本文件定义对外 DTO。
//
// 一条不可动摇的约束：**凭据绝不出现在任何 DTO 里**。
// 没有 api_key 字段，也没有原始 base_url —— 只有脱敏展示形态。
// 这不是靠「记得别填」来保证，而是 DTO 里根本没有这个字段可填。

// TaskDTO 是题库里的一道题。
type TaskDTO struct {
	TaskType string `json:"task_type"`
	Key      string `json:"key"`
	Title    string `json:"title"`
	Prompt   string `json:"prompt"`
	Expected string `json:"expected"`
}

// TaskBankDTO 是 GET /tasks 的响应。
type TaskBankDTO struct {
	Logic  []TaskDTO `json:"logic"`
	Render []TaskDTO `json:"render"`
}

// toTaskDTO 转换单题。
//
// prompt 对 render 题是模板（含 {scene}/{code} 占位符）——
// 前端展示时不必替换，真正替换发生在发起时。
func toTaskDTO(t modeldegradation.Task) TaskDTO {
	return TaskDTO{
		TaskType: t.Type,
		Key:      t.Key,
		Title:    t.Title,
		Prompt:   t.Prompt,
		Expected: t.Expected,
	}
}

// toTaskBankDTO 转换题库。
func toTaskBankDTO(bank *modeldegradation.TaskBank) TaskBankDTO {
	out := TaskBankDTO{Logic: []TaskDTO{}, Render: []TaskDTO{}}
	if bank == nil {
		return out
	}
	for _, t := range bank.Logic {
		out.Logic = append(out.Logic, toTaskDTO(t))
	}
	for _, t := range bank.Render {
		out.Render = append(out.Render, toTaskDTO(t))
	}
	return out
}

// ItemDTO 是单题结果。
type ItemDTO struct {
	TaskType string `json:"task_type"`
	TaskKey  string `json:"task_key"`
	Prompt   string `json:"prompt"`
	Expected string `json:"expected"`
	Response string `json:"response"`
	Passed   bool   `json:"passed"`
	Verdict  string `json:"verdict"`
	Detail   string `json:"detail"`
	Artwork  string `json:"artwork,omitempty"`
	Duration int64  `json:"duration_ms"`
}

// toItemDTO 转换单题结果。
func toItemDTO(it modeldegradation.Item) ItemDTO {
	return ItemDTO{
		TaskType: it.Type,
		TaskKey:  it.Key,
		Prompt:   it.Prompt,
		Expected: it.Expected,
		Response: it.Response,
		Passed:   it.Passed,
		Verdict:  it.Verdict,
		Detail:   it.Detail,
		Artwork:  it.Artwork,
		Duration: it.DurationMS,
	}
}

// RunDTO 是运行记录（不含单题）。
type RunDTO struct {
	ID       int64  `json:"id"`
	BaseURL  string `json:"base_url"`
	ModelID  string `json:"model_id"`
	Protocol string `json:"protocol"`
	// ReasoningEffort 空串表示未指定。
	ReasoningEffort      string `json:"reasoning_effort"`
	RenderTimeoutSeconds int    `json:"render_timeout_seconds"`

	Status        string `json:"status"`
	Verdict       string `json:"verdict"`
	LogicPassed   int    `json:"logic_passed"`
	LogicTotal    int    `json:"logic_total"`
	RenderVerdict string `json:"render_verdict"`

	ErrorMessage string `json:"error_message"`
	ErrorKind    string `json:"error_kind"`

	DurationMS  int64      `json:"duration_ms"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at"`
	Retrying    bool       `json:"retrying"`

	// OwnerEmail 已脱敏（形如 1***@qq.com）。
	OwnerEmail string `json:"owner_email"`
	GroupID    int64  `json:"group_id"`
	GroupName  string `json:"group_name"`
	IsOwner    bool   `json:"is_owner"`

	// ArtworkPreview 是首个生成题的作品源码，供历史墙缩略图使用。
	// 逻辑题检测或作品过大时为空串。
	ArtworkPreview string `json:"artwork_preview"`
}

// toRunDTO 转换运行记录。
func toRunDTO(r modeldegradation.Run) RunDTO {
	return RunDTO{
		ID:                   r.ID,
		BaseURL:              r.BaseURLDisplay,
		ModelID:              r.ModelID,
		Protocol:             r.Protocol,
		ReasoningEffort:      r.ReasoningEffort,
		RenderTimeoutSeconds: r.RenderTimeoutSeconds,
		Status:               r.Status,
		Verdict:              r.Verdict,
		LogicPassed:          r.LogicPassed,
		LogicTotal:           r.LogicTotal,
		RenderVerdict:        r.RenderVerdict,
		ErrorMessage:         r.ErrorMessage,
		ErrorKind:            r.ErrorKind,
		DurationMS:           r.DurationMS,
		CreatedAt:            r.CreatedAt,
		CompletedAt:          r.CompletedAt,
		Retrying:             r.Retrying,
		OwnerEmail:           r.OwnerEmail,
		GroupID:              r.GroupID,
		GroupName:            r.GroupName,
		IsOwner:              r.IsOwner,
		ArtworkPreview:       r.ArtworkPreview,
	}
}

// RunDetailDTO 是运行记录 + 单题明细。
type RunDetailDTO struct {
	RunDTO
	Items []ItemDTO `json:"items"`
}

// toRunDetailDTO 转换详情。
func toRunDetailDTO(d *modeldegradation.RunDetail) RunDetailDTO {
	out := RunDetailDTO{RunDTO: toRunDTO(d.Run), Items: []ItemDTO{}}
	for _, it := range d.Items {
		out.Items = append(out.Items, toItemDTO(it))
	}
	return out
}

// RunListDTO 是列表响应。
type RunListDTO struct {
	Items []RunDTO `json:"items"`
	Total int64    `json:"total"`
}

// StatsDTO 是顶部统计。
type StatsDTO struct {
	PassRate       float64 `json:"pass_rate"`
	LastDurationMS int64   `json:"last_duration_ms"`
	RunCount       int64   `json:"run_count"`
}

// ModelsRequest 是 POST /models 的请求体。
//
// 这是唯一接收用户凭据的请求体之一，它不被持久化。
type ModelsRequest struct {
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Protocol string `json:"protocol"`
}

// VisibilityDTO 是管理端可见性配置。
type VisibilityDTO struct {
	HiddenGroupIDs []int64   `json:"hidden_group_ids"`
	HiddenHosts    []string  `json:"hidden_hosts"`
	Hosts          []HostDTO `json:"hosts"`
}

// HostDTO 是未命中分组的上游主机。
type HostDTO struct {
	Host     string `json:"host"`
	RunCount int64  `json:"run_count"`
}

// SetGroupHiddenRequest 是 PUT /history-visibility/groups/:id 的请求体。
type SetGroupHiddenRequest struct {
	Hidden bool `json:"hidden"`
}

// SetHostHiddenRequest 是 PUT /history-visibility/hosts 的请求体。
type SetHostHiddenRequest struct {
	Host   string `json:"host"`
	Hidden bool   `json:"hidden"`
}
