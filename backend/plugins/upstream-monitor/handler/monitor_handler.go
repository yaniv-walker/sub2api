package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/ent/upstreammonitorupstream"
	"github.com/Wei-Shaw/sub2api/internal/config"
	hostservice "github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/service"
	"github.com/gin-gonic/gin"
)

// MonitorHandler handles HTTP requests for the upstream monitor plugin.
type MonitorHandler struct {
	fetcher    *service.UpstreamInfoFetcher
	aggregator *service.BalanceAggregator
	analyzer   *service.ErrorAnalyzer
	predictor  *service.UsagePredictor
	accounts   AccountProvider
	upstreams  UpstreamProvider
	encryptor  hostservice.SecretEncryptor
	config     *config.UpstreamMonitorPluginConfig
}

// NewMonitorHandler creates a new monitor handler.
func NewMonitorHandler(
	fetcher *service.UpstreamInfoFetcher,
	aggregator *service.BalanceAggregator,
	analyzer *service.ErrorAnalyzer,
	predictor *service.UsagePredictor,
	accounts AccountProvider,
	upstreams UpstreamProvider,
	encryptor hostservice.SecretEncryptor,
	config *config.UpstreamMonitorPluginConfig,
) *MonitorHandler {
	return &MonitorHandler{
		fetcher:    fetcher,
		aggregator: aggregator,
		analyzer:   analyzer,
		predictor:  predictor,
		accounts:   accounts,
		upstreams:  upstreams,
		encryptor:  encryptor,
		config:     config,
	}
}

// RegisterRoutes registers all routes for the monitor handler.
func (h *MonitorHandler) RegisterRoutes(router *gin.RouterGroup) {
	// Overview
	router.GET("/overview", h.GetOverview)
	router.GET("/upstreams", h.ListUpstreams)
	router.PUT("/upstreams", h.ConfigureUpstream)

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

	upstreams, err := h.loadMonitorUpstreams(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	accounts := monitorUpstreamInfos(upstreams)
	totalAccounts := 0
	for _, upstream := range upstreams {
		if upstream.Enabled {
			totalAccounts += len(upstream.Accounts)
		}
	}
	if len(accounts) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"total_balance":     0,
			"total_accounts":    0,
			"total_upstreams":   0,
			"by_type":           gin.H{},
			"by_upstream":       gin.H{},
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
		"total_accounts":    totalAccounts,
		"total_upstreams":   len(result.ByUpstream),
		"by_type":           result.ByType,
		"by_upstream":       h.upstreamBalanceDetails(upstreams, result),
		"low_balance_count": lowBalanceCount,
		"error_count_24h":   errorCount,
		"errors":            result.Errors,
	})
}

func (h *MonitorHandler) upstreamBalanceDetails(upstreams []monitorUpstream, result *service.AggregatedBalance) []gin.H {
	details := make([]gin.H, 0, len(result.ByUpstream))
	for _, upstream := range upstreams {
		balance, ok := result.ByUpstream[upstream.BaseURL]
		if !ok {
			continue
		}
		details = append(details, gin.H{
			"id": upstream.ID, "name": upstream.Name, "base_url": upstream.BaseURL,
			"type": balance.UpstreamType, "balance": balance.Balance,
			"account_count": len(upstream.Accounts), "account_id": balance.AccountID,
		})
	}
	return details
}

// loadActiveAccounts reads administrator-managed accounts from the host repository.
func (h *MonitorHandler) loadActiveAccounts(ctx context.Context) ([]monitorAccount, error) {
	upstreams, err := h.loadMonitorUpstreams(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]monitorAccount, 0)
	for _, upstream := range upstreams {
		if upstream.Enabled {
			result = append(result, upstream.Accounts...)
		}
	}
	return result, nil
}

func (h *MonitorHandler) loadMonitorUpstreams(ctx context.Context) ([]monitorUpstream, error) {
	accounts, err := h.accounts.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	configs, err := h.upstreams.List(ctx)
	if err != nil {
		return nil, err
	}
	grouped := groupMonitorAccounts(monitorAccounts(accounts), configs)
	if err := h.applyMonitoringTokens(grouped); err != nil {
		return nil, err
	}
	return grouped, nil
}

