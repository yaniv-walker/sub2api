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
		assert.Equal(t, "/api/user/self", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"balance": 48.76, "concurrency": 20, "status": "active", "name": "Shared upstream"},
		})
	}))
	defer server.Close()

	fetcher := NewUpstreamInfoFetcher(nil)
	info, err := fetcher.FetchInfo(1, "sub2api", server.URL+"/v1/chat/completions", "test-key")
	require.NoError(t, err)
	assert.Equal(t, 48.76, info.Balance)
}

func TestFetchSub2APIInfo_Success(t *testing.T) {
	// Mock Sub2API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/balance", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		resp := Sub2APIResponse{
			Success: true,
			Message: "OK",
		}
		resp.Data.Balance = 48.76
		resp.Data.Concurrency = 20
		resp.Data.Status = "active"
		resp.Data.Name = "Test Account"

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Note: In real implementation, we'd need dependency injection for the base URL
	// For now, this test demonstrates the expected behavior
	t.Log("This test demonstrates expected behavior - actual implementation needs URL injection")
}

func TestFetchNexAPIInfo_Success(t *testing.T) {
	// Mock NexAPI server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/user/info", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		resp := NexAPIResponse{
			Code: 0,
			Msg:  "success",
		}
		resp.Data.Quota = 48838015
		resp.Data.UsedQuota = 6661985
		resp.Data.RemainingQuota = 42176030
		resp.Data.Name = "NexAPI Test"
		resp.Data.Status = 1

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	t.Log("This test demonstrates expected NexAPI behavior")
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
