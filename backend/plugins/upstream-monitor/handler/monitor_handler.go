package handler

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/service"
	"github.com/gin-gonic/gin"
)

// MonitorHandler handles HTTP requests for the upstream monitor plugin.
type MonitorHandler struct {
	fetcher    *service.UpstreamInfoFetcher
	aggregator *service.BalanceAggregator
	analyzer   *service.ErrorAnalyzer
	predictor  *service.UsagePredictor
	config     *config.UpstreamMonitorPluginConfig
}

// NewMonitorHandler creates a new monitor handler.
func NewMonitorHandler(
	fetcher *service.UpstreamInfoFetcher,
	aggregator *service.BalanceAggregator,
	analyzer *service.ErrorAnalyzer,
	predictor *service.UsagePredictor,
	config *config.UpstreamMonitorPluginConfig,
) *MonitorHandler {
	return &MonitorHandler{
		fetcher:    fetcher,
		aggregator: aggregator,
		analyzer:   analyzer,
		predictor:  predictor,
		config:     config,
	}
}

// RegisterRoutes registers all routes for the monitor handler.
func (h *MonitorHandler) RegisterRoutes(router *gin.RouterGroup) {
	// Overview
	router.GET("/overview", h.GetOverview)

	// Account management
	router.GET("/accounts", h.ListAccounts)
	router.GET("/accounts/:id", h.GetAccountDetail)
	router.POST("/accounts/:id/refresh", h.RefreshBalance)

	// Error analysis
	router.GET("/accounts/:id/errors", h.GetErrorStats)

	// Usage analysis
	router.GET("/accounts/:id/usage", h.GetUsageAnalysis)

	// Prediction
	router.GET("/accounts/:id/prediction", h.GetPrediction)

	// Batch operations
	router.POST("/refresh-all", h.RefreshAllBalances)
}

// GetOverview returns an overview of all upstream accounts.
func (h *MonitorHandler) GetOverview(c *gin.Context) {
	ctx := c.Request.Context()

	// Get enabled accounts from config
	accounts := h.getEnabledAccounts()
	if len(accounts) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"total_balance":     0,
			"total_accounts":    0,
			"by_type":           gin.H{},
			"low_balance_count": 0,
			"error_count_24h":   0,
		})
		return
	}

	// Aggregate balances
	result := h.aggregator.Aggregate(ctx, accounts)

	// Count low balance accounts
	lowBalanceCount := 0
	for _, info := range result.ByAccount {
		if info.Balance < h.config.Alerts.LowBalanceThreshold {
			lowBalanceCount++
		}
	}

	// Get error count for last 24 hours (from all accounts)
	errorCount := 0
	for accountID := range result.ByAccount {
		// Get account type from config
		accountType := ""
		for _, acc := range accounts {
			if acc.ID == accountID {
				accountType = acc.UpstreamType
				break
			}
		}
		if accountType == "" {
			continue
		}

		stats, err := h.analyzer.GetErrorStats(ctx, accountType, accountID, 1)
		if err == nil && stats != nil {
			errorCount += stats.TotalErrors
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_balance":     result.TotalBalance,
		"total_accounts":    len(result.ByAccount),
		"by_type":           result.ByType,
		"low_balance_count": lowBalanceCount,
		"error_count_24h":   errorCount,
		"errors":            result.Errors,
	})
}

// getEnabledAccounts returns a list of enabled accounts from config
func (h *MonitorHandler) getEnabledAccounts() []service.AccountInfo {
	accounts := make([]service.AccountInfo, 0)
	for _, acc := range h.config.Accounts {
		if acc.Enabled {
			accounts = append(accounts, service.AccountInfo{
				ID:           acc.ID,
				UpstreamType: acc.Type,
				ApiKey:       acc.ApiKey,
			})
		}
	}
	return accounts
}

