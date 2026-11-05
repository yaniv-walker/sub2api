package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type tutorialRepository struct{ db *sql.DB }

func NewTutorialRepository(db *sql.DB) service.TutorialRepository { return &tutorialRepository{db: db} }
func tutorialDecode(b []byte) (*service.Tutorial, error) {
	var t service.Tutorial
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	if t.Resources == nil {
		t.Resources = []service.TutorialResource{}
	}
	return &t, nil
}
func tutorialPayload(admin bool) string {
	if admin {
		return "to_jsonb(d)-'published_snapshot'"
	}
	return "published_snapshot"
}
func (r *tutorialRepository) List(ctx context.Context, p service.TutorialListParams) ([]service.Tutorial, int64, error) {
	args := []any{}
	where := []string{"1=1"}
	payload := tutorialPayload(p.Admin)
	if !p.Admin {
		where = append(where, "status='published' AND published_snapshot IS NOT NULL")
	}
	field := func(k string) string {
		if p.Admin {
			return k
		}
		return "published_snapshot->>'" + k + "'"
	}
	if p.PublicOnly {
		where = append(where, "published_snapshot->>'is_public'='true'")
	}
	if q := strings.TrimSpace(p.Query); q != "" {
		args = append(args, "%"+q+"%")
		n := len(args)
		where = append(where, fmt.Sprintf("(%s ILIKE $%d OR %s ILIKE $%d OR %s ILIKE $%d)", field("title"), n, field("summary"), n, field("content_markdown"), n))
	}
	if p.Category != "" {
		args = append(args, p.Category)
		where = append(where, fmt.Sprintf("%s=$%d", field("category"), len(args)))
	}
	base := " FROM tutorial_documents d WHERE " + strings.Join(where, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*)"+base, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "sort_order"
	if !p.Admin {
		order = "COALESCE((published_snapshot->>'sort_order')::int,100)"
	}
	args = append(args, p.PageSize, (p.Page-1)*p.PageSize)
	rows, err := r.db.QueryContext(ctx, "SELECT "+payload+base+" ORDER BY "+order+",id DESC LIMIT $"+fmt.Sprint(len(args)-1)+" OFFSET $"+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	out := []service.Tutorial{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, 0, err
		}
		t, err := tutorialDecode(b)
		if err != nil {
			return nil, 0, err
		}
		t.ContentHTML = ""
		t.ContentMarkdown = ""
		out = append(out, *t)
	}
	return out, total, rows.Err()
}
func (r *tutorialRepository) GetByID(ctx context.Context, id int64, admin bool) (*service.Tutorial, error) {
	return r.get(ctx, "id", id, admin)
}
func (r *tutorialRepository) GetBySlug(ctx context.Context, slug string, admin bool) (*service.Tutorial, error) {
	return r.get(ctx, "slug", slug, admin)
}
func (r *tutorialRepository) get(ctx context.Context, field string, value any, admin bool) (*service.Tutorial, error) {
	where := field + "=$1"
	if !admin {
		if field == "slug" {
			where = "published_snapshot->>'slug'=$1"
		}
		where += " AND status='published' AND published_snapshot IS NOT NULL"
	}
	var b []byte
	err := r.db.QueryRowContext(ctx, "SELECT "+tutorialPayload(admin)+" FROM tutorial_documents d WHERE "+where, value).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrTutorialNotFound
	}
	if err != nil {
		return nil, err
	}
	return tutorialDecode(b)
}
func revisionInsert(ctx context.Context, tx *sql.Tx, t *service.Tutorial) error {
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tutorial_revisions(tutorial_id,version,snapshot,created_by)VALUES($1,$2,$3,$4)`, t.ID, t.Version, b, t.UpdatedBy)
	return err
}
func validateAssetRefs(ctx context.Context, tx *sql.Tx, t *service.Tutorial) error {
	content := t.ContentMarkdown
	for _, r := range t.Resources {
		content += "\n" + r.URL
	}
	for _, key := range service.TutorialAssetKeys(content) {
		var aid int64
		if err := tx.QueryRowContext(ctx, "SELECT id FROM tutorial_assets WHERE storage_key=$1", key).Scan(&aid); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: missing asset %s", service.ErrTutorialInvalidInput, key)
			}
			return err
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO tutorial_asset_links(tutorial_id,asset_id)VALUES($1,$2) ON CONFLICT DO NOTHING", t.ID, aid); err != nil {
			return err
		}
	}
	return nil
}

// Reserve current and published paths in one namespace. The table lock also
// serializes publication, so a concurrent draft rename cannot steal a live URL.
func reserveTutorialSlug(ctx context.Context, tx *sql.Tx, id int64, slug string) error {
	if _, err := tx.ExecContext(ctx, "LOCK TABLE tutorial_documents IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return err
	}
	var used bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM tutorial_documents WHERE id<>$1 AND (slug=$2 OR published_snapshot->>'slug'=$2))", id, slug).Scan(&used); err != nil {
		return err
	}
	if used {
		return service.ErrTutorialSlugReserved
	}
	return nil
}
func (r *tutorialRepository) Create(ctx context.Context, t *service.Tutorial) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = reserveTutorialSlug(ctx, tx, t.ID, t.Slug); err != nil {
		return err
	}
	resources, _ := json.Marshal(t.Resources)
	err = tx.QueryRowContext(ctx, `INSERT INTO tutorial_documents(slug,title,summary,category,content_html,content_markdown,status,is_public,sort_order,created_by,updated_by,resources,source_origin,source_document_id,source_updated_at,source_relation)VALUES($1,$2,$3,$4,$5,$6,'draft',$7,$8,$9,$9,$10,$11,$12,$13,$14)RETURNING id,version,created_at,updated_at`, t.Slug, t.Title, t.Summary, t.Category, t.ContentHTML, t.ContentMarkdown, t.IsPublic, t.SortOrder, t.CreatedBy, resources, t.SourceOrigin, t.SourceDocumentID, t.SourceUpdatedAt, t.SourceRelation).Scan(&t.ID, &t.Version, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return err
	}
	t.Status = service.TutorialStatusDraft
	if err = validateAssetRefs(ctx, tx, t); err != nil {
		return err
	}
	if err = revisionInsert(ctx, tx, t); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *tutorialRepository) Update(ctx context.Context, t *service.Tutorial) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = reserveTutorialSlug(ctx, tx, t.ID, t.Slug); err != nil {
		return err
	}
	resources, _ := json.Marshal(t.Resources)
	err = tx.QueryRowContext(ctx, `UPDATE tutorial_documents SET slug=$1,title=$2,summary=$3,category=$4,content_html=$5,content_markdown=$6,is_public=$7,sort_order=$8,updated_by=$9,resources=$10,source_origin=$11,source_document_id=$12,source_updated_at=$13,source_relation=$14,version=version+1,updated_at=NOW()WHERE id=$15 AND version=$16 RETURNING version,updated_at`, t.Slug, t.Title, t.Summary, t.Category, t.ContentHTML, t.ContentMarkdown, t.IsPublic, t.SortOrder, t.UpdatedBy, resources, t.SourceOrigin, t.SourceDocumentID, t.SourceUpdatedAt, t.SourceRelation, t.ID, t.Version).Scan(&t.Version, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrTutorialConflict
	}
	if err != nil {
		return err
	}
	if err = validateAssetRefs(ctx, tx, t); err != nil {
		return err
	}
	if err = revisionInsert(ctx, tx, t); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *tutorialRepository) Delete(ctx context.Context, id, version int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM tutorial_documents WHERE id=$1 AND version=$2", id, version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrTutorialConflict
	}
	return nil
}
func tutorialAffected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrTutorialNotFound
	}
	return nil
}
func (r *tutorialRepository) SetStatus(ctx context.Context, id int64, status string, publishedAt *time.Time, actor, version int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "LOCK TABLE tutorial_documents IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return err
	}
	var b []byte
	err = tx.QueryRowContext(ctx, "SELECT to_jsonb(d)-'published_snapshot' FROM tutorial_documents d WHERE id=$1 AND version=$2 FOR UPDATE", id, version).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrTutorialConflict
	}
	if err != nil {
		return err
	}
	t, err := tutorialDecode(b)
	if err != nil {
		return err
	}
	if err = reserveTutorialSlug(ctx, tx, t.ID, t.Slug); err != nil {
		return err
	}
	t.Status = status
	t.Version++
	t.UpdatedBy = actor
	t.UpdatedAt = time.Now()
	if status == service.TutorialStatusPublished {
		t.PublishedAt = publishedAt
		markdown := t.ContentMarkdown
		if markdown == "" {
			markdown = t.ContentHTML
		}
		safe, renderErr := service.RenderTutorialMarkdown(markdown)
		if renderErr != nil {
			return renderErr
		}
		t.ContentHTML = safe
		if err = validateAssetRefs(ctx, tx, t); err != nil {
			return err
		}
	}
	snapshot, _ := json.Marshal(t)
	if status == service.TutorialStatusPublished {
		_, err = tx.ExecContext(ctx, "UPDATE tutorial_documents SET status=$1,published_at=$2,updated_by=$3,version=$4,updated_at=$5,published_snapshot=$6 WHERE id=$7", status, publishedAt, actor, t.Version, t.UpdatedAt, snapshot, id)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE tutorial_documents SET status=$1,updated_by=$2,version=$3,updated_at=$4 WHERE id=$5", status, actor, t.Version, t.UpdatedAt, id)
	}
	if err != nil {
		return err
	}
	if err = revisionInsert(ctx, tx, t); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *tutorialRepository) Revisions(ctx context.Context, id int64) ([]service.TutorialRevision, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT id,tutorial_id,version,snapshot,created_by,created_at FROM tutorial_revisions WHERE tutorial_id=$1 ORDER BY version DESC", id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []service.TutorialRevision{}
	for rows.Next() {
		var rev service.TutorialRevision
		var b []byte
		if err = rows.Scan(&rev.ID, &rev.TutorialID, &rev.Version, &b, &rev.CreatedBy, &rev.CreatedAt); err != nil {
			return nil, err
		}
		t, err := tutorialDecode(b)
		if err != nil {
			return nil, err
		}
		t.ContentHTML = ""
		t.ContentMarkdown = ""
		rev.Snapshot = *t
		out = append(out, rev)
	}
	return out, rows.Err()
}
func (r *tutorialRepository) GetRevision(ctx context.Context, id, rid int64) (*service.TutorialRevision, error) {
	var rev service.TutorialRevision
	var b []byte
	err := r.db.QueryRowContext(ctx, "SELECT id,tutorial_id,version,snapshot,created_by,created_at FROM tutorial_revisions WHERE tutorial_id=$1 AND id=$2", id, rid).Scan(&rev.ID, &rev.TutorialID, &rev.Version, &b, &rev.CreatedBy, &rev.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrTutorialNotFound
	}
	if err != nil {
		return nil, err
	}
	t, err := tutorialDecode(b)
	if err != nil {
		return nil, err
	}
	rev.Snapshot = *t
	return &rev, nil
}
func (r *tutorialRepository) ListAssets(ctx context.Context, tid int64) ([]service.TutorialAsset, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT to_jsonb(a) FROM tutorial_assets a JOIN tutorial_asset_links l ON l.asset_id=a.id WHERE l.tutorial_id=$1 ORDER BY a.id`, tid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []service.TutorialAsset{}
	for rows.Next() {
		var b []byte
		var a service.TutorialAsset
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(b, &a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (r *tutorialRepository) GetAssetByKey(ctx context.Context, key string) (*service.TutorialAsset, error) {
	var b []byte
	err := r.db.QueryRowContext(ctx, "SELECT to_jsonb(a) FROM tutorial_assets a WHERE storage_key=$1", key).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrTutorialNotFound
	}
	if err != nil {
		return nil, err
	}
	var a service.TutorialAsset
	err = json.Unmarshal(b, &a)
	return &a, err
}
func (r *tutorialRepository) CreateAsset(ctx context.Context, a *service.TutorialAsset) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var b []byte
	err = tx.QueryRowContext(ctx, `INSERT INTO tutorial_assets(tutorial_id,kind,storage_key,public_url,original_name,label,mime_type,size_bytes,sha256,created_by)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)ON CONFLICT(storage_key)DO UPDATE SET storage_key=EXCLUDED.storage_key RETURNING to_jsonb(tutorial_assets)`, a.TutorialID, a.Kind, a.StorageKey, a.PublicURL, a.OriginalName, a.Label, a.MIMEType, a.SizeBytes, a.SHA256, a.CreatedBy).Scan(&b)
	if err != nil {
		return err
	}
	tid := *a.TutorialID
	if err = json.Unmarshal(b, a); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO tutorial_asset_links(tutorial_id,asset_id)VALUES($1,$2) ON CONFLICT DO NOTHING", tid, a.ID); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *tutorialRepository) LinkAsset(ctx context.Context, tid, aid int64) error {
	_, err := r.db.ExecContext(ctx, "INSERT INTO tutorial_asset_links(tutorial_id,asset_id)VALUES($1,$2) ON CONFLICT DO NOTHING", tid, aid)
	return err
}
func (r *tutorialRepository) UpdateAssetLabel(ctx context.Context, tid, aid int64, label string) error {
	res, err := r.db.ExecContext(ctx, "UPDATE tutorial_assets SET label=$1 WHERE id=$2 AND EXISTS(SELECT 1 FROM tutorial_asset_links WHERE tutorial_id=$3 AND asset_id=$2)", label, aid, tid)
	return tutorialAffected(res, err)
}
func (r *tutorialRepository) AssetReferences(ctx context.Context, id int64) ([]service.Tutorial, error) {
	var key string
	err := r.db.QueryRowContext(ctx, "SELECT storage_key FROM tutorial_assets WHERE id=$1", id).Scan(&key)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT DISTINCT d.id,d.title,d.slug FROM tutorial_documents d WHERE content_markdown LIKE $1 OR published_snapshot::text LIKE $1 OR EXISTS(SELECT 1 FROM tutorial_revisions r WHERE r.tutorial_id=d.id AND r.snapshot::text LIKE $1) UNION SELECT 0::bigint,'售后设置','' FROM settings WHERE key='tutorial_support_v2' AND value LIKE $1`, "%/api/v1/tutorial-assets/"+key+"%")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []service.Tutorial{}
	for rows.Next() {
		var t service.Tutorial
		if err = rows.Scan(&t.ID, &t.Title, &t.Slug); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (r *tutorialRepository) DeleteAsset(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "LOCK TABLE tutorial_documents, settings IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return err
	}
	var key string
	if err = tx.QueryRowContext(ctx, "SELECT storage_key FROM tutorial_assets WHERE id=$1 FOR UPDATE", id).Scan(&key); err != nil {
		return err
	}
	var used bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tutorial_documents WHERE content_markdown LIKE $1 OR published_snapshot::text LIKE $1 UNION ALL SELECT 1 FROM tutorial_revisions WHERE snapshot::text LIKE $1 UNION ALL SELECT 1 FROM settings WHERE key='tutorial_support_v2' AND value LIKE $1)`, "%/api/v1/tutorial-assets/"+key+"%").Scan(&used)
	if err != nil {
		return err
	}
	if used {
		return service.ErrTutorialAssetInUse
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM tutorial_assets WHERE id=$1", id)
	if err = tutorialAffected(res, err); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *tutorialRepository) CanReadAsset(ctx context.Context, key string, id, version int64, scope string) (bool, error) {
	if scope == "admin" {
		var ok bool
		err := r.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM tutorial_asset_links l JOIN tutorial_assets a ON a.id=l.asset_id WHERE l.tutorial_id=$1 AND a.storage_key=$2)", id, key).Scan(&ok)
		return ok, err
	}
	t, err := r.GetByID(ctx, id, false)
	if err != nil {
		return false, err
	}
	if scope == "public" && !t.IsPublic {
		return false, nil
	}
	if t.Version != version {
		return false, nil
	}
	for _, k := range service.TutorialAssetKeys(t.ContentHTML + "\n" + t.ContentMarkdown) {
		if k == key {
			return true, nil
		}
	}
	for _, res := range t.Resources {
		for _, k := range service.TutorialAssetKeys(res.URL) {
			if k == key {
				return true, nil
			}
		}
	}
	return false, nil
}
