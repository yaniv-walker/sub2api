package routes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelDegradationFeatureGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		enabled bool
		err     error
		status  int
	}{
		{"enabled", true, nil, http.StatusOK},
		{"disabled", false, nil, http.StatusNotFound},
		{"settings failure", false, errors.New("database unavailable"), http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			gate := modelDegradationFeatureGate(func(context.Context) (bool, error) { return tc.enabled, tc.err })
			called := 0
			for _, path := range []string{"/model-degradation/tasks", "/admin/model-degradation/history-visibility"} {
				router.GET(path, gate, func(c *gin.Context) { called++; c.Status(http.StatusOK) })
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
				require.Equal(t, tc.status, rec.Code)
			}
			if tc.enabled {
				require.Equal(t, 2, called)
			} else {
				require.Zero(t, called)
			}
		})
	}
}
