//go:build unit

package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/requestobs"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestGatewayStagesAuthenticationStopsBeforeHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{RunMode: config.RunModeSimple}
	key := &service.APIKey{ID: 1, Status: service.StatusActive, User: &service.User{ID: 1, Status: service.StatusActive}}
	repo := &stubApiKeyRepo{getByKey: func(context.Context, string) (*service.APIKey, error) { return key, nil }}
	svc := service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg)
	for _, enabled := range []bool{false, true} {
		r := gin.New()
		r.Use(GatewayRemediation(func() bool { return enabled }), gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
		r.GET("/t", func(c *gin.Context) {
			s := requestobs.FromContext(c.Request.Context())
			if enabled {
				samples := s.StageSnapshot()
				require.Len(t, samples, 1, "auth must end before downstream execution")
				require.Equal(t, "authentication", samples[0].Stage)
			} else {
				require.Nil(t, s)
			}
			c.Status(http.StatusOK)
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/t", nil)
		req.Header.Set("Authorization", "Bearer test-key")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
	}
}

func TestGatewayStageAccessFieldsOmitUnobservedBoundaries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(GatewayRemediation(func() bool { return true }))
	r.GET("/t", func(c *gin.Context) {
		s := requestobs.FromContext(c.Request.Context())
		requestobs.StartStage(c.Request.Context(), requestobs.StageRouting)()
		s.MarkUpstreamStarted(s.Snapshot().StartedAt.Add(40 * time.Millisecond))
		fields := appendRequestObservabilityFields(nil, c, time.Now())
		enc := zapcore.NewMapObjectEncoder()
		for _, f := range fields {
			f.AddTo(enc)
		}
		require.Equal(t, int64(40), enc.Fields["pre_upstream_ms"])
		require.Contains(t, enc.Fields, "gateway_stages")
		require.NotContains(t, enc.Fields, "user_slot_wait_ms")
		// No synthetic pre-upstream latency when no HTTP attempt was observed.
		empty := requestobs.New(time.Now())
		c.Request = c.Request.WithContext(requestobs.WithContext(c.Request.Context(), empty))
		fields = appendRequestObservabilityFields([]zap.Field{}, c, time.Now())
		enc = zapcore.NewMapObjectEncoder()
		for _, f := range fields {
			f.AddTo(enc)
		}
		require.NotContains(t, enc.Fields, "pre_upstream_ms")
		require.NotContains(t, enc.Fields, "gateway_stages")
		c.Status(http.StatusOK)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/t", nil))
}

func TestGatewayStagesRealAccessLogUsesBoundedMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, enabled := range []bool{false, true} {
		sink := initMiddlewareTestLogger(t)
		cfg := &config.Config{RunMode: config.RunModeSimple}
		svc := service.NewAPIKeyService(&stubApiKeyRepo{}, nil, nil, nil, nil, nil, cfg)
		r := gin.New()
		r.Use(RequestLogger(), GatewayRemediation(func() bool { return enabled }), Logger(), gin.HandlerFunc(NewAPIKeyAuthMiddleware(svc, nil, cfg)))
		called := false
		r.GET("/t", func(c *gin.Context) { called = true; c.Status(http.StatusOK) })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/t", nil))
		require.Equal(t, http.StatusUnauthorized, w.Code)
		require.False(t, called)
		found := false
		for _, event := range sink.list() {
			if event.Message != "http request completed" {
				continue
			}
			found = true
			if enabled {
				require.Contains(t, event.Fields, "gateway_stages")
				encoded, err := json.Marshal(event.Fields["gateway_stages"])
				require.NoError(t, err)
				var stages []requestobs.StageSample
				require.NoError(t, json.Unmarshal(encoded, &stages))
				require.Len(t, stages, 1)
				require.Equal(t, "authentication", stages[0].Stage)
				require.Equal(t, 1, stages[0].Calls)
				require.Equal(t, int64(0), event.Fields["upstream_attempts"])
			} else {
				require.NotContains(t, event.Fields, "gateway_stages")
			}
			require.NotContains(t, event.Fields, "pre_upstream_ms")
			require.NotContains(t, event.Fields, "Authorization")
		}
		require.True(t, found)
	}
}
