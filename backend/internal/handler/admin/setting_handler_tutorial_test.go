package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettingsTutorialSwitchRoundTrip(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyTutorialEnabled:  "false",
		service.SettingKeyTutorialDocument: `{"title":"Existing tutorial","content":"Keep this content"}`,
		service.SettingKeySiteName:         "Example Gateway",
	})

	for _, enabled := range []bool{true, false} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			rec := doUpdateSettings(t, h, map[string]any{"tutorial_enabled": enabled}, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, strconv.FormatBool(enabled), repo.values[service.SettingKeyTutorialEnabled])
			var saved struct {
				Data dto.SystemSettings `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &saved))
			require.Equal(t, enabled, saved.Data.TutorialEnabled, "save response must match the stored switch")

			// Saving another setting must not reset the tutorial switch.
			rec = doUpdateSettings(t, h, map[string]any{"site_name": "Example Gateway"}, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, strconv.FormatBool(enabled), repo.values[service.SettingKeyTutorialEnabled])

			rec = httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
			h.GetSettings(c)
			require.Equal(t, http.StatusOK, rec.Code)
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &saved))
			require.Equal(t, enabled, saved.Data.TutorialEnabled, "reloading settings must keep the switch")

			public, err := h.settingService.GetPublicSettings(t.Context())
			require.NoError(t, err)
			require.Equal(t, enabled, public.TutorialEnabled)
			injected, err := h.settingService.GetPublicSettingsForInjection(t.Context())
			require.NoError(t, err)
			require.Equal(t, enabled, injected.(*service.PublicSettingsInjectionPayload).TutorialEnabled)
		})
	}
	// The switch must not rewrite tutorial content or unrelated site settings.
	require.Equal(t, `{"title":"Existing tutorial","content":"Keep this content"}`, repo.values[service.SettingKeyTutorialDocument])
	require.Equal(t, "Example Gateway", repo.values[service.SettingKeySiteName])
}

func TestDiffSettingsDetectsTutorialSwitchChange(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		changed := diffSettings(
			&service.SystemSettings{TutorialEnabled: !enabled},
			&service.SystemSettings{TutorialEnabled: enabled},
			nil, nil, UpdateSettingsRequest{},
		)
		require.Contains(t, changed, service.SettingKeyTutorialEnabled)
	}
}
