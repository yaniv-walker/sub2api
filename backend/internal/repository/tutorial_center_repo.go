package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type tutorialRepository struct{ db *sql.DB }

func NewTutorialRepository(db *sql.DB) service.TutorialRepository { return &tutorialRepository{db: db} }

func (r *tutorialRepository) List(ctx context.Context, p service.TutorialListParams) ([]service.Tutorial, int64, error) {
	args := []any{}
	where := []string{"1=1"}
	if !p.Admin {
		where = append(where, "status = 'published' AND (is_public = TRUE OR is_public = FALSE)")
	}
	if q := strings.TrimSpace(p.Query); q != "" {
		args = append(args, "%"+q+"%")
		where = append(where, fmt.Sprintf("(title ILIKE $%d OR summary ILIKE $%d OR content_markdown ILIKE $%d)", len(args), len(args), len(args)))
	}
	if c := strings.TrimSpace(p.Category); c != "" {
		args = append(args, c)
		where = append(where, fmt.Sprintf("category = $%d", len(args)))
	}
	base := " FROM tutorial_documents WHERE " + strings.Join(where, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*)"+base, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (p.Page - 1) * p.PageSize
	args = append(args, p.PageSize, offset)
	rows, err := r.db.QueryContext(ctx, "SELECT id, slug, title, summary, category, content_html, content_markdown, status, is_public, sort_order, published_at, created_by, updated_by, created_at, updated_at"+base+" ORDER BY sort_order ASC, id DESC LIMIT $"+fmt.Sprint(len(args)-1)+" OFFSET $"+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []service.Tutorial{}
	for rows.Next() {
		var t service.Tutorial
		if err := rows.Scan(&t.ID, &t.Slug, &t.Title, &t.Summary, &t.Category, &t.ContentHTML, &t.ContentMarkdown, &t.Status, &t.IsPublic, &t.SortOrder, &t.PublishedAt, &t.CreatedBy, &t.UpdatedBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, t)
	}
	return items, total, rows.Err()
}

func (r *tutorialRepository) GetByID(ctx context.Context, id int64, admin bool) (*service.Tutorial, error) {
	return r.get(ctx, "id", id, admin)
}
func (r *tutorialRepository) GetBySlug(ctx context.Context, slug string, admin bool) (*service.Tutorial, error) {
	return r.get(ctx, "slug", slug, admin)
}

func (r *tutorialRepository) get(ctx context.Context, field string, value any, admin bool) (*service.Tutorial, error) {
	where := field + " = $1"
	if !admin {
		where += " AND status = 'published'"
	}
	var t service.Tutorial
	err := r.db.QueryRowContext(ctx, "SELECT id, slug, title, summary, category, content_html, content_markdown, status, is_public, sort_order, published_at, created_by, updated_by, created_at, updated_at FROM tutorial_documents WHERE "+where, value).Scan(&t.ID, &t.Slug, &t.Title, &t.Summary, &t.Category, &t.ContentHTML, &t.ContentMarkdown, &t.Status, &t.IsPublic, &t.SortOrder, &t.PublishedAt, &t.CreatedBy, &t.UpdatedBy, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrTutorialNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *tutorialRepository) Create(ctx context.Context, t *service.Tutorial) error {
	err := r.db.QueryRowContext(ctx, `INSERT INTO tutorial_documents (slug,title,summary,category,content_html,content_markdown,status,is_public,sort_order,created_by,updated_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10) RETURNING id,created_at,updated_at`, t.Slug, t.Title, t.Summary, t.Category, t.ContentHTML, t.ContentMarkdown, t.Status, t.IsPublic, t.SortOrder, t.CreatedBy).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
	return err
}

func (r *tutorialRepository) Update(ctx context.Context, t *service.Tutorial) error {
	res, err := r.db.ExecContext(ctx, `UPDATE tutorial_documents SET slug=$1,title=$2,summary=$3,category=$4,content_html=$5,content_markdown=$6,is_public=$7,sort_order=$8,updated_by=$9,updated_at=NOW() WHERE id=$10`, t.Slug, t.Title, t.Summary, t.Category, t.ContentHTML, t.ContentMarkdown, t.IsPublic, t.SortOrder, t.UpdatedBy, t.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return service.ErrTutorialNotFound
	}
	return nil
}

func (r *tutorialRepository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM tutorial_documents WHERE id=$1", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return service.ErrTutorialNotFound
	}
	return nil
}

func (r *tutorialRepository) SetStatus(ctx context.Context, id int64, status string, publishedAt *time.Time, actorID int64) error {
	res, err := r.db.ExecContext(ctx, "UPDATE tutorial_documents SET status=$1,published_at=$2,updated_by=$3,updated_at=NOW() WHERE id=$4", status, publishedAt, actorID, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return service.ErrTutorialNotFound
	}
	return nil
}

func (r *tutorialRepository) ListAssets(ctx context.Context, tutorialID int64) ([]service.TutorialAsset, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id,tutorial_id,kind,storage_key,public_url,original_name,label,mime_type,size_bytes,sha256,created_by,created_at FROM tutorial_assets WHERE tutorial_id=$1 ORDER BY id", tutorialID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []service.TutorialAsset{}
	for rows.Next() {
		var a service.TutorialAsset
		if err := rows.Scan(&a.ID, &a.TutorialID, &a.Kind, &a.StorageKey, &a.PublicURL, &a.OriginalName, &a.Label, &a.MIMEType, &a.SizeBytes, &a.SHA256, &a.CreatedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *tutorialRepository) GetAssetByKey(ctx context.Context, key string) (*service.TutorialAsset, error) {
	var a service.TutorialAsset
	err := r.db.QueryRowContext(ctx, "SELECT id,tutorial_id,kind,storage_key,public_url,original_name,label,mime_type,size_bytes,sha256,created_by,created_at FROM tutorial_assets WHERE storage_key=$1", key).Scan(&a.ID, &a.TutorialID, &a.Kind, &a.StorageKey, &a.PublicURL, &a.OriginalName, &a.Label, &a.MIMEType, &a.SizeBytes, &a.SHA256, &a.CreatedBy, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrTutorialNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *tutorialRepository) CreateAsset(ctx context.Context, a *service.TutorialAsset) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO tutorial_assets (tutorial_id,kind,storage_key,public_url,original_name,label,mime_type,size_bytes,sha256,created_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id,created_at`, a.TutorialID, a.Kind, a.StorageKey, a.PublicURL, a.OriginalName, a.Label, a.MIMEType, a.SizeBytes, a.SHA256, a.CreatedBy).Scan(&a.ID, &a.CreatedAt)
}

func (r *tutorialRepository) DeleteAsset(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM tutorial_assets WHERE id=$1", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return service.ErrTutorialNotFound
	}
	return nil
}
