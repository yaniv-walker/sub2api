package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/upstreammonitorupstream"
	"github.com/gin-gonic/gin"
)

type fakeUpstreamProvider struct {
	saved *ent.UpstreamMonitorUpstream
}

func (f *fakeUpstreamProvider) List(context.Context) ([]*ent.UpstreamMonitorUpstream, error) {
	return nil, nil
}

func (f *fakeUpstreamProvider) Upsert(_ context.Context, baseURL, name string, upstreamType upstreammonitorupstream.UpstreamType, enabled bool) (*ent.UpstreamMonitorUpstream, error) {
	f.saved = &ent.UpstreamMonitorUpstream{ID: 1, BaseURL: baseURL, Name: name, UpstreamType: upstreamType, Enabled: enabled}
	return f.saved, nil
}

func TestConfigureUpstreamStoresOneTypeForNormalizedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &fakeUpstreamProvider{}
	handler := &MonitorHandler{upstreams: provider}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/upstreams", bytes.NewBufferString(`{
		"base_url":"https://Gateway.Example.com/v1/chat/completions",
		"name":"Primary relay",
		"type":"nexapi",
		"enabled":true
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.ConfigureUpstream(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if provider.saved.BaseURL != "https://gateway.example.com" || provider.saved.UpstreamType != upstreammonitorupstream.UpstreamTypeNexapi {
		t.Fatalf("saved config = %+v", provider.saved)
	}
}