// ListAccounts lists all upstream accounts.
func (h *MonitorHandler) ListAccounts(c *gin.Context) {
	// Query parameters
	platform := c.Query("platform") // "sub2api" or "nexapi"
	status := c.Query("status")     // "active", "inactive", etc.

	// Get all accounts from config
	accounts := h.config.Accounts

	// Build response
	result := make([]gin.H, 0)
	for _, acc := range accounts {
		// Apply filters
		if platform != "" && acc.Type != platform {
			continue
		}
		if status != "" && acc.Enabled && status != "active" {
			continue
		}
		if status != "" && !acc.Enabled && status != "inactive" {
			continue
		}

		accountStatus := "active"
		if !acc.Enabled {
			accountStatus = "inactive"
		}

		result = append(result, gin.H{
			"id":          acc.ID,
			"name":        acc.Name,
			"type":        acc.Type,
			"status":      accountStatus,
			"description": acc.Description,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"accounts": result,
		"total":    len(result),
	})
}

// GetAccountDetail returns detailed information for a specific account.
func (h *MonitorHandler) GetAccountDetail(c *gin.Context) {
	_ = c.Request.Context() // ctx reserved for future use
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	// Find account in config
	var account *config.UpstreamMonitorAccountConfig
	for i := range h.config.Accounts {
		if h.config.Accounts[i].ID == id {
			account = &h.config.Accounts[i]
			break
		}
	}

	if account == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	// Fetch current info
	info, err := h.fetcher.FetchInfo(account.ID, account.Type, account.ApiKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	status := "active"
	if !account.Enabled {
		status = "inactive"
	}

	c.JSON(http.StatusOK, gin.H{
		"id":           info.AccountID,
		"name":         info.Name,
		"type":         info.Type,
		"balance":      info.Balance,
		"concurrency":  info.Concurrency,
		"status":       status,
		"description":  account.Description,
		"last_updated": nil, // Would need to track this in database
	})
}

// RefreshBalance refreshes the balance for a specific account.
func (h *MonitorHandler) RefreshBalance(c *gin.Context) {
	_ = c.Request.Context() // ctx reserved for future use
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	// Find account in config
	var account *config.UpstreamMonitorAccountConfig
	for i := range h.config.Accounts {
		if h.config.Accounts[i].ID == id {
			account = &h.config.Accounts[i]
			break
		}
	}

	if account == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	if !account.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Account is disabled"})
		return
	}

	// Fetch current balance
	info, err := h.fetcher.FetchInfo(account.ID, account.Type, account.ApiKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"account_id":   info.AccountID,
		"balance":      info.Balance,
		"refreshed_at": nil, // Would track in database
	})
}

// GetErrorStats returns error statistics for a specific account.
func (h *MonitorHandler) GetErrorStats(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	days := 7
	if daysStr := c.Query("days"); daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}

	// Find account in config to get type
	var accountType string
	for _, acc := range h.config.Accounts {
		if acc.ID == id {
			accountType = acc.Type
			break
		}
	}

	if accountType == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	stats, err := h.analyzer.GetErrorStats(ctx, accountType, id, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetUsageAnalysis returns usage analysis for a specific account.
func (h *MonitorHandler) GetUsageAnalysis(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	days := 30
	if daysStr := c.Query("days"); daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}

	// Find account in config to get type
	var accountType string
	for _, acc := range h.config.Accounts {
		if acc.ID == id {
			accountType = acc.Type
			break
		}
	}

	if accountType == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	snapshots, err := h.predictor.Snapshots(ctx, accountType, id, days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	stats := gin.H{"account_id": id, "days": days, "total_requests": 0, "total_cost": 0.0, "average_daily_cost": 0.0, "daily_usage": []gin.H{}}
	if len(snapshots) > 1 {
		first, last := snapshots[0], snapshots[len(snapshots)-1]
		cost := first.Balance - last.Balance
		if cost < 0 {
			cost = 0
		}
		stats["total_cost"] = cost
		stats["average_daily_cost"] = cost / float64(days)
	}
	c.JSON(http.StatusOK, stats)
}

// GetPrediction returns usage prediction for a specific account.
func (h *MonitorHandler) GetPrediction(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	// Find account in config to get type
	var accountType string
	for _, acc := range h.config.Accounts {
		if acc.ID == id {
			accountType = acc.Type
			break
		}
	}

	if accountType == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	prediction, err := h.predictor.PredictFromHistory(ctx, accountType, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, prediction)
}

// RefreshAllBalances refreshes balance for all accounts.
func (h *MonitorHandler) RefreshAllBalances(c *gin.Context) {
	ctx := c.Request.Context()

	// Get enabled accounts from config
	accounts := h.getEnabledAccounts()
	if len(accounts) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"refreshed_count": 0,
			"failed_count":    0,
			"total_balance":   0,
		})
		return
	}

	// Aggregate balances
	result := h.aggregator.Aggregate(ctx, accounts)

	c.JSON(http.StatusOK, gin.H{
		"refreshed_count": len(result.ByAccount),
		"failed_count":    len(result.Errors),
		"total_balance":   result.TotalBalance,
		"errors":          result.Errors,
	})
}