func (h *MonitorHandler) applyMonitoringTokens(grouped []monitorUpstream) error {
	for i := range grouped {
		if len(grouped[i].Accounts) == 0 {
			continue
		}
		configuredToken := grouped[i].PersonalAccessToken
		if configuredToken == nil {
			configuredToken = grouped[i].Passkey
		}
		if configuredToken == nil {
			configuredToken = grouped[i].AccessToken
		}
		if configuredToken == nil {
			continue
		}
		token, err := h.encryptor.Decrypt(*configuredToken)
		if err != nil {
			return fmt.Errorf("decrypt monitoring token for %s: %w", grouped[i].BaseURL, err)
		}
		grouped[i].Accounts[0].info.ApiKey = token
	}
	return nil
}
func monitorInfos(accounts []monitorAccount) []service.AccountInfo {
	result := make([]service.AccountInfo, 0, len(accounts))
	for _, account := range accounts {
		result = append(result, account.info)
	}
	return result
}
func (h *MonitorHandler) loadAccount(ctx context.Context, id int64) (*monitorAccount, error) {
	account, err := h.accounts.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	info, ok := monitorAccountInfo(account)
	if !ok {
		return nil, hostservice.ErrAccountNotFound
	}
	configs, err := h.upstreams.List(ctx)
	if err != nil {
		return nil, err
	}
	grouped := groupMonitorAccounts([]monitorAccount{{account: account, info: info}}, configs)
	if err := h.applyMonitoringTokens(grouped); err != nil {
		return nil, err
	}
	if len(grouped) != 1 || !grouped[0].Enabled {
		return nil, hostservice.ErrAccountNotFound
	}
	return &grouped[0].Accounts[0], nil
}

// ListUpstreams returns discovered upstream origins with plugin-owned settings.
func (h *MonitorHandler) ListUpstreams(c *gin.Context) {
	upstreams, err := h.loadMonitorUpstreams(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	result := make([]gin.H, 0, len(upstreams))
	for _, upstream := range upstreams {
		accountIDs := make([]int64, 0, len(upstream.Accounts))
		for _, account := range upstream.Accounts {
			accountIDs = append(accountIDs, account.account.ID)
		}
		result = append(result, gin.H{
			"id": upstream.ID, "name": upstream.Name, "base_url": upstream.BaseURL,
			"type": upstream.Type, "configured": upstream.Configured, "enabled": upstream.Enabled,
			"account_count": len(accountIDs), "account_ids": accountIDs,
			"has_access_token":          upstream.AccessToken != nil,
			"has_personal_access_token": upstream.PersonalAccessToken != nil,
			"has_passkey":               upstream.Passkey != nil,
			"quota_divider":             upstream.QuotaDivider,
		})
	}
	c.JSON(http.StatusOK, gin.H{"upstreams": result, "total": len(result)})
}

type configureUpstreamRequest struct {
	BaseURL             string   `json:"base_url" binding:"required"`
	Name                string   `json:"name"`
	Type                string   `json:"type" binding:"required"`
	Enabled             *bool    `json:"enabled"`
	AccessToken         *string  `json:"access_token"`
	PersonalAccessToken *string  `json:"personal_access_token"`
	Passkey             *string  `json:"passkey"`
	QuotaDivider        *float64 `json:"quota_divider"`
}

// ConfigureUpstream stores type and enablement in the plugin's own table.
func (h *MonitorHandler) ConfigureUpstream(c *gin.Context) {
	var request configureUpstreamRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rootURL, err := service.NormalizeUpstreamBaseURL(request.BaseURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	typeName := normalizeUpstreamType(request.Type)
	if typeName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type must be sub2api or nexapi"})
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	quotaDivider := service.DefaultNexAPIBalanceDivider
	if request.QuotaDivider != nil {
		if *request.QuotaDivider <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "quota_divider must be greater than zero"})
			return
		}
		quotaDivider = *request.QuotaDivider
	}
	var encryptedToken, encryptedPersonalAccessToken, encryptedPasskey *string
	for _, item := range []struct {
		value  *string
		target **string
		label  string
	}{
		{request.AccessToken, &encryptedToken, "access token"},
		{request.PersonalAccessToken, &encryptedPersonalAccessToken, "personal access token"},
		{request.Passkey, &encryptedPasskey, "passkey"},
	} {
		if item.value == nil || strings.TrimSpace(*item.value) == "" {
			continue
		}
		encrypted, err := h.encryptor.Encrypt(strings.TrimSpace(*item.value))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encrypt " + item.label})
			return
		}
		*item.target = &encrypted
	}
	configured, err := h.upstreams.Upsert(c.Request.Context(), rootURL, strings.TrimSpace(request.Name), upstreammonitorupstream.UpstreamType(typeName), enabled, encryptedToken, encryptedPersonalAccessToken, encryptedPasskey, quotaDivider)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id": configured.ID, "base_url": configured.BaseURL, "name": configured.Name,
		"upstream_type": configured.UpstreamType, "enabled": configured.Enabled,
		"has_access_token":          configured.AccessToken != nil,
		"has_personal_access_token": configured.PersonalAccessToken != nil,
		"has_passkey":               configured.Passkey != nil,
		"quota_divider":             configured.QuotaDivider,
		"created_at":                configured.CreatedAt, "updated_at": configured.UpdatedAt,
	})
}

