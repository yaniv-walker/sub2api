package model

import (
	"time"
)

// UpstreamInfo represents aggregated information about an upstream account.
type UpstreamInfo struct {
	AccountID         int64               `json:"account_id"`
	Name              string              `json:"name"`
	UpstreamType      string              `json:"upstream_type"` // "sub2api", "nexapi", etc.
	BaseURL           string              `json:"base_url"`
	Balance           float64             `json:"balance"`           // In CNY
	Currency          string              `json:"currency"`          // Always "CNY"
	ConcurrencyLimit  int                 `json:"concurrency_limit"` // 0 = unlimited
	Status            string              `json:"status"`            // "active", "disabled", "error"
	LastChecked       time.Time           `json:"last_checked"`
	GroupAssociations []GroupAssociation  `json:"group_associations"`
	ErrorStats        *ErrorStats         `json:"error_stats,omitempty"`
	UsageStats        *UsageStats         `json:"usage_stats,omitempty"`
	Prediction        *BalancePrediction  `json:"prediction,omitempty"`
	Extra             map[string]interface{} `json:"extra,omitempty"`
}

// GroupAssociation represents the mapping between local groups and upstream groups.
type GroupAssociation struct {
	LocalGroupID    int64  `json:"local_group_id"`
	LocalGroupName  string `json:"local_group_name"`
	UpstreamGroupID string `json:"upstream_group_id,omitempty"`
	UpstreamGroupName string `json:"upstream_group_name,omitempty"`
}

// ErrorStats contains error statistics for an upstream account.
type ErrorStats struct {
	TotalErrors      int64              `json:"total_errors"`
	ErrorRate        float64            `json:"error_rate"` // Percentage (0-1)
	ErrorsByType     map[string]int64   `json:"errors_by_type"`
	RecentErrors     []ErrorRecord      `json:"recent_errors"`
	LastErrorTime    *time.Time         `json:"last_error_time,omitempty"`
}

// ErrorRecord represents a single error occurrence.
type ErrorRecord struct {
	ID           int64     `json:"id"`
	AccountID    int64     `json:"account_id"`
	ErrorType    string    `json:"error_type"`    // "rate_limit", "server_error", "timeout"
	ErrorCode    string    `json:"error_code"`    // "429", "500", "503"
	ErrorMessage string    `json:"error_message"`
	RequestID    string    `json:"request_id"`
	CreatedAt    time.Time `json:"created_at"`
}

// UsageStats contains usage statistics for an upstream account.
type UsageStats struct {
	TotalRequests    int64              `json:"total_requests"`
	TotalCost        float64            `json:"total_cost"` // In CNY
	AverageDailyCost float64            `json:"average_daily_cost"`
	DailyUsage       []DailyUsage       `json:"daily_usage"`
	PeakHours        []int              `json:"peak_hours"` // Hours with most requests (0-23)
}

// DailyUsage represents usage for a single day.
type DailyUsage struct {
	Date         string  `json:"date"` // YYYY-MM-DD
	RequestCount int64   `json:"request_count"`
	Cost         float64 `json:"cost"` // In CNY
}

// BalancePrediction contains balance prediction information.
type BalancePrediction struct {
	CurrentBalance     float64   `json:"current_balance"`
	DailyBurnRate      float64   `json:"daily_burn_rate"` // Average daily cost
	EstimatedDaysLeft  int       `json:"estimated_days_left"`
	EstimatedDepletionDate *time.Time `json:"estimated_depletion_date,omitempty"`
	Confidence         float64   `json:"confidence"` // 0-1, prediction confidence
	Algorithm          string    `json:"algorithm"`  // Algorithm used
}

// OverviewStats represents the overview statistics for all upstream accounts.
type OverviewStats struct {
	TotalAccounts       int     `json:"total_accounts"`
	ActiveAccounts      int     `json:"active_accounts"`
	TotalBalance        float64 `json:"total_balance"` // In CNY
	TotalDailyBurnRate  float64 `json:"total_daily_burn_rate"`
	TotalEstimatedDays  int     `json:"total_estimated_days"`
	AccountsByType      map[string]int `json:"accounts_by_type"`
	LowBalanceAccounts  int     `json:"low_balance_accounts"`
	ErrorAccounts       int     `json:"error_accounts"`
}

// AccountListFilter contains filters for listing accounts.
type AccountListFilter struct {
	Platform     string   `form:"platform"`      // Filter by upstream type
	Status       string   `form:"status"`        // Filter by status
	GroupID      int64    `form:"group_id"`      // Filter by local group
	LowBalance   bool     `form:"low_balance"`   // Show only low balance accounts
	HasErrors    bool     `form:"has_errors"`    // Show only accounts with errors
	Page         int      `form:"page" binding:"min=1"`
	PageSize     int      `form:"page_size" binding:"min=1,max=100"`
}

// PaginatedAccountList represents a paginated list of accounts.
type PaginatedAccountList struct {
	Items      []UpstreamInfo `json:"items"`
	Total      int64          `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalPages int            `json:"total_pages"`
}

// RefreshBalanceRequest represents a request to refresh account balance.
type RefreshBalanceRequest struct {
	Force bool `json:"force"` // Force refresh even if cache is valid
}

// RefreshBalanceResponse represents the response after refreshing balance.
type RefreshBalanceResponse struct {
	AccountID   int64     `json:"account_id"`
	Balance     float64   `json:"balance"`
	Currency    string    `json:"currency"`
	LastChecked time.Time `json:"last_checked"`
	Message     string    `json:"message,omitempty"`
}
