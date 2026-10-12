package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/model"
	"github.com/Wei-Shaw/sub2api/plugins/upstream-monitor/repository"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type alertUpstreamsFake struct{ fakeUpstreamProvider }

func (*alertUpstreamsFake) List(context.Context) ([]*ent.UpstreamMonitorUpstream, error) {
	return []*ent.UpstreamMonitorUpstream{{ID: 1, BaseURL: "https://example.com"}}, nil
}

type alertConfigStoreFake struct {
	calls  int
	config model.BalanceAlertConfig
	err    error
}

func (s *alertConfigStoreFake) Get(_ context.Context, id int64) (*model.BalanceAlertRule, error) {
	return &model.BalanceAlertRule{UpstreamID: id, BalanceAlertConfig: s.config}, nil
}
func (s *alertConfigStoreFake) Save(_ context.Context, _ int64, config model.BalanceAlertConfig) error {
	s.calls++
	s.config = config
	return s.err
}

func TestBalanceAlertRoutesValidateUpstreamAndPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, path, body string
		status           int
		busy             bool
	}{
		{"unknown upstream", "/upstreams/2/balance-alert", `{}`, 404, false},
		{"unsaved upstream", "/upstreams/0/balance-alert", `{}`, 400, false},
		{"invalid email", "/upstreams/1/balance-alert", `{"enabled":true,"threshold":"0","recipients":["bad"],"locale":"zh"}`, 400, false},
		{"valid zero threshold", "/upstreams/1/balance-alert", `{"enabled":true,"threshold":"0","recipients":["ops@example.com"],"locale":"zh"}`, 200, false},
		{"active check", "/upstreams/1/balance-alert", `{"enabled":true,"threshold":"0","recipients":["ops@example.com"],"locale":"zh"}`, 409, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &alertConfigStoreFake{}
			if test.busy {
				store.err = repository.ErrAlertBusy
			}
			h := NewBalanceAlertHandler(store, &alertUpstreamsFake{})
			router := gin.New()
			h.RegisterRoutes(router.Group(""))
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPut, test.path, strings.NewReader(test.body))
			r.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, r)
			require.Equal(t, test.status, w.Code, w.Body.String())
			if test.status == 400 || test.status == 404 {
				require.Zero(t, store.calls)
			}
		})
	}
}
