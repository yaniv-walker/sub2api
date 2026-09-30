package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// UpstreamInfoFetcher fetches information from upstream providers.
type UpstreamInfoFetcher struct {
	httpClient *http.Client
	logger     *slog.Logger
}

const DefaultNexAPIBalanceDivider = 500000.0

func DefaultBalanceDivider(upstreamType string) float64 {
	if strings.EqualFold(strings.TrimSpace(upstreamType), "sub2api") {
		return 1
	}
	return DefaultNexAPIBalanceDivider
}

// NewUpstreamInfoFetcher creates a new upstream info fetcher.
func NewUpstreamInfoFetcher(logger *slog.Logger) *UpstreamInfoFetcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &UpstreamInfoFetcher{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger.With("component", "upstream_info_fetcher"),
	}
}

// UpstreamInfo contains information about an upstream account.
type UpstreamInfo struct {
	AccountID   int64   `json:"account_id"`
	Name        string  `json:"name"`
	Type        string  `json:"type"` // "sub2api" or "nexapi"
	Balance     float64 `json:"balance"`
	Concurrency int     `json:"concurrency"`
	Status      string  `json:"status"`
	ApiKey      string  `json:"-"` // Hidden from JSON
}

// FetchInfo fetches upstream information for a given account.
func (f *UpstreamInfoFetcher) FetchInfo(accountID int64, upstreamType, baseURL, apiKey string) (*UpstreamInfo, error) {
	return f.FetchInfoWithBalanceDivider(accountID, upstreamType, baseURL, apiKey, DefaultBalanceDivider(upstreamType))
}

func (f *UpstreamInfoFetcher) FetchInfoWithBalanceDivider(accountID int64, upstreamType, baseURL, apiKey string, quotaDivider float64) (*UpstreamInfo, error) {
	f.logger.Debug("Fetching upstream info",
		"account_id", accountID,
		"upstream_type", upstreamType,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	rootURL, err := NormalizeUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if quotaDivider <= 0 {
		quotaDivider = DefaultBalanceDivider(upstreamType)
	}

	switch upstreamType {
	case "sub2api":
		return f.fetchSub2APIInfo(ctx, accountID, rootURL, apiKey)
	case "nexapi":
		return f.fetchNexAPIInfo(ctx, accountID, rootURL, apiKey, quotaDivider)
	default:
		return nil, fmt.Errorf("unsupported upstream type: %s", upstreamType)
	}
}

// NormalizeUpstreamBaseURL reduces an account's model endpoint to the shared
// upstream origin. Accounts with the same origin belong to the same upstream.
func NormalizeUpstreamBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid upstream base_url %q", raw)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

// Sub2APIResponse represents the API-key self-service usage response.
type Sub2APIResponse struct {
	Mode      string   `json:"mode"`
	IsValid   bool     `json:"isValid"`
	Status    string   `json:"status"`
	PlanName  string   `json:"planName"`
	Balance   *float64 `json:"balance"`
	Remaining *float64 `json:"remaining"`
	Quota     *struct {
		Remaining float64 `json:"remaining"`
	} `json:"quota"`
}

// fetchSub2APIInfo fetches information from Sub2API.
func (f *UpstreamInfoFetcher) fetchSub2APIInfo(ctx context.Context, accountID int64, rootURL, apiKey string) (*UpstreamInfo, error) {
	f.logger.Debug("Fetching Sub2API info", "account_id", accountID)

	url := rootURL + "/v1/usage"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var apiResp Sub2APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !apiResp.IsValid {
		return nil, fmt.Errorf("Sub2API reports API key as invalid")
	}
	balance := 0.0
	switch {
	case apiResp.Balance != nil:
		balance = *apiResp.Balance
	case apiResp.Remaining != nil:
		balance = *apiResp.Remaining
	case apiResp.Quota != nil:
		balance = apiResp.Quota.Remaining
	}
	status := apiResp.Status
	if status == "" && apiResp.IsValid {
		status = "active"
	}

	info := &UpstreamInfo{
		AccountID:   accountID,
		Name:        apiResp.PlanName,
		Type:        "sub2api",
		Balance:     balance,
		Concurrency: 0,
		Status:      status,
		ApiKey:      apiKey,
	}

	f.logger.Info("Sub2API info fetched successfully",
		"account_id", accountID,
		"balance", info.Balance,
		"status", info.Status,
	)

	return info, nil
}

// NexAPIResponse represents the response from NexAPI balance query
type NexAPIResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Balance        *float64 `json:"balance"`         // Direct balance, when exposed by the deployment
		Quota          int64    `json:"quota"`           // 总配额
		UsedQuota      int64    `json:"used_quota"`      // 已用配额
		RemainingQuota int64    `json:"remaining_quota"` // 剩余配额
		Name           string   `json:"name"`            // 账号名称
		Status         int      `json:"status"`          // 1=active, 0=inactive
	} `json:"data"`
}

// fetchNexAPIInfo fetches information from NexAPI.
func (f *UpstreamInfoFetcher) fetchNexAPIInfo(ctx context.Context, accountID int64, rootURL, apiKey string, quotaDivider float64) (*UpstreamInfo, error) {
	f.logger.Debug("Fetching NexAPI info", "account_id", accountID)

	// NexAPI API endpoint
	url := rootURL + "/api/user/self"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var apiResp NexAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: %s", apiResp.Msg)
	}

	// NexAPI's self endpoint exposes quota in its internal unit. The plugin's
	// configured divider converts that total quota to the displayed balance.
	// Do not use balance, used_quota, or remaining_quota: deployments may expose
	// those fields with different semantics or units.
	balanceCNY := float64(apiResp.Data.Quota) / quotaDivider

	status := nexAPIStatus(apiResp.Data.Status)

	info := &UpstreamInfo{
		AccountID:   accountID,
		Name:        apiResp.Data.Name,
		Type:        "nexapi",
		Balance:     balanceCNY,
		Concurrency: 0, // NexAPI doesn't report concurrency limit
		Status:      status,
		ApiKey:      apiKey,
	}

	f.logger.Info("NexAPI info fetched successfully",
		"account_id", accountID,
		"balance", info.Balance,
		"quota", apiResp.Data.Quota,
		"quota_divider", quotaDivider,
		"status", info.Status,
	)

	return info, nil
}

func nexAPIStatus(status int) string {
	if status == 1 {
		return "active"
	}
	return "inactive"
}
