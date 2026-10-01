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

type observabilityHandlerRepo struct {
	service.SettingRepository
	value string
	fail  bool
}

func (r *observabilityHandlerRepo) GetValue(context.Context, string) (string, error) {
	if r.value == "" {
		return "", service.ErrSettingNotFound
	}
	return r.value, nil
}
func (r *observabilityHandlerRepo) Set(_ context.Context, _ string, value string) error {
	if r.fail {
		return errors.New("storage unavailable")
	}
	r.value = value
	return nil
}

func TestRequestObservabilitySettingsEndpointValidationAndFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &observabilityHandlerRepo{}
	svc := service.NewSettingService(repo, &config.Config{})
	h := &SettingHandler{settingService: svc}
	router := gin.New()
	router.GET("/settings/request-observability", h.GetRequestObservabilitySettings)
	router.PUT("/settings/request-observability", h.UpdateRequestObservabilitySettings)
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{}`, 400}, {`{"enabled":null}`, 400}, {`{"enabled":"true"}`, 400},
		{`{"enabled":true}`, 200}, {`{"enabled":false}`, 200},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/settings/request-observability", strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		require.Equal(t, tc.status, w.Code, tc.body)
	}
	require.NoError(t, svc.SetRequestObservabilitySettings(t.Context(), service.RequestObservabilitySettings{Enabled: true}))
	repo.fail = true
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/settings/request-observability", strings.NewReader(`{"enabled":false}`)))
	require.Equal(t, 500, w.Code)
	require.True(t, svc.RequestObservabilityEnabled())
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings/request-observability", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"enabled":true`)
}
