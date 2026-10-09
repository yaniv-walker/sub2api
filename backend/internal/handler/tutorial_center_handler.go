package handler

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type TutorialCenterHandler struct {
	service  *service.TutorialService
	settings *service.SettingService
	assetDir string
}

func NewTutorialCenterHandler(s *service.TutorialService, settings *service.SettingService, cfg *config.Config) *TutorialCenterHandler {
	assetDir := ""
	if cfg != nil {
		assetDir = filepath.Join(cfg.Pricing.DataDir, "tutorial-assets")
	}
	return &TutorialCenterHandler{service: s, settings: settings, assetDir: assetDir}
}

func (h *TutorialCenterHandler) List(c *gin.Context) {
	if h.settings != nil && !h.settings.TutorialEnabled(c.Request.Context()) {
		response.NotFound(c, "Tutorial is disabled")
		return
	}
	page, size := response.ParsePagination(c)
	items, total, err := h.service.List(c.Request.Context(), service.TutorialListParams{Page: page, PageSize: size, Query: c.Query("query"), Category: c.Query("category")})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, size)
}

func (h *TutorialCenterHandler) Get(c *gin.Context) {
	if h.settings != nil && !h.settings.TutorialEnabled(c.Request.Context()) {
		response.NotFound(c, "Tutorial is disabled")
		return
	}
	t, err := h.service.GetBySlug(c.Request.Context(), c.Param("slug"), false)
	if err != nil {
		if err == service.ErrTutorialNotFound {
			response.NotFound(c, "Tutorial not found")
			return
		}
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, t)
}

func (h *TutorialCenterHandler) Asset(c *gin.Context) {
	key := filepath.Base(c.Param("token"))
	asset, err := h.service.GetAssetByKey(c.Request.Context(), key)
	if err != nil {
		response.NotFound(c, "Asset not found")
		return
	}
	path := filepath.Join(h.assetDir, key)
	if h.assetDir == "" {
		response.NotFound(c, "Asset storage is not configured")
		return
	}
	if _, err := os.Stat(path); err != nil {
		response.NotFound(c, "Asset not found")
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", "inline; filename=\""+filepath.Base(asset.OriginalName)+"\"")
	c.File(path)
}

func parseTutorialID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid tutorial ID")
		return 0, false
	}
	return id, true
}
