package service

import (
	"io"
	"log/slog"
	"strconv"

	"github.com/tidwall/gjson"
)

// Gemini streams report cumulative snapshots, not token deltas. Preserve field
// presence until final normalization so omitted cache counts cannot become zero.
type geminiMessagesUsageAccumulator struct {
	prompt, candidates, cached, thoughts, image int
	hasPrompt, warned                           bool
}

func newGeminiMessagesUsageAccumulator() *geminiMessagesUsageAccumulator {
	return &geminiMessagesUsageAccumulator{}
}

func (a *geminiMessagesUsageAccumulator) observe(raw []byte) {
	u := gjson.GetBytes(raw, "usageMetadata")
	if !u.IsObject() {
		return
	}
	next := *a
	for _, field := range []struct {
		key string
		dst *int
	}{
		{"promptTokenCount", &next.prompt},
		{"candidatesTokenCount", &next.candidates},
		{"cachedContentTokenCount", &next.cached},
		{"thoughtsTokenCount", &next.thoughts},
	} {
		v := u.Get(field.key)
		if !v.Exists() {
			continue
		}
		n, err := strconv.ParseInt(v.Raw, 10, strconv.IntSize)
		if v.Type != gjson.Number || err != nil || n < 0 {
			a.invalid()
			return
		}
		*field.dst = int(n)
		if field.key == "promptTokenCount" {
			next.hasPrompt = true
		}
	}
	if next.hasPrompt && next.cached > next.prompt {
		a.invalid()
		return
	}
	if next.candidates > int(^uint(0)>>1)-next.thoughts {
		a.invalid()
		return
	}
	if u.Get("candidatesTokensDetails").IsArray() {
		next.image = 0
		for _, detail := range u.Get("candidatesTokensDetails").Array() {
			if detail.Get("modality").String() != "IMAGE" {
				continue
			}
			v := detail.Get("tokenCount")
			n, err := strconv.ParseInt(v.Raw, 10, strconv.IntSize)
			if v.Type != gjson.Number || err != nil || n < 0 {
				a.invalid()
				return
			}
			next.image = int(n)
			break
		}
	}
	*a = next
}

func (a *geminiMessagesUsageAccumulator) invalid() {
	if !a.warned {
		slog.Warn("gemini messages: ignored invalid usage snapshot")
		a.warned = true
	}
}

func (a *geminiMessagesUsageAccumulator) usage() (ClaudeUsage, bool) {
	if !a.hasPrompt && a.cached > 0 {
		// Cache may arrive before its prompt total. Keep it pending, never emit
		// a negative ordinary-input bucket or infer the missing total.
		return ClaudeUsage{OutputTokens: a.candidates + a.thoughts, ImageOutputTokens: a.image}, false
	}
	return ClaudeUsage{
		InputTokens:          a.prompt - a.cached,
		OutputTokens:         a.candidates + a.thoughts,
		CacheReadInputTokens: a.cached,
		ImageOutputTokens:    a.image,
	}, true
}

func (s *GeminiMessagesCompatService) messagesUsageReconciliationEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.Gateway.GeminiMessagesUsageReconciliationEnabled
}

func geminiMessagesUsageMap(u ClaudeUsage) map[string]any {
	return map[string]any{
		"input_tokens":            u.InputTokens,
		"output_tokens":           u.OutputTokens,
		"cache_read_input_tokens": u.CacheReadInputTokens,
	}
}

// Keep the shared native/Chat collector unchanged; only Messages opts in.
func (s *GeminiMessagesCompatService) collectMessagesSSE(body io.Reader) (map[string]any, *ClaudeUsage, error) {
	if !s.messagesUsageReconciliationEnabled() {
		return collectGeminiSSE(body, true)
	}
	a := newGeminiMessagesUsageAccumulator()
	collected, _, _, err := collectGeminiSSEObserved(body, true, a.observe)
	u, _ := a.usage()
	return collected, &u, err
}
