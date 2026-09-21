package handler

import (
	"context"
	"net/url"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	monitorservice "github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/service"
)

// AccountProvider is the small part of the host account repository needed by
// the monitor. Account credentials remain owned by the host database.
type AccountProvider interface {
	ListActive(ctx context.Context) ([]service.Account, error)
	GetByID(ctx context.Context, id int64) (*service.Account, error)
}

type monitorAccount struct {
	account *service.Account
	info    monitorservice.AccountInfo
}

func monitorAccounts(accounts []service.Account) []monitorAccount {
	result := make([]monitorAccount, 0, len(accounts))
	for i := range accounts {
		account := &accounts[i]
		info, ok := monitorAccountInfo(account)
		if ok {
			result = append(result, monitorAccount{account: account, info: info})
		}
	}
	return result
}

func monitorAccountInfo(account *service.Account) (monitorservice.AccountInfo, bool) {
	if account == nil || account.Type != service.AccountTypeUpstream || !account.IsActive() {
		return monitorservice.AccountInfo{}, false
	}
	typeName := upstreamMonitorType(account)
	apiKey := account.GetCredential("api_key")
	if typeName == "" || apiKey == "" {
		return monitorservice.AccountInfo{}, false
	}
	return monitorservice.AccountInfo{ID: account.ID, UpstreamType: typeName, ApiKey: apiKey}, true
}

func upstreamMonitorType(account *service.Account) string {
	for _, key := range []string{"upstream_monitor_type", "upstream_type"} {
		if value, ok := account.Extra[key].(string); ok {
			if normalized := normalizeUpstreamType(value); normalized != "" {
				return normalized
			}
		}
	}
	baseURL := account.GetCredential("base_url")
	host := strings.ToLower(baseURL)
	if parsed, err := url.Parse(baseURL); err == nil {
		host = strings.ToLower(parsed.Hostname())
	}
	switch {
	case strings.Contains(host, "sub2api"):
		return "sub2api"
	case strings.Contains(host, "nexapi"):
		return "nexapi"
	default:
		return ""
	}
}

func normalizeUpstreamType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sub2api":
		return "sub2api"
	case "nexapi":
		return "nexapi"
	default:
		return ""
	}
}
