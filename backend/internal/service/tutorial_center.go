package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	TutorialStatusDraft     = "draft"
	TutorialStatusPublished = "published"
	TutorialStatusOffline   = "offline"
)

var tutorialSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

var (
	ErrTutorialNotFound      = errors.New("tutorial not found")
	ErrTutorialInvalidSlug   = errors.New("tutorial slug is invalid")
	ErrTutorialInvalidStatus = errors.New("tutorial status is invalid")
	ErrTutorialInvalidInput  = errors.New("tutorial input is invalid")
	ErrTutorialConflict      = errors.New("tutorial changed; reload before saving")
	ErrTutorialSlugReserved  = errors.New("tutorial slug is reserved by another document or published version")
	ErrTutorialAssetInUse    = errors.New("tutorial asset is referenced by a document, saved revision, or support settings")
)

type TutorialResource struct {
	Label    string `json:"label"`
	URL      string `json:"url"`
	Version  string `json:"version"`
	Platform string `json:"platform"`
	SHA256   string `json:"sha256"`
}

type TutorialRevision struct {
	ID         int64     `json:"id"`
	TutorialID int64     `json:"tutorial_id"`
	Version    int64     `json:"version"`
	Snapshot   Tutorial  `json:"snapshot"`
	CreatedBy  int64     `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
}

type Tutorial struct {
	Version          int64              `json:"version"`
	Resources        []TutorialResource `json:"resources"`
	SourceOrigin     string             `json:"source_origin,omitempty"`
	SourceDocumentID string             `json:"source_document_id,omitempty"`
	SourceUpdatedAt  string             `json:"source_updated_at,omitempty"`
	SourceRelation   string             `json:"source_relation,omitempty"`
	ID               int64              `json:"id"`
	Slug             string             `json:"slug"`
	Title            string             `json:"title"`
	Summary          string             `json:"summary"`
	Category         string             `json:"category"`
	ContentHTML      string             `json:"content_html"`
	ContentMarkdown  string             `json:"content_markdown"`
	Status           string             `json:"status"`
	IsPublic         bool               `json:"is_public"`
	SortOrder        int                `json:"sort_order"`
	PublishedAt      *time.Time         `json:"published_at,omitempty"`
	CreatedBy        int64              `json:"created_by"`
	UpdatedBy        int64              `json:"updated_by"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
}

