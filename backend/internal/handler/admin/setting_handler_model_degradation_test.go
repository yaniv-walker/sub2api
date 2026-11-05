package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestModelDegradationSwitchRoundTrip(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyTutorialEnabled: "true"})
	for _, enabled := range []bool{false, true} {
		rec := doUpdateSettings(t, h, map[string]any{"model_degradation_enabled": enabled}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Equal(t, strconv.FormatBool(enabled), repo.values[service.SettingKeyModelDegradationEnabled])
		var response struct {
			Data dto.SystemSettings `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.Equal(t, enabled, response.Data.ModelDegradationEnabled)
		rec = doUpdateSettings(t, h, map[string]any{"site_name": "Example"}, nil)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, strconv.FormatBool(enabled), repo.values[service.SettingKeyModelDegradationEnabled])
		public, err := h.settingService.GetPublicSettings(t.Context())
		require.NoError(t, err)
		require.Equal(t, enabled, public.ModelDegradationEnabled)
		injection, err := h.settingService.GetPublicSettingsForInjection(t.Context())
		require.NoError(t, err)
		payload, ok := injection.(*service.PublicSettingsInjectionPayload)
		require.True(t, ok)
		require.Equal(t, enabled, payload.ModelDegradationEnabled)
		require.Equal(t, "true", repo.values[service.SettingKeyTutorialEnabled])
	}
}

func TestDiffSettingsDetectsModelDegradationSwitch(t *testing.T) {
	changed := diffSettings(&service.SystemSettings{ModelDegradationEnabled: true}, &service.SystemSettings{ModelDegradationEnabled: false}, nil, nil, UpdateSettingsRequest{})
	require.Contains(t, changed, service.SettingKeyModelDegradationEnabled)
}
