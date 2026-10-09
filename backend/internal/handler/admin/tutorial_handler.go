package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type TutorialHandler struct {
	service  *service.TutorialService
	assetDir string
}

func NewTutorialHandler(s *service.TutorialService, cfg *config.Config) *TutorialHandler {
	assetDir := ""
	if cfg != nil {
		assetDir = filepath.Join(cfg.Pricing.DataDir, "tutorial-assets")
	}
	return &TutorialHandler{service: s, assetDir: assetDir}
}

type tutorialRequest struct {
	Slug            string `json:"slug"`
	Title           string `json:"title"`
	Summary         string `json:"summary"`
	Category        string `json:"category"`
	ContentHTML     string `json:"content_html"`
	ContentMarkdown string `json:"content_markdown"`
	IsPublic        bool   `json:"is_public"`
	SortOrder       int    `json:"sort_order"`
}

func (h *TutorialHandler) List(c *gin.Context) {
	page, size := response.ParsePagination(c)
	items, total, err := h.service.List(c.Request.Context(), service.TutorialListParams{Page: page, PageSize: size, Query: c.Query("query"), Category: c.Query("category"), Admin: true})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, size)
}

func (h *TutorialHandler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	t, err := h.service.GetByID(c.Request.Context(), id, true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	assets, _ := h.service.ListAssets(c.Request.Context(), id)
	response.Success(c, gin.H{"tutorial": t, "assets": assets})
}

func (h *TutorialHandler) Create(c *gin.Context) {
	var req tutorialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	sub, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	t := &service.Tutorial{Slug: req.Slug, Title: req.Title, Summary: req.Summary, Category: req.Category, ContentHTML: req.ContentHTML, ContentMarkdown: req.ContentMarkdown, IsPublic: req.IsPublic, SortOrder: req.SortOrder, Status: service.TutorialStatusDraft, CreatedBy: sub.UserID, UpdatedBy: sub.UserID}
	if err := h.service.Create(c.Request.Context(), t); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, t)
}

func (h *TutorialHandler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req tutorialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	t, err := h.service.GetByID(c.Request.Context(), id, true)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	sub, _ := middleware.GetAuthSubjectFromContext(c)
	if strings.TrimSpace(req.Slug) != "" {
		t.Slug = req.Slug
	}
	if req.Title != "" {
		t.Title = req.Title
	}
	t.Summary = req.Summary
	t.Category = req.Category
	t.ContentHTML = req.ContentHTML
	t.ContentMarkdown = req.ContentMarkdown
	t.IsPublic = req.IsPublic
	t.SortOrder = req.SortOrder
	t.UpdatedBy = sub.UserID
	if err := h.service.Update(c.Request.Context(), t); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	assets, _ := h.service.ListAssets(c.Request.Context(), id)
	response.Success(c, gin.H{"tutorial": t, "assets": assets})
}

func (h *TutorialHandler) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Tutorial deleted"})
}

func (h *TutorialHandler) SetStatus(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request")
		return
	}
	sub, _ := middleware.GetAuthSubjectFromContext(c)
	if err := h.service.SetStatus(c.Request.Context(), id, req.Status, sub.UserID); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"status": req.Status})
}

func (h *TutorialHandler) ListAssets(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	assets, err := h.service.ListAssets(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, assets)
}

func (h *TutorialHandler) UploadAsset(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	sub, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not found in context")
		return
	}
	if h.assetDir == "" {
		response.BadRequest(c, "Tutorial asset storage is not configured")
		return
	}
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "file is required")
		return
	}
	defer file.Close()
	if header.Size <= 0 || header.Size > 100*1024*1024 {
		response.BadRequest(c, "file size must be between 1 byte and 100 MiB")
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, 100*1024*1024+1))
	if err != nil || len(data) > 100*1024*1024 {
		response.BadRequest(c, "file could not be read")
		return
	}
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(header.Filename))
	}
	kind := "file"
	if strings.HasPrefix(mimeType, "image/") {
		kind = "image"
	} else if strings.HasPrefix(mimeType, "video/") {
		kind = "video"
	}
	if kind == "file" && mimeType != "application/pdf" && mimeType != "text/plain" && mimeType != "application/zip" {
		response.BadRequest(c, "file type is not allowed")
		return
	}
	sha := sha256.Sum256(data)
	token := hex.EncodeToString(sha[:]) + strings.ToLower(filepath.Ext(header.Filename))
	if err := os.MkdirAll(h.assetDir, 0o750); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	path := filepath.Join(h.assetDir, token)
	if err := os.WriteFile(path, data, 0o640); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	label := strings.TrimSpace(c.PostForm("label"))
	if label == "" {
		label = header.Filename
	}
	asset := &service.TutorialAsset{TutorialID: &id, Kind: kind, StorageKey: token, PublicURL: "/api/v1/tutorial-assets/" + token, OriginalName: filepath.Base(header.Filename), Label: label, MIMEType: mimeType, SizeBytes: int64(len(data)), SHA256: hex.EncodeToString(sha[:]), CreatedBy: sub.UserID}
	if err := h.service.CreateAsset(c.Request.Context(), asset); err != nil {
		_ = os.Remove(path)
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, asset)
}

func (h *TutorialHandler) DeleteAsset(c *gin.Context) {
	if _, ok := parseID(c); !ok {
		return
	}
	assetID, err := strconv.ParseInt(c.Param("asset_id"), 10, 64)
	if err != nil || assetID <= 0 {
		response.BadRequest(c, "Invalid asset ID")
		return
	}
	if err := h.service.DeleteAsset(c.Request.Context(), assetID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Asset deleted"})
}

func parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid tutorial ID")
		return 0, false
	}
	return id, true
}
