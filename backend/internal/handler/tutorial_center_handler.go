package handler

import (
	"errors"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type TutorialCenterHandler struct {
	service  *service.TutorialService
	settings *service.SettingService
	assetDir string
	signer   *service.TutorialMediaSigner
}

func NewTutorialCenterHandler(s *service.TutorialService, settings *service.SettingService, cfg *config.Config) *TutorialCenterHandler {
	dir, secret := "", ""
	if cfg != nil {
		dir = filepath.Join(cfg.Pricing.DataDir, "tutorial-assets")
		secret = cfg.JWT.Secret
	}
	return &TutorialCenterHandler{service: s, settings: settings, assetDir: dir, signer: service.NewTutorialMediaSigner(secret)}
}
func (h *TutorialCenterHandler) enabled(c *gin.Context) bool {
	if h.settings != nil && !h.settings.TutorialEnabled(c.Request.Context()) {
		response.NotFound(c, "Tutorial is disabled")
		return false
	}
	return true
}
func (h *TutorialCenterHandler) publicAllowed(c *gin.Context) bool {
	if !h.enabled(c) {
		return false
	}
	if h.settings == nil {
		response.NotFound(c, "Public sharing disabled")
		return false
	}
	support, err := h.settings.GetTutorialSupport(c.Request.Context())
	if err != nil || !support.PublicEnabled {
		response.NotFound(c, "Public sharing disabled")
		return false
	}
	return true
}
func (h *TutorialCenterHandler) List(c *gin.Context)       { h.list(c, false) }
func (h *TutorialCenterHandler) PublicList(c *gin.Context) { h.list(c, true) }
func (h *TutorialCenterHandler) list(c *gin.Context, public bool) {
	if public {
		if !h.publicAllowed(c) {
			return
		}
	} else if !h.enabled(c) {
		return
	}
	page, size := response.ParsePagination(c)
	if size > 100 {
		size = 100
	}
	items, total, err := h.service.List(c.Request.Context(), service.TutorialListParams{Page: page, PageSize: size, Query: c.Query("query"), Category: c.Query("category"), PublicOnly: public})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, size)
}
func (h *TutorialCenterHandler) Get(c *gin.Context)       { h.get(c, false) }
func (h *TutorialCenterHandler) PublicGet(c *gin.Context) { h.get(c, true) }
func (h *TutorialCenterHandler) get(c *gin.Context, public bool) {
	if public {
		if !h.publicAllowed(c) {
			return
		}
	} else if !h.enabled(c) {
		return
	}
	t, err := h.service.GetBySlug(c.Request.Context(), c.Param("slug"), false)
	if err != nil {
		if errors.Is(err, service.ErrTutorialNotFound) {
			response.NotFound(c, "Tutorial not found")
		} else {
			response.ErrorFrom(c, err)
		}
		return
	}
	if public && !t.IsPublic {
		response.NotFound(c, "Tutorial not found")
		return
	}
	scope := "published"
	if public {
		scope = "public"
	}
	t.ContentHTML = h.signer.SignHTML(t, scope)
	for i := range t.Resources {
		key := strings.TrimPrefix(t.Resources[i].URL, "/api/v1/tutorial-assets/")
		if key != t.Resources[i].URL {
			t.Resources[i].URL = h.signer.URL(key, t.ID, t.Version, scope, time.Now().Add(2*time.Hour))
		}
	}
	response.Success(c, t)
}
func (h *TutorialCenterHandler) Asset(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	key := c.Param("token")
	if key == "" || key != filepath.Base(key) || strings.ContainsAny(key, "/\\") {
		response.NotFound(c, "Asset not found")
		return
	}
	if !h.signer.VerifyURL(c.Request.URL.String(), time.Now()) {
		response.Forbidden(c, "Media link expired or unauthorized")
		return
	}
	id, _ := strconv.ParseInt(c.Query("document"), 10, 64)
	version, _ := strconv.ParseInt(c.Query("version"), 10, 64)
	scope := c.Query("scope")
	if scope == "public" && !h.publicAllowed(c) {
		return
	}
	var allowed bool
	var err error
	if scope == "support" {
		if h.settings != nil {
			support, e := h.settings.GetTutorialSupport(c.Request.Context())
			if e == nil {
				allowed = support.WechatQRURL == "/api/v1/tutorial-assets/"+key || support.QQQRURL == "/api/v1/tutorial-assets/"+key
			}
		}
	} else {
		allowed, err = h.service.CanReadAsset(c.Request.Context(), key, id, version, scope)
	}
	if err != nil || !allowed {
		response.NotFound(c, "Asset unavailable")
		return
	}
	a, err := h.service.GetAssetByKey(c.Request.Context(), key)
	if err != nil {
		response.NotFound(c, "Asset not found")
		return
	}
	path := filepath.Join(h.assetDir, key)
	if h.assetDir == "" {
		response.NotFound(c, "Asset storage unavailable")
		return
	}
	if _, err = os.Stat(path); err != nil {
		response.NotFound(c, "Asset not found")
		return
	}
	c.Header("Content-Type", a.MIMEType)
	c.Header("X-Content-Type-Options", "nosniff")
	disposition := "inline"
	if a.Kind == "file" {
		disposition = "attachment"
	}
	c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": a.OriginalName}))
	c.Header("Cache-Control", "private, no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.File(path)
}
func (h *TutorialCenterHandler) Support(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	h.readSupport(c, false)
}
func (h *TutorialCenterHandler) PublicSupport(c *gin.Context) {
	if !h.publicAllowed(c) {
		return
	}
	h.readSupport(c, false)
}
func (h *TutorialCenterHandler) AdminSupport(c *gin.Context) { h.readSupport(c, true) }
func (h *TutorialCenterHandler) readSupport(c *gin.Context, admin bool) {
	if h.settings == nil {
		response.Success(c, service.TutorialSupport{Title: "联系售后"})
		return
	}
	support, err := h.settings.GetTutorialSupport(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !admin {
		for _, target := range []*string{&support.WechatQRURL, &support.QQQRURL} {
			key := strings.TrimPrefix(*target, "/api/v1/tutorial-assets/")
			if key != *target {
				a, e := h.service.GetAssetByKey(c.Request.Context(), key)
				if e == nil && a.Kind == "image" {
					id := int64(1)
					if a.TutorialID != nil {
						id = *a.TutorialID
					}
					*target = h.signer.URL(key, id, 0, "support", time.Now().Add(2*time.Hour))
				} else {
					*target = ""
				}
			}
		}
	}
	response.Success(c, support)
}
func (h *TutorialCenterHandler) UpdateSupport(c *gin.Context) {
	var input service.TutorialSupport
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid support settings")
		return
	}
	if h.settings == nil {
		response.InternalError(c, "Settings unavailable")
		return
	}
	if err := h.settings.UpdateTutorialSupport(c.Request.Context(), input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, input)
}
