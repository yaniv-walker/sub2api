package service

import (
	"context"
	"errors"
	"fmt"
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
)

type Tutorial struct {
	ID              int64      `json:"id"`
	Slug            string     `json:"slug"`
	Title           string     `json:"title"`
	Summary         string     `json:"summary"`
	Category        string     `json:"category"`
	ContentHTML     string     `json:"content_html"`
	ContentMarkdown string     `json:"content_markdown"`
	Status          string     `json:"status"`
	IsPublic        bool       `json:"is_public"`
	SortOrder       int        `json:"sort_order"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	CreatedBy       int64      `json:"created_by"`
	UpdatedBy       int64      `json:"updated_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type TutorialAsset struct {
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
	Page     int
	PageSize int
	Query    string
	Category string
	Admin    bool
}

type TutorialRepository interface {
	List(context.Context, TutorialListParams) ([]Tutorial, int64, error)
	GetByID(context.Context, int64, bool) (*Tutorial, error)
	GetBySlug(context.Context, string, bool) (*Tutorial, error)
	Create(context.Context, *Tutorial) error
	Update(context.Context, *Tutorial) error
	Delete(context.Context, int64) error
	SetStatus(context.Context, int64, string, *time.Time, int64) error
	ListAssets(context.Context, int64) ([]TutorialAsset, error)
	GetAssetByKey(context.Context, string) (*TutorialAsset, error)
	CreateAsset(context.Context, *TutorialAsset) error
	DeleteAsset(context.Context, int64) error
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
	if err := validateTutorial(t); err != nil {
		return err
	}
	if t.Status == "" {
		t.Status = TutorialStatusDraft
	}
	return s.repo.Create(ctx, t)
}

func (s *TutorialService) Update(ctx context.Context, t *Tutorial) error {
	if err := validateTutorial(t); err != nil {
		return err
	}
	return s.repo.Update(ctx, t)
}

func (s *TutorialService) Delete(ctx context.Context, id int64) error { return s.repo.Delete(ctx, id) }

func (s *TutorialService) SetStatus(ctx context.Context, id int64, status string, actorID int64) error {
	if status != TutorialStatusDraft && status != TutorialStatusPublished && status != TutorialStatusOffline {
		return ErrTutorialInvalidStatus
	}
	var publishedAt *time.Time
	if status == TutorialStatusPublished {
		now := time.Now()
		publishedAt = &now
	}
	return s.repo.SetStatus(ctx, id, status, publishedAt, actorID)
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
