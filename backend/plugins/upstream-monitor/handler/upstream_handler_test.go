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

type fakeSecretEncryptor struct{}

func (fakeSecretEncryptor) Encrypt(value string) (string, error) { return "encrypted:" + value, nil }
func (fakeSecretEncryptor) Decrypt(value string) (string, error) { return value, nil }

func (f *fakeUpstreamProvider) List(context.Context) ([]*ent.UpstreamMonitorUpstream, error) {
	return nil, nil
}

func (f *fakeUpstreamProvider) Upsert(_ context.Context, baseURL, name string, upstreamType upstreammonitorupstream.UpstreamType, enabled bool, accessToken, personalAccessToken, passkey *string, quotaDivider float64) (*ent.UpstreamMonitorUpstream, error) {
	f.saved = &ent.UpstreamMonitorUpstream{ID: 1, BaseURL: baseURL, Name: name, UpstreamType: upstreamType, Enabled: enabled, AccessToken: accessToken, PersonalAccessToken: personalAccessToken, Passkey: passkey, QuotaDivider: quotaDivider}
	return f.saved, nil
}

func TestConfigureUpstreamStoresOneTypeForNormalizedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &fakeUpstreamProvider{}
	handler := &MonitorHandler{upstreams: provider, encryptor: fakeSecretEncryptor{}}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/upstreams", bytes.NewBufferString(`{
		"base_url":"https://Gateway.Example.com/v1/chat/completions",
		"name":"Primary relay",
		"type":"nexapi",
		"quota_divider":100000,
		"access_token":"secret-token",
        "personal_access_token":"personal-token",
        "passkey":"passkey-token",
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
	if provider.saved.QuotaDivider != 100000 {
		t.Fatalf("quota divider = %v", provider.saved.QuotaDivider)
	}
	if provider.saved.AccessToken == nil || *provider.saved.AccessToken != "encrypted:secret-token" {
		t.Fatalf("encrypted access token = %v", provider.saved.AccessToken)
	}
	if provider.saved.PersonalAccessToken == nil || *provider.saved.PersonalAccessToken != "encrypted:personal-token" || provider.saved.Passkey == nil || *provider.saved.Passkey != "encrypted:passkey-token" {
		t.Fatalf("encrypted long-lived credentials = %+v", provider.saved)
	}
	if bytes.Contains(recorder.Body.Bytes(), []byte("secret-token")) || bytes.Contains(recorder.Body.Bytes(), []byte("personal-token")) || bytes.Contains(recorder.Body.Bytes(), []byte("passkey-token")) || bytes.Contains(recorder.Body.Bytes(), []byte("encrypted:")) {
		t.Fatalf("response exposed access token: %s", recorder.Body.String())
	}
}
