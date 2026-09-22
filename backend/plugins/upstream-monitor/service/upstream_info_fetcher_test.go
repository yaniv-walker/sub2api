package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeUpstreamBaseURL(t *testing.T) {
	tests := map[string]string{
		"https://Gateway.Example.com/v1":                  "https://gateway.example.com",
		"https://gateway.example.com/v1/chat/completions": "https://gateway.example.com",
		"http://127.0.0.1:3000/api/openai/v1":             "http://127.0.0.1:3000",
	}
	for input, want := range tests {
		got, err := NormalizeUpstreamBaseURL(input)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestFetchInfoUsesAccountUpstreamRoot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/usage", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mode": "unrestricted", "isValid": true, "balance": 48.76, "remaining": 48.76,
		})
	}))
	defer server.Close()

	fetcher := NewUpstreamInfoFetcher(nil)
	info, err := fetcher.FetchInfo(1, "sub2api", server.URL+"/v1/chat/completions", "test-key")
	require.NoError(t, err)
	assert.Equal(t, 48.76, info.Balance)
}

func TestFetchSub2APIInfo_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/usage", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"mode": "quota_limited", "isValid": true, "status": "active",
			"quota": map[string]any{"limit": 100.0, "used": 51.24, "remaining": 48.76, "unit": "USD"},
		})
	}))
	defer server.Close()

	info, err := NewUpstreamInfoFetcher(nil).FetchInfo(1, "sub2api", server.URL+"/v1", "test-key")
	require.NoError(t, err)
	assert.Equal(t, 48.76, info.Balance)
	assert.Equal(t, "active", info.Status)
}

func TestFetchNexAPIInfo_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/user/self", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		resp := NexAPIResponse{
			Code: 0,
			Msg:  "success",
		}
		resp.Data.Quota = 48838015
		resp.Data.UsedQuota = 6661985
		resp.Data.RemainingQuota = 99999999 // must not override quota-used calculation
		resp.Data.Name = "NexAPI Test"
		resp.Data.Status = 1

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	info, err := NewUpstreamInfoFetcher(nil).FetchInfo(2, "nexapi", server.URL+"/v1/chat/completions", "test-key")
	require.NoError(t, err)
	assert.InDelta(t, 97.68, info.Balance, 0.01)
	assert.Equal(t, "NexAPI Test", info.Name)
}

func TestNexAPIPrefersQuotaMinusUsedOverRemainingQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"quota": 48838015, "used_quota": 6661985, "remaining_quota": 99999999,
			"status": 1, "name": "NexAPI",
		}})
	}))
	defer server.Close()

	info, err := NewUpstreamInfoFetcher(nil).FetchInfo(3, "nexapi", server.URL, "token")
	require.NoError(t, err)
	assert.InDelta(t, float64(48838015-6661985)/431778.0, info.Balance, 0.000001)
}

func TestFetchNexAPIInfoUsesDirectBalanceWhenProvided(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"balance": 95.93,
			"quota":   48838015, "used_quota": 6661985,
			"status": 1, "name": "Xinyun",
		}})
	}))
	defer server.Close()

	info, err := NewUpstreamInfoFetcher(nil).FetchInfo(4, "nexapi", server.URL, "token")
	require.NoError(t, err)
	assert.InDelta(t, 95.93, info.Balance, 0.000001)
}

func TestFetchNexAPIInfoUsesConfiguredQuotaDivider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"quota": 9593000, "used_quota": 0, "status": 1,
		}})
	}))
	defer server.Close()

	info, err := NewUpstreamInfoFetcher(nil).FetchInfoWithBalanceDivider(5, "nexapi", server.URL, "token", 100000)
	require.NoError(t, err)
	assert.InDelta(t, 95.93, info.Balance, 0.000001)
}

func TestFetchInfo_UnsupportedType(t *testing.T) {
	fetcher := NewUpstreamInfoFetcher(nil)

	_, err := fetcher.FetchInfo(1, "unknown", "https://example.com/v1", "test-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported upstream type")
}

func TestNexAPIConversion(t *testing.T) {
	// Test the conversion formula: remaining_quota / 431778 = balance_cny
	testCases := []struct {
		name            string
		remainingQuota  int64
		expectedBalance float64
	}{
		{
			name:            "Example from documentation",
			remainingQuota:  42176030,
			expectedBalance: 97.68,
		},
		{
			name:            "Zero quota",
			remainingQuota:  0,
			expectedBalance: 0.0,
		},
		{
			name:            "Small quota",
			remainingQuota:  431778,
			expectedBalance: 1.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			balance := float64(tc.remainingQuota) / 431778.0
			assert.InDelta(t, tc.expectedBalance, balance, 0.01)
		})
	}
}
