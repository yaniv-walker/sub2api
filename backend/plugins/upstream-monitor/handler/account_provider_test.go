package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestMonitorAccountInfoUsesHostAccountData(t *testing.T) {
	tests := []struct {
		name     string
		account  service.Account
		wantType string
		wantOK   bool
	}{
		{
			name:     "sub2api from base url",
			account:  service.Account{ID: 1, Type: service.AccountTypeUpstream, Status: service.StatusActive, Credentials: map[string]any{"api_key": "key", "base_url": "https://api.sub2api.com"}},
			wantType: "sub2api", wantOK: true,
		},
		{
			name:     "nexapi from base url",
			account:  service.Account{ID: 2, Type: service.AccountTypeUpstream, Status: service.StatusActive, Credentials: map[string]any{"api_key": "key", "base_url": "https://api.nexapi.cc"}},
			wantType: "nexapi", wantOK: true,
		},
		{
			name:     "explicit extra wins",
			account:  service.Account{ID: 3, Type: service.AccountTypeUpstream, Status: service.StatusActive, Credentials: map[string]any{"api_key": "key", "base_url": "https://api.nexapi.cc"}, Extra: map[string]any{"upstream_monitor_type": "sub2api"}},
			wantType: "sub2api", wantOK: true,
		},
		{
			name:    "non upstream filtered",
			account: service.Account{ID: 4, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Credentials: map[string]any{"api_key": "key", "base_url": "https://api.sub2api.com"}},
			wantOK:  false,
		},
		{
			name:    "inactive filtered",
			account: service.Account{ID: 5, Type: service.AccountTypeUpstream, Status: service.StatusDisabled, Credentials: map[string]any{"api_key": "key", "base_url": "https://api.sub2api.com"}},
			wantOK:  false,
		},
		{
			name:    "missing api key filtered",
			account: service.Account{ID: 6, Type: service.AccountTypeUpstream, Status: service.StatusActive, Credentials: map[string]any{"base_url": "https://api.sub2api.com"}},
			wantOK:  false,
		},
		{
			name:    "unknown upstream filtered",
			account: service.Account{ID: 7, Type: service.AccountTypeUpstream, Status: service.StatusActive, Credentials: map[string]any{"api_key": "key", "base_url": "https://example.com"}},
			wantType: "sub2api", wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := monitorAccountInfo(&tt.account)
			if ok != tt.wantOK {
				t.Fatalf("monitorAccountInfo ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && info.UpstreamType != tt.wantType {
				t.Fatalf("upstream type = %q, want %q", info.UpstreamType, tt.wantType)
			}
		})
	}
}

func TestMonitorAccountsFiltersHostAccounts(t *testing.T) {
	accounts := []service.Account{
		{ID: 1, Type: service.AccountTypeUpstream, Status: service.StatusActive, Credentials: map[string]any{"api_key": "key", "base_url": "https://api.sub2api.com"}},
		{ID: 2, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Credentials: map[string]any{"api_key": "key", "base_url": "https://api.sub2api.com"}},
	}
	got := monitorAccounts(accounts)
	if len(got) != 1 || got[0].account.ID != 1 {
		t.Fatalf("monitorAccounts returned %+v, want only active upstream account 1", got)
	}
}