// ListAccounts lists all upstream accounts.
func (h *MonitorHandler) ListAccounts(c *gin.Context) {
	// Query parameters
	platform := c.Query("platform") // "sub2api" or "nexapi"
	status := c.Query("status")     // "active", "inactive", etc.

	managed, err := h.loadActiveAccounts(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Build response
	result := make([]gin.H, 0)
	for _, item := range managed {
		acc, info := item.account, item.info
		// Apply filters
		if platform != "" && info.UpstreamType != platform {
			continue
		}
		if status != "" && status != "active" {
			continue
		}

		result = append(result, gin.H{
			"id":          acc.ID,
			"name":        acc.Name,
			"type":        info.UpstreamType,
			"base_url":    info.BaseURL,
			"status":      "active",
			"description": acc.Notes,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"accounts": result,
		"total":    len(result),
	})
}

// GetAccountDetail returns detailed information for a specific account.
func (h *MonitorHandler) GetAccountDetail(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	managed, err := h.loadAccount(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	// Fetch current info
	info, err := h.fetcher.FetchInfo(managed.info.ID, managed.info.UpstreamType, managed.info.BaseURL, managed.info.ApiKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":           info.AccountID,
		"name":         info.Name,
		"type":         info.Type,
		"base_url":     managed.info.BaseURL,
		"balance":      info.Balance,
		"concurrency":  info.Concurrency,
		"status":       "active",
		"description":  managed.account.Notes,
		"last_updated": nil, // Would need to track this in database
	})
}

// RefreshBalance refreshes the balance for a specific account.
func (h *MonitorHandler) RefreshBalance(c *gin.Context) {
	ctx := c.Request.Context()
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid account ID"})
		return
	}

	managed, err := h.loadAccount(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	// Fetch current balance
	info, err := h.fetcher.FetchInfo(managed.info.ID, managed.info.UpstreamType, managed.info.BaseURL, managed.info.ApiKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"account_id":   info.AccountID,
		"base_url":     managed.info.BaseURL,
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

	managed, err := h.loadAccount(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	stats, err := h.analyzer.GetErrorStats(ctx, managed.info.UpstreamType, id, days)
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

	managed, err := h.loadAccount(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	snapshots, err := h.predictor.Snapshots(ctx, managed.info.UpstreamType, id, days)
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

	managed, err := h.loadAccount(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Account not found"})
		return
	}

	prediction, err := h.predictor.PredictFromHistory(ctx, managed.info.UpstreamType, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, prediction)
}

// RefreshAllBalances refreshes balance for all accounts.
func (h *MonitorHandler) RefreshAllBalances(c *gin.Context) {
	ctx := c.Request.Context()

	upstreams, err := h.loadMonitorUpstreams(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	accounts := monitorUpstreamInfos(upstreams)
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
		"refreshed_count":     len(result.ByAccount),
		"refreshed_upstreams": len(result.ByUpstream),
		"failed_count":        len(result.Errors),
		"total_balance":       result.TotalBalance,
		"by_upstream":         h.upstreamBalanceDetails(upstreams, result),
		"errors":              result.Errors,
	})
}
