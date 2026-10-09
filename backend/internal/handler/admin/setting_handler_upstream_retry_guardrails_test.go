package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type retryGuardrailsHandlerRepo struct {
	service.SettingRepository
	value string
	fail  bool
}

func (r *retryGuardrailsHandlerRepo) GetValue(context.Context, string) (string, error) {
	if r.value == "" {
		return "", service.ErrSettingNotFound
	}
	return r.value, nil
}

func (r *retryGuardrailsHandlerRepo) Set(_ context.Context, _ string, value string) error {
	if r.fail {
		return errors.New("storage unavailable")
	}
	r.value = value
	return nil
}

func TestUpstreamRetryGuardrailsSettingsEndpointValidationAndFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &retryGuardrailsHandlerRepo{}
	svc := service.NewSettingService(repo, &config.Config{})
	h := &SettingHandler{settingService: svc}
	router := gin.New()
	router.GET("/settings/upstream-retry-guardrails", h.GetUpstreamRetryGuardrailsSettings)
	router.PUT("/settings/upstream-retry-guardrails", h.UpdateUpstreamRetryGuardrailsSettings)

	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{}`, http.StatusBadRequest},
		{`{"enabled":null}`, http.StatusBadRequest},
		{`{"enabled":"true"}`, http.StatusBadRequest},
		{`{"enabled":true}`, http.StatusOK},
		{`{"enabled":false}`, http.StatusOK},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/settings/upstream-retry-guardrails", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		require.Equal(t, tc.status, w.Code, tc.body)
	}

	require.NoError(t, svc.SetUpstreamRetryGuardrailsSettings(t.Context(), service.UpstreamRetryGuardrailsSettings{Enabled: true}))
	repo.fail = true
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/settings/upstream-retry-guardrails", strings.NewReader(`{"enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.True(t, svc.UpstreamRetryGuardrailsEnabled())

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings/upstream-retry-guardrails", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"enabled":true`)
}
