package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiMessagesUsageAccumulatorPreservesOmittedCache(t *testing.T) {
	a := newGeminiMessagesUsageAccumulator()
	a.observe([]byte(`{"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":10,"cachedContentTokenCount":30}}`))
	a.observe([]byte(`{"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20}}`))
	got, ok := a.usage()
	if !ok {
		t.Fatal("expected usage")
	}
	if got.InputTokens != 70 || got.OutputTokens != 20 || got.CacheReadInputTokens != 30 {
		t.Fatalf("got %+v", got)
	}
}

func TestGeminiMessagesUsageAccumulatorIgnoresNullAndAcceptsExplicitZero(t *testing.T) {
	a := newGeminiMessagesUsageAccumulator()
	a.observe([]byte(`{"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":10,"cachedContentTokenCount":30}}`))
	a.observe([]byte(`{"usageMetadata":null}`))
	got, _ := a.usage()
	if got.CacheReadInputTokens != 30 {
		t.Fatalf("null cleared usage: %+v", got)
	}
	a.observe([]byte(`{"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":0}}`))
	got, _ = a.usage()
	if got.CacheReadInputTokens != 0 || got.InputTokens != 100 {
		t.Fatalf("explicit zero not authoritative: %+v", got)
	}
}

func TestGeminiMessagesUsageAccumulatorRejectsInvalidSnapshot(t *testing.T) {
	a := newGeminiMessagesUsageAccumulator()
	a.observe([]byte(`{"usageMetadata":{"promptTokenCount":10,"cachedContentTokenCount":4}}`))
	a.observe([]byte(`{"usageMetadata":{"promptTokenCount":2,"cachedContentTokenCount":3}}`))
	got, _ := a.usage()
	if got.InputTokens != 6 || got.CacheReadInputTokens != 4 {
		t.Fatalf("invalid snapshot replaced valid usage: %+v", got)
	}
}

func TestGeminiMessagesUsageReconciliationEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []bool{false, true} {
		for _, streaming := range []bool{false, true} {
			t.Run(geminiMessagesUsageTestCase(enabled, streaming), func(t *testing.T) {
				svc := &GeminiMessagesCompatService{cfg: &config.Config{Gateway: config.GatewayConfig{GeminiMessagesUsageReconciliationEnabled: enabled}}}
				raw := `{"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":20,"cachedContentTokenCount":30}}`
				body := raw
				if streaming {
					body = "data: " + raw + "\n\n" + `data: {"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":25}}` + "\n\n" + "data: [DONE]\n\n"
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
				var usage *ClaudeUsage
				if streaming {
					result, err := svc.handleStreamingResponse(c, resp, time.Now(), "gemini-4-argon")
					require.NoError(t, err)
					usage = result.usage
				} else {
					var err error
					usage, err = svc.handleNonStreamingResponse(c, resp, "gemini-4-argon")
					require.NoError(t, err)
				}
				if enabled {
					require.Equal(t, 70, usage.InputTokens)
					require.Equal(t, 30, usage.CacheReadInputTokens)
					require.Contains(t, rec.Body.String(), `"cache_read_input_tokens":30`)
				} else {
					require.NotContains(t, rec.Body.String(), "cache_read_input_tokens")
					if streaming {
						require.Zero(t, usage.CacheReadInputTokens)
					} else {
						require.Equal(t, 30, usage.CacheReadInputTokens)
					}
				}
				if !streaming {
					var msg map[string]any
					require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &msg))
					require.Equal(t, float64(usage.InputTokens), msg["usage"].(map[string]any)["input_tokens"])
				}
			})
		}
	}
}
func geminiMessagesUsageTestCase(enabled, streaming bool) string {
	if enabled {
		if streaming {
			return "enabled_stream"
		}
		return "enabled_nonstream"
	}
	if streaming {
		return "disabled_stream"
	}
	return "disabled_nonstream"
}

func TestGeminiMessagesUsageReconciliationCollectedSSE(t *testing.T) {
	svc := &GeminiMessagesCompatService{cfg: &config.Config{Gateway: config.GatewayConfig{GeminiMessagesUsageReconciliationEnabled: true}}}
	body := `data: {"response":{"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":100,"candidatesTokenCount":4,"thoughtsTokenCount":5}}}` + "\n\n" + `data: {"response":{"usageMetadata":null}}` + "\n\n"
	_, usage, err := svc.collectMessagesSSE(strings.NewReader(body))
	require.NoError(t, err)
	require.Zero(t, usage.InputTokens)
	require.Equal(t, 100, usage.CacheReadInputTokens)
	require.Equal(t, 9, usage.OutputTokens)
}

