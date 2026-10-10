package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/stretchr/testify/require"
)

func TestRemediationParseDecoratorPreservesParserResults(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, input := range []string{`{"model":"claude-test","messages":[{"role":"user","content":"private prompt"}]}`, `{"model":`} {
			s := requestobs.New(time.Now())
			ctx := context.WithValue(requestobs.WithContext(context.Background(), s), ctxkey.UpstreamRetryGuardrailsEnabled, enabled)
			expected, expectedErr := ParseGatewayRequest(NewRequestBodyRef([]byte(input)), "anthropic")
			actual, err := ParseGatewayRequestWithContext(ctx, NewRequestBodyRef([]byte(input)), "anthropic")
			require.Equal(t, expected, actual)
			if expectedErr != nil {
				require.EqualError(t, err, expectedErr.Error())
			} else {
				require.NoError(t, err)
			}
			samples := s.StageSnapshot()
			if enabled {
				require.Len(t, samples, 1)
				require.Equal(t, "json_parse", samples[0].Stage)
				require.Equal(t, 1, samples[0].Calls)
			} else {
				require.Empty(t, samples)
			}
			require.True(t, s.FirstUpstreamAt().IsZero())
		}
	}
}

func TestGatewayStagesTextWebsocketSelectionWithoutCandidate(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	cfg := newSchedulerTestOpenAIWSV2Config()
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{},
		cache:              &schedulerTestGatewayCache{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	s := requestobs.New(time.Now())
	ctx := context.WithValue(requestobs.WithContext(context.Background(), s), ctxkey.UpstreamRetryGuardrailsEnabled, true)
	groupID := int64(10109)
	selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportResponsesWebsocketV2, false)
	require.ErrorContains(t, err, "no available OpenAI accounts")
	require.Nil(t, selection)
	samples := s.StageSnapshot()
	var routingCalls int
	for _, sample := range samples {
		if sample.Stage == "routing" {
			routingCalls = sample.Calls
		}
	}
	require.Equal(t, 1, routingCalls, "selection must be observed even without a legacy HTTP selector")
}
