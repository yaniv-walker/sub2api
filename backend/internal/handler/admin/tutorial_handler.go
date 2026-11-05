package admin

import (
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
)

type TutorialHandler struct {
	service  *service.TutorialService
	assetDir string
	signer   *service.TutorialMediaSigner
}

func NewTutorialHandler(s *service.TutorialService, cfg *config.Config) *TutorialHandler {
	dir, secret := "", ""
	if cfg != nil {
		dir = filepath.Join(cfg.Pricing.DataDir, "tutorial-assets")
		secret = cfg.JWT.Secret
	}
	return &TutorialHandler{service: s, assetDir: dir, signer: service.NewTutorialMediaSigner(secret)}
}

type tutorialRequest struct {
	Slug             *string                     `json:"slug"`
	Title            *string                     `json:"title"`
	Summary          *string                     `json:"summary"`
	Category         *string                     `json:"category"`
	ContentHTML      *string                     `json:"content_html"`
	ContentMarkdown  *string                     `json:"content_markdown"`
	IsPublic         *bool                       `json:"is_public"`
	SortOrder        *int                        `json:"sort_order"`
	Version          int64                       `json:"version"`
	Resources        *[]service.TutorialResource `json:"resources"`
	SourceOrigin     *string                     `json:"source_origin"`
	SourceDocumentID *string                     `json:"source_document_id"`
	SourceUpdatedAt  *string                     `json:"source_updated_at"`
	SourceRelation   *string                     `json:"source_relation"`
}

