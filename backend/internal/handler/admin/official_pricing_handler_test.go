package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type officialHandlerRepo struct {
	service.SettingRepository
	values map[string]string
}

func (r *officialHandlerRepo) GetValue(_ context.Context, k string) (string, error) {
	v, ok := r.values[k]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return v, nil
}
func (r *officialHandlerRepo) Set(_ context.Context, k, v string) error { r.values[k] = v; return nil }
func (r *officialHandlerRepo) Delete(_ context.Context, k string) error {
	delete(r.values, k)
	return nil
}
func (r *officialHandlerRepo) GetAll(context.Context) (map[string]string, error) {
	return r.values, nil
}

func TestOfficialPricingHandlerSaveGetDeleteAndDefaultLookup(t *testing.T) {
	repo := &officialHandlerRepo{values: map[string]string{}}
	svc := service.NewOfficialPricingService(repo, nil, nil, nil)
	h := NewChannelHandler(nil, nil, nil, svc)
	r := gin.New()
	r.GET("/official", h.ListOfficialPricing)
	r.GET("/model", h.GetOfficialPricing)
	r.PUT("/model", h.SaveOfficialPricing)
	r.DELETE("/model", h.DeleteOfficialPricing)
	r.GET("/default", h.GetModelDefaultPricing)
	call := func(method, url, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, url, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	w := call(http.MethodPut, "/model?platform=openai&model=example", `{"input_price":0,"output_price":0.000006}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"source":"manual"`)
	w = call(http.MethodGet, "/default?platform=openai&model=example", "")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"found":true`)
	require.Contains(t, w.Body.String(), `"input_price":0`)
	w = call(http.MethodGet, "/official?platform=openai", "")
	require.Contains(t, w.Body.String(), `"model":"example"`)
	w = call(http.MethodPut, "/model?platform=openai&model=example", `{"input_price":-1}`)
	require.Equal(t, 400, w.Code)
	w = call(http.MethodDelete, "/model?platform=openai&model=example", "")
	require.Equal(t, 200, w.Code)
	w = call(http.MethodGet, "/model?platform=openai&model=example", "")
	require.Contains(t, w.Body.String(), `"source":"missing"`)
}