func TestGeminiMessagesUsageAccumulatorSnapshotCases(t *testing.T) {
	for _, tc := range []struct {
		name, frame          string
		input, cache, output int
	}{
		{"late_cache", `{"promptTokenCount":100,"cachedContentTokenCount":80,"candidatesTokenCount":20}`, 20, 80, 20},
		{"full_cache", `{"promptTokenCount":100,"cachedContentTokenCount":100,"thoughtsTokenCount":5}`, 0, 100, 15},
		{"negative", `{"cachedContentTokenCount":-1}`, 70, 30, 10},
		{"fractional", `{"cachedContentTokenCount":1.5}`, 70, 30, 10},
		{"string", `{"cachedContentTokenCount":"30"}`, 70, 30, 10},
		{"field_null", `{"cachedContentTokenCount":null}`, 70, 30, 10},
		{"empty", `{}`, 70, 30, 10},
		{"repeated", `{"promptTokenCount":100,"cachedContentTokenCount":30,"candidatesTokenCount":10}`, 70, 30, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newGeminiMessagesUsageAccumulator()
			a.observe([]byte(`{"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":30,"candidatesTokenCount":10}}`))
			a.observe([]byte(`{"usageMetadata":` + tc.frame + `}`))
			got, _ := a.usage()
			require.Equal(t, tc.input, got.InputTokens)
			require.Equal(t, tc.cache, got.CacheReadInputTokens)
			require.Equal(t, tc.output, got.OutputTokens)
		})
	}
}

func TestGeminiMessagesUsageReconciliationForwardBuffered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []bool{false, true} {
		t.Run(geminiMessagesUsageTestCase(enabled, false), func(t *testing.T) {
			body := `data: {"response":{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":100}}}` + "\n\n" + `data: {"response":{"usageMetadata":null}}` + "\n\n"
			stub := &geminiCompatHTTPUpstreamStub{response: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}}
			svc := &GeminiMessagesCompatService{tokenProvider: &GeminiTokenProvider{}, httpUpstream: stub, cfg: &config.Config{Gateway: config.GatewayConfig{GeminiMessagesUsageReconciliationEnabled: enabled}}}
			account := &Account{ID: 101, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "test-token", "project_id": "test-project"}, Concurrency: 1}
			request := []byte(`{"model":"gemini-4-argon","stream":false,"messages":[{"role":"user","content":"hi"}]}`)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(request)))
			result, err := svc.Forward(context.Background(), c, account, request)
			require.NoError(t, err)
			require.NotNil(t, result)
			if enabled {
				require.Equal(t, 100, result.Usage.CacheReadInputTokens)
				require.Zero(t, result.Usage.InputTokens)
				require.Contains(t, rec.Body.String(), `"cache_read_input_tokens":100`)
			} else {
				require.NotContains(t, rec.Body.String(), "cache_read_input_tokens")
			}
		})
	}
}

func TestGeminiMessagesUsageAccumulatorImageAndPendingCache(t *testing.T) {
	a := newGeminiMessagesUsageAccumulator()
	a.observe([]byte(`{"usageMetadata":{"cachedContentTokenCount":30,"candidatesTokenCount":2}}`))
	pending, ok := a.usage()
	require.False(t, ok)
	require.Zero(t, pending.InputTokens)
	require.Zero(t, pending.CacheReadInputTokens)
	a.observe([]byte(`{"usageMetadata":{"promptTokenCount":100,"candidatesTokensDetails":[{"modality":"IMAGE","tokenCount":2}]}}`))
	got, ok := a.usage()
	require.True(t, ok)
	require.Equal(t, 70, got.InputTokens)
	require.Equal(t, 30, got.CacheReadInputTokens)
	require.Equal(t, 2, got.ImageOutputTokens)
	a.observe([]byte(`{"usageMetadata":{"candidatesTokensDetails":[{"modality":"IMAGE","tokenCount":-2}]}}`))
	got, _ = a.usage()
	require.Equal(t, 2, got.ImageOutputTokens)
	a.observe([]byte(`{"usageMetadata":{"candidatesTokenCount":4}}`))
	got, _ = a.usage()
	require.Equal(t, 2, got.ImageOutputTokens)
	require.Equal(t, 4, got.OutputTokens)
}
