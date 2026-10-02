package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetUpstreamRetryGuardrailsSettings(c *gin.Context) {
	settings, err := h.settingService.GetUpstreamRetryGuardrailsSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *SettingHandler) UpdateUpstreamRetryGuardrailsSettings(c *gin.Context) {
	var request struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "enabled must be a boolean")
		return
	}
	settings := service.UpstreamRetryGuardrailsSettings{Enabled: *request.Enabled}
	if err := h.settingService.SetUpstreamRetryGuardrailsSettings(c.Request.Context(), settings); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}