type TutorialAsset struct {
	PreviewURL   string    `json:"preview_url,omitempty"`
	ID           int64     `json:"id"`
	TutorialID   *int64    `json:"tutorial_id,omitempty"`
	Kind         string    `json:"kind"`
	StorageKey   string    `json:"storage_key"`
	PublicURL    string    `json:"public_url"`
	OriginalName string    `json:"original_name"`
	Label        string    `json:"label"`
	MIMEType     string    `json:"mime_type"`
	SizeBytes    int64     `json:"size_bytes"`
	SHA256       string    `json:"sha256"`
	CreatedBy    int64     `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
}

type TutorialListParams struct {
	Page       int
	PageSize   int
	Query      string
	Category   string
	Admin      bool
	PublicOnly bool
}

type TutorialRepository interface {
	List(context.Context, TutorialListParams) ([]Tutorial, int64, error)
	GetByID(context.Context, int64, bool) (*Tutorial, error)
	GetBySlug(context.Context, string, bool) (*Tutorial, error)
	Create(context.Context, *Tutorial) error
	Update(context.Context, *Tutorial) error
	Delete(context.Context, int64, int64) error
	SetStatus(context.Context, int64, string, *time.Time, int64, int64) error
	ListAssets(context.Context, int64) ([]TutorialAsset, error)
	GetAssetByKey(context.Context, string) (*TutorialAsset, error)
	CreateAsset(context.Context, *TutorialAsset) error
	DeleteAsset(context.Context, int64) error
	LinkAsset(context.Context, int64, int64) error
	UpdateAssetLabel(context.Context, int64, int64, string) error
	AssetReferences(context.Context, int64) ([]Tutorial, error)
	Revisions(context.Context, int64) ([]TutorialRevision, error)
	GetRevision(context.Context, int64, int64) (*TutorialRevision, error)
	CanReadAsset(context.Context, string, int64, int64, string) (bool, error)
}

type TutorialService struct{ repo TutorialRepository }

func NewTutorialService(repo TutorialRepository) *TutorialService {
	return &TutorialService{repo: repo}
}

func (s *TutorialService) List(ctx context.Context, params TutorialListParams) ([]Tutorial, int64, error) {
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 || params.PageSize > 100 {
		params.PageSize = 20
	}
	return s.repo.List(ctx, params)
}

func (s *TutorialService) GetBySlug(ctx context.Context, slug string, admin bool) (*Tutorial, error) {
	return s.repo.GetBySlug(ctx, strings.TrimSpace(slug), admin)
}

func (s *TutorialService) GetByID(ctx context.Context, id int64, admin bool) (*Tutorial, error) {
	return s.repo.GetByID(ctx, id, admin)
}

func (s *TutorialService) Create(ctx context.Context, t *Tutorial) error {
	if err := prepareTutorial(t); err != nil {
		return err
	}
	if t.Status == "" {
		t.Status = TutorialStatusDraft
	}
	return s.repo.Create(ctx, t)
}

func (s *TutorialService) Update(ctx context.Context, t *Tutorial) error {
	if err := prepareTutorial(t); err != nil {
		return err
	}
	if t.Version < 1 {
		return ErrTutorialConflict
	}
	return s.repo.Update(ctx, t)
}

func (s *TutorialService) Delete(ctx context.Context, id, version int64) error {
	if version < 1 {
		return ErrTutorialConflict
	}
	return s.repo.Delete(ctx, id, version)
}

func (s *TutorialService) SetStatus(ctx context.Context, id int64, status string, actorID int64, version int64) error {
	if status != TutorialStatusDraft && status != TutorialStatusPublished && status != TutorialStatusOffline {
		return ErrTutorialInvalidStatus
	}
	var publishedAt *time.Time
	if status == TutorialStatusPublished {
		now := time.Now()
		publishedAt = &now
	}
	if version < 1 {
		return ErrTutorialConflict
	}
	return s.repo.SetStatus(ctx, id, status, publishedAt, actorID, version)
}

func (s *TutorialService) ListAssets(ctx context.Context, id int64) ([]TutorialAsset, error) {
	return s.repo.ListAssets(ctx, id)
}
func (s *TutorialService) GetAssetByKey(ctx context.Context, key string) (*TutorialAsset, error) {
	return s.repo.GetAssetByKey(ctx, key)
}

func (s *TutorialService) CreateAsset(ctx context.Context, asset *TutorialAsset) error {
	if asset == nil || asset.TutorialID == nil || asset.PublicURL == "" || asset.Kind == "" {
		return ErrTutorialInvalidInput
	}
	return s.repo.CreateAsset(ctx, asset)
}

func (s *TutorialService) DeleteAsset(ctx context.Context, id int64) error {
	return s.repo.DeleteAsset(ctx, id)
}

func prepareTutorial(t *Tutorial) error {
	if err := validateTutorial(t); err != nil {
		return err
	}
	if t.ContentMarkdown == "" {
		t.ContentMarkdown = t.ContentHTML
	}
	t.ContentMarkdown = CanonicalTutorialContent(t.ContentMarkdown)
	html, err := RenderTutorialMarkdown(t.ContentMarkdown)
	if err != nil {
		return err
	}
	t.ContentHTML = html
	if t.Resources == nil {
		t.Resources = []TutorialResource{}
	}
	if len(t.Resources) > 100 || len(t.SourceOrigin) > 2048 || len(t.SourceDocumentID) > 160 || len(t.SourceRelation) > 2048 || len(t.SourceUpdatedAt) > 100 {
		return ErrTutorialInvalidInput
	}
	for i := range t.Resources {
		t.Resources[i].URL = CanonicalTutorialContent(t.Resources[i].URL)
	}
	for _, r := range t.Resources {
		if strings.TrimSpace(r.Label) == "" || len([]rune(r.Label)) > 160 || len(r.URL) > 4096 || !validTutorialLink(r.URL) {
			return fmt.Errorf("%w: invalid download resource", ErrTutorialInvalidInput)
		}
	}
	return nil
}

func validateTutorial(t *Tutorial) error {
	if t == nil || strings.TrimSpace(t.Title) == "" || len([]rune(t.Title)) > 160 || len([]rune(t.Summary)) > 400 {
		return ErrTutorialInvalidInput
	}
	t.Slug = strings.TrimSpace(strings.ToLower(t.Slug))
	if !tutorialSlugPattern.MatchString(t.Slug) {
		return fmt.Errorf("%w: slug", ErrTutorialInvalidSlug)
	}
	if t.Category == "" {
		t.Category = "getting-started"
	}
	if t.ContentHTML == "" && t.ContentMarkdown == "" {
		return fmt.Errorf("%w: content", ErrTutorialInvalidInput)
	}
	if len([]byte(t.ContentHTML)) > 2*1024*1024 || len([]byte(t.ContentMarkdown)) > 2*1024*1024 {
		return fmt.Errorf("%w: content too large", ErrTutorialInvalidInput)
	}
	if strings.Contains(strings.ToLower(t.ContentHTML), "<script") || strings.Contains(strings.ToLower(t.ContentHTML), "javascript:") {
		return fmt.Errorf("%w: unsafe content", ErrTutorialInvalidInput)
	}
	if t.Status == "" {
		t.Status = TutorialStatusDraft
	}
	return nil
}

func validTutorialLink(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.User == nil && ((u.Scheme == "https" && u.Host != "") || (u.Scheme == "" && u.Host == "" && strings.HasPrefix(u.Path, "/api/v1/tutorial-assets/")))
}
func (s *TutorialService) LinkAsset(ctx context.Context, tid, aid int64) error {
	return s.repo.LinkAsset(ctx, tid, aid)
}
func (s *TutorialService) UpdateAssetLabel(ctx context.Context, tid, aid int64, label string) error {
	if len([]rune(label)) > 160 || strings.TrimSpace(label) == "" {
		return ErrTutorialInvalidInput
	}
	return s.repo.UpdateAssetLabel(ctx, tid, aid, label)
}
func (s *TutorialService) AssetReferences(ctx context.Context, id int64) ([]Tutorial, error) {
	return s.repo.AssetReferences(ctx, id)
}
func (s *TutorialService) Revisions(ctx context.Context, id int64) ([]TutorialRevision, error) {
	return s.repo.Revisions(ctx, id)
}
func (s *TutorialService) Restore(ctx context.Context, id, rid, version, actor int64) (*Tutorial, error) {
	r, err := s.repo.GetRevision(ctx, id, rid)
	if err != nil {
		return nil, err
	}
	t := r.Snapshot
	t.ID = id
	t.Version = version
	t.UpdatedBy = actor
	current, err := s.repo.GetByID(ctx, id, true)
	if err != nil {
		return nil, err
	}
	t.Status = current.Status
	t.PublishedAt = current.PublishedAt
	if err = s.Update(ctx, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
func (s *TutorialService) CanReadAsset(ctx context.Context, key string, id, version int64, scope string) (bool, error) {
	return s.repo.CanReadAsset(ctx, key, id, version, scope)
}
