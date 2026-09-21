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

	switch upstreamType {
	case "sub2api":
		return f.fetchSub2APIInfo(ctx, accountID, rootURL, apiKey)
	case "nexapi":
		return f.fetchNexAPIInfo(ctx, accountID, rootURL, apiKey)
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

// Sub2APIResponse represents the response from Sub2API balance query
type Sub2APIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		Balance     float64 `json:"balance"`     // CNY
		Concurrency int     `json:"concurrency"` // 并发数
		Status      string  `json:"status"`      // active/inactive
		Name        string  `json:"name"`        // 账号名称
	} `json:"data"`
}

// fetchSub2APIInfo fetches information from Sub2API.
func (f *UpstreamInfoFetcher) fetchSub2APIInfo(ctx context.Context, accountID int64, rootURL, apiKey string) (*UpstreamInfo, error) {
	f.logger.Debug("Fetching Sub2API info", "account_id", accountID)

	// Sub2API API endpoint
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

	var apiResp Sub2APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if !apiResp.Success && apiResp.Message != "" {
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
	}

	info := &UpstreamInfo{
		AccountID:   accountID,
		Name:        apiResp.Data.Name,
		Type:        "sub2api",
		Balance:     apiResp.Data.Balance, // Already in CNY
		Concurrency: apiResp.Data.Concurrency,
		Status:      apiResp.Data.Status,
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
		Quota          int64  `json:"quota"`           // 总配额
		UsedQuota      int64  `json:"used_quota"`      // 已用配额
		RemainingQuota int64  `json:"remaining_quota"` // 剩余配额
		Name           string `json:"name"`            // 账号名称
		Status         int    `json:"status"`          // 1=active, 0=inactive
	} `json:"data"`
}

// fetchNexAPIInfo fetches information from NexAPI.
func (f *UpstreamInfoFetcher) fetchNexAPIInfo(ctx context.Context, accountID int64, rootURL, apiKey string) (*UpstreamInfo, error) {
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

	// NexAPI conversion: remaining_quota / 431778 = balance_cny
	balanceCNY := float64(apiResp.Data.RemainingQuota) / 431778.0

	status := "inactive"
	if apiResp.Data.Status == 1 {
		status = "active"
	}

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
		"quota", apiResp.Data.RemainingQuota,
		"status", info.Status,
	)

	return info, nil
}