func (req tutorialRequest) apply(t *service.Tutorial) {
	if req.Slug != nil {
		t.Slug = *req.Slug
	}
	if req.Title != nil {
		t.Title = *req.Title
	}
	if req.Summary != nil {
		t.Summary = *req.Summary
	}
	if req.Category != nil {
		t.Category = *req.Category
	}
	if req.ContentMarkdown != nil {
		t.ContentMarkdown = *req.ContentMarkdown
	}
	if req.ContentHTML != nil {
		t.ContentHTML = *req.ContentHTML
	}
	if req.IsPublic != nil {
		t.IsPublic = *req.IsPublic
	}
	if req.SortOrder != nil {
		t.SortOrder = *req.SortOrder
	}
	if req.Resources != nil {
		t.Resources = *req.Resources
	}
	if req.SourceOrigin != nil {
		t.SourceOrigin = *req.SourceOrigin
	}
	if req.SourceDocumentID != nil {
		t.SourceDocumentID = *req.SourceDocumentID
	}
	if req.SourceUpdatedAt != nil {
		t.SourceUpdatedAt = *req.SourceUpdatedAt
	}
	if req.SourceRelation != nil {
		t.SourceRelation = *req.SourceRelation
	}
}
func tutorialError(c *gin.Context, err error) {
	var pe *pq.Error
	switch {
	case errors.Is(err, service.ErrTutorialConflict), errors.Is(err, service.ErrTutorialAssetInUse), errors.Is(err, service.ErrTutorialSlugReserved):
		response.Error(c, http.StatusConflict, err.Error())
	case errors.Is(err, service.ErrTutorialNotFound):
		response.NotFound(c, "Tutorial or asset not found")
	case errors.As(err, &pe) && pe.Code == "23505":
		response.Error(c, http.StatusConflict, "Tutorial slug or source already exists")
	case errors.Is(err, service.ErrTutorialInvalidInput), errors.Is(err, service.ErrTutorialInvalidSlug), errors.Is(err, service.ErrTutorialInvalidStatus):
		response.BadRequest(c, err.Error())
	default:
		response.ErrorFrom(c, err)
	}
}
func (h *TutorialHandler) List(c *gin.Context) {
	page, size := response.ParsePagination(c)
	if size > 100 {
		size = 100
	}
	items, total, err := h.service.List(c.Request.Context(), service.TutorialListParams{Page: page, PageSize: size, Query: c.Query("query"), Category: c.Query("category"), Admin: true})
	if err != nil {
		tutorialError(c, err)
		return
	}
	response.Paginated(c, items, total, page, size)
}
func (h *TutorialHandler) preview(t *service.Tutorial) { t.ContentHTML = h.signer.SignHTML(t, "admin") }
func (h *TutorialHandler) assets(c *gin.Context, id int64) ([]service.TutorialAsset, error) {
	assets, err := h.service.ListAssets(c.Request.Context(), id)
	if err != nil {
		return nil, err
	}
	for i := range assets {
		assets[i].PreviewURL = h.signer.URL(assets[i].StorageKey, id, 0, "admin", time.Now().Add(2*time.Hour))
	}
	return assets, nil
}
func (h *TutorialHandler) respond(c *gin.Context, t *service.Tutorial) {
	assets, err := h.assets(c, t.ID)
	if err != nil {
		tutorialError(c, err)
		return
	}
	h.preview(t)
	response.Success(c, gin.H{"tutorial": t, "assets": assets})
}
func (h *TutorialHandler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	t, err := h.service.GetByID(c.Request.Context(), id, true)
	if err != nil {
		tutorialError(c, err)
		return
	}
	h.respond(c, t)
}
func (h *TutorialHandler) Create(c *gin.Context) {
	var req tutorialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid tutorial request")
		return
	}
	sub, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "Authentication required")
		return
	}
	t := &service.Tutorial{Status: service.TutorialStatusDraft, SortOrder: 100, CreatedBy: sub.UserID, UpdatedBy: sub.UserID}
	req.apply(t)
	if err := h.service.Create(c.Request.Context(), t); err != nil {
		tutorialError(c, err)
		return
	}
	h.preview(t)
	response.Success(c, t)
}
func (h *TutorialHandler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req tutorialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid tutorial request")
		return
	}
	t, err := h.service.GetByID(c.Request.Context(), id, true)
	if err != nil {
		tutorialError(c, err)
		return
	}
	req.apply(t)
	t.Version = req.Version
	sub, _ := middleware.GetAuthSubjectFromContext(c)
	t.UpdatedBy = sub.UserID
	if err = h.service.Update(c.Request.Context(), t); err != nil {
		tutorialError(c, err)
		return
	}
	h.respond(c, t)
}
func (h *TutorialHandler) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	version, err := strconv.ParseInt(c.Query("version"), 10, 64)
	if err != nil || version < 1 {
		response.Error(c, http.StatusConflict, "Document version is required; reload before deleting")
		return
	}
	if err := h.service.Delete(c.Request.Context(), id, version); err != nil {
		tutorialError(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Tutorial deleted; shared media retained"})
}
func (h *TutorialHandler) SetStatus(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req struct {
		Status  string `json:"status"`
		Version int64  `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid status request")
		return
	}
	sub, _ := middleware.GetAuthSubjectFromContext(c)
	if err := h.service.SetStatus(c.Request.Context(), id, req.Status, sub.UserID, req.Version); err != nil {
		tutorialError(c, err)
		return
	}
	t, err := h.service.GetByID(c.Request.Context(), id, true)
	if err != nil {
		tutorialError(c, err)
		return
	}
	h.preview(t)
	response.Success(c, t)
}
func (h *TutorialHandler) Revisions(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	items, err := h.service.Revisions(c.Request.Context(), id)
	if err != nil {
		tutorialError(c, err)
		return
	}
	response.Success(c, items)
}
func (h *TutorialHandler) Restore(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req struct {
		RevisionID int64 `json:"revision_id"`
		Version    int64 `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid restore request")
		return
	}
	sub, _ := middleware.GetAuthSubjectFromContext(c)
	t, err := h.service.Restore(c.Request.Context(), id, req.RevisionID, req.Version, sub.UserID)
	if err != nil {
		tutorialError(c, err)
		return
	}
	h.respond(c, t)
}
func (h *TutorialHandler) ListAssets(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	items, err := h.assets(c, id)
	if err != nil {
		tutorialError(c, err)
		return
	}
	response.Success(c, items)
}
func (h *TutorialHandler) UploadAsset(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if _, err := h.service.GetByID(c.Request.Context(), id, true); err != nil {
		tutorialError(c, err)
		return
	}
	h.upload(c, id)
}
func (h *TutorialHandler) upload(c *gin.Context, id int64) {
	if h.assetDir == "" {
		response.BadRequest(c, "Asset storage unavailable")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 201<<20)
	defer func() {
		if c.Request.MultipartForm != nil {
			_ = c.Request.MultipartForm.RemoveAll()
		}
	}()
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		response.BadRequest(c, "A file up to 200 MiB is required")
		return
	}
	defer file.Close()
	asset, err := service.StoreTutorialAsset(h.assetDir, header.Filename, file)
	if err != nil {
		tutorialError(c, err)
		return
	}
	sub, _ := middleware.GetAuthSubjectFromContext(c)
	asset.TutorialID = &id
	asset.CreatedBy = sub.UserID
	asset.Label = strings.TrimSpace(c.PostForm("label"))
	if asset.Label == "" {
		asset.Label = header.Filename
	}
	if len([]rune(asset.Label)) > 160 {
		asset.Label = string([]rune(asset.Label)[:160])
	}
	if err = h.service.CreateAsset(c.Request.Context(), asset); err != nil {
		tutorialError(c, err)
		return
	}
	asset.PreviewURL = h.signer.URL(asset.StorageKey, id, 0, "admin", time.Now().Add(2*time.Hour))
	response.Success(c, asset)
}
func (h *TutorialHandler) assetForDocument(c *gin.Context) (int64, *service.TutorialAsset, bool) {
	id, ok := parseID(c)
	if !ok {
		return 0, nil, false
	}
	aid, err := strconv.ParseInt(c.Param("asset_id"), 10, 64)
	if err != nil || aid <= 0 {
		response.BadRequest(c, "Invalid asset ID")
		return 0, nil, false
	}
	items, err := h.service.ListAssets(c.Request.Context(), id)
	if err != nil {
		tutorialError(c, err)
		return 0, nil, false
	}
	for i := range items {
		if items[i].ID == aid {
			return id, &items[i], true
		}
	}
	response.NotFound(c, "Asset does not belong to this tutorial")
	return 0, nil, false
}
func (h *TutorialHandler) ReplaceAsset(c *gin.Context) {
	id, _, ok := h.assetForDocument(c)
	if !ok {
		return
	}
	h.upload(c, id)
}
func (h *TutorialHandler) RenameAsset(c *gin.Context) {
	id, a, ok := h.assetForDocument(c)
	if !ok {
		return
	}
	var req struct {
		Label string `json:"label"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid label")
		return
	}
	if err := h.service.UpdateAssetLabel(c.Request.Context(), id, a.ID, req.Label); err != nil {
		tutorialError(c, err)
		return
	}
	a.Label = req.Label
	a.PreviewURL = h.signer.URL(a.StorageKey, id, 0, "admin", time.Now().Add(2*time.Hour))
	response.Success(c, a)
}
func (h *TutorialHandler) AssetReferences(c *gin.Context) {
	_, a, ok := h.assetForDocument(c)
	if !ok {
		return
	}
	refs, err := h.service.AssetReferences(c.Request.Context(), a.ID)
	if err != nil {
		tutorialError(c, err)
		return
	}
	response.Success(c, refs)
}
func (h *TutorialHandler) DeleteAsset(c *gin.Context) {
	_, a, ok := h.assetForDocument(c)
	if !ok {
		return
	}
	if err := h.service.DeleteAsset(c.Request.Context(), a.ID); err != nil {
		tutorialError(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Asset removed; unreferenced bytes retained for recovery"})
}
func parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid tutorial ID")
		return 0, false
	}
	return id, true
}
