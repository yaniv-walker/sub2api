package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCheckinClaimRejectsUntrustedSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{`{"reward_amount":"100"}`, `{"user_id":2}`, `{"business_date":"2099-01-01"}`, `{} {}`, `[]`, `null`, strings.Repeat("a", 1025)} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/api/v1/checkin/claim", strings.NewReader(body))
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 11})
		h := &CheckinHandler{}
		h.Claim(c)
		require.Equal(t, 400, w.Code, body)
	}
}

func TestCheckinUserEndpointsRequireAuthentication(t *testing.T) {
	h := &CheckinHandler{}
	for _, endpoint := range []func(*gin.Context){h.Status, h.Claim, h.Cycle, h.History, h.AdminUpdateConfig} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/", nil)
		endpoint(c)
		require.Equal(t, 401, w.Code)
	}
}

func TestCheckinPaginationBounds(t *testing.T) {
	for _, query := range []string{"cursor=-1", "limit=101", "limit=0", "cursor=abc", "limit=x"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/?"+query, nil)
		_, _, ok := checkinPagination(c)
		require.False(t, ok)
		require.Equal(t, 400, w.Code)
	}
}
