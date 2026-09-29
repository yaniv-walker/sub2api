package handler

import (
	"context"
	"net/url"
	"strings"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/upstreammonitorupstream"
	"github.com/Wei-Shaw/sub2api/internal/service"
	monitorservice "github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/service"
)

// AccountProvider is the small part of the host account repository needed by
// the monitor. Account credentials remain owned by the host database.
type AccountProvider interface {
	ListActive(ctx context.Context) ([]service.Account, error)
	GetByID(ctx context.Context, id int64) (*service.Account, error)
}

// UpstreamProvider is the plugin-owned configuration boundary.
type UpstreamProvider interface {
	List(ctx context.Context) ([]*ent.UpstreamMonitorUpstream, error)
	Upsert(ctx context.Context, baseURL, name string, upstreamType upstreammonitorupstream.UpstreamType, enabled bool, accessToken, personalAccessToken, passkey *string, quotaDivider float64) (*ent.UpstreamMonitorUpstream, error)
}

type monitorAccount struct {
	account *service.Account
	info    monitorservice.AccountInfo
}

type monitorUpstream struct {
	ID                  int64
	BaseURL             string
	Name                string
	Type                string
	Configured          bool
	Enabled             bool
	AccessToken         *string
	PersonalAccessToken *string
	Passkey             *string
	QuotaDivider        float64
	CredentialError     string
	Accounts            []monitorAccount
}

func groupMonitorAccounts(accounts []monitorAccount, configs []*ent.UpstreamMonitorUpstream) []monitorUpstream {
	configByURL := make(map[string]*ent.UpstreamMonitorUpstream, len(configs))
	for _, config := range configs {
		configByURL[config.BaseURL] = config
	}

	byURL := make(map[string]*monitorUpstream)
	order := make([]string, 0)
	for _, account := range accounts {
		rootURL, err := monitorservice.NormalizeUpstreamBaseURL(account.info.BaseURL)
		if err != nil {
			continue
		}
		upstream := byURL[rootURL]
		if upstream == nil {
			upstream = &monitorUpstream{BaseURL: rootURL, Type: account.info.UpstreamType, Enabled: true, QuotaDivider: monitorservice.DefaultBalanceDivider(account.info.UpstreamType)}
			if config := configByURL[rootURL]; config != nil {
				upstream.ID = config.ID
				upstream.Name = config.Name
				upstream.Type = string(config.UpstreamType)
				upstream.Configured = true
				upstream.Enabled = config.Enabled
				upstream.AccessToken = config.AccessToken
				upstream.PersonalAccessToken = config.PersonalAccessToken
				upstream.Passkey = config.Passkey
				upstream.QuotaDivider = config.QuotaDivider
				if upstream.Type == "sub2api" {
					upstream.QuotaDivider = 1
				}
			}
			byURL[rootURL] = upstream
			order = append(order, rootURL)
		}
		account.info.BaseURL = rootURL
		account.info.UpstreamType = upstream.Type
		account.info.QuotaDivider = upstream.QuotaDivider
		upstream.Accounts = append(upstream.Accounts, account)
	}

	result := make([]monitorUpstream, 0, len(order))
	for _, rootURL := range order {
		result = append(result, *byURL[rootURL])
	}
	return result
}

func monitorUpstreamInfos(upstreams []monitorUpstream) []monitorservice.AccountInfo {
	result := make([]monitorservice.AccountInfo, 0, len(upstreams))
	for _, upstream := range upstreams {
		if !upstream.Enabled || len(upstream.Accounts) == 0 {
			continue
		}
		// Balance belongs to the upstream, so query it once with one of its
		// associated credentials instead of once per account/group.
		result = append(result, upstream.Accounts[0].info)
	}
	return result
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
	if account == nil || !account.IsActive() || !isMonitorAccount(account) {
		return monitorservice.AccountInfo{}, false
	}
	typeName := upstreamMonitorType(account)
	apiKey := account.GetCredential("api_key")
	baseURL := account.GetCredential("base_url")
	if typeName == "" || apiKey == "" || baseURL == "" {
		return monitorservice.AccountInfo{}, false
	}
	return monitorservice.AccountInfo{ID: account.ID, UpstreamType: typeName, BaseURL: baseURL, ApiKey: apiKey}, true
}

// isMonitorAccount accepts both the legacy upstream type and the type emitted
// by the admin UI for API-key based custom upstreams. Ordinary API-key accounts
// without a custom base URL remain outside the monitor.
func isMonitorAccount(account *service.Account) bool {
	if account.Type == service.AccountTypeUpstream {
		return true
	}
	return account.Type == service.AccountTypeAPIKey && strings.TrimSpace(account.GetCredential("base_url")) != ""
}

func upstreamMonitorType(account *service.Account) string {
	for _, key := range []string{"upstream_monitor_type", "upstream_type"} {
		if value, ok := account.Extra[key].(string); ok {
			if normalized := normalizeUpstreamType(value); normalized != "" {
				return normalized
			}
		}
	}
	// Some imported accounts store the monitor protocol in platform rather
	// than in extra. Treat only the plugin's explicit protocol names as such;
	// unrelated host platform names must not change the selection.
	if normalized := normalizeUpstreamType(account.Platform); normalized != "" {
		return normalized
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
		// The host account type already establishes that this is a managed
		// upstream account. Sub2API is the monitor's default protocol; an
		// account using NexAPI should set upstream_monitor_type/upstream_type
		// in its backend account extra data.
		return "sub2api"
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
