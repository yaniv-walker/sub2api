package handler

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type TutorialHandler struct {
	settingService *service.SettingService
}

func NewTutorialHandler(settingService *service.SettingService) *TutorialHandler {
	return &TutorialHandler{settingService: settingService}
}

// Get serves the user-facing tutorial document.
func (h *TutorialHandler) Get(c *gin.Context) {
	if !h.settingService.TutorialEnabled(c.Request.Context()) {
		response.Error(c, http.StatusNotFound, "Tutorial is disabled")
		return
	}
	doc, err := h.settingService.GetTutorialDocument(c.Request.Context())
	if err != nil {
		response.InternalError(c, "Failed to load tutorial")
		return
	}
	response.Success(c, doc)
}

// GetAdmin allows an authenticated administrator to load the document even
// while the public entry is disabled, so it can be prepared before publishing.
func (h *TutorialHandler) GetAdmin(c *gin.Context) {
	doc, err := h.settingService.GetTutorialDocument(c.Request.Context())
	if err != nil {
		response.InternalError(c, "Failed to load tutorial")
		return
	}
	response.Success(c, gin.H{"enabled": h.settingService.TutorialEnabled(c.Request.Context()), "title": doc.Title, "content": doc.Content})
}

func (h *TutorialHandler) Update(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024*1024+16*1024)
	var req struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid tutorial payload")
		return
	}
	if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.Content) == "" {
		response.BadRequest(c, "Tutorial title and content are required")
		return
	}
	if err := h.settingService.UpdateTutorialDocument(c.Request.Context(), service.TutorialDocument{Title: req.Title, Content: req.Content}); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	h.GetAdmin(c)
}
