package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Runs against a caller-selected database in an isolated, temporary schema.
// Example: TUTORIAL_TEST_DATABASE_URL='host=/var/run/postgresql user=sub2api dbname=sub2api sslmode=disable'.
func tutorialPostgresTestDB(t *testing.T) *sql.DB {
	dsn := os.Getenv("TUTORIAL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TUTORIAL_TEST_DATABASE_URL for the real PostgreSQL integration test")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	require.NoError(t, db.Ping())
	schema := fmt.Sprintf("tutorial_test_%d", time.Now().UnixNano())
	_, err = db.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.Exec("DROP SCHEMA " + schema + " CASCADE") })
	_, err = db.Exec("SET search_path TO " + schema)
	require.NoError(t, err)
	dir := os.Getenv("TUTORIAL_TEST_MIGRATIONS_DIR")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "migrations")
	}
	for _, file := range []string{"242_tutorial_center.sql", "257_tutorial_authoring.sql"} {
		b, e := os.ReadFile(filepath.Join(dir, file))
		require.NoError(t, e)
		_, e = db.Exec(string(b)) // #nosec G701 -- Only the two explicitly named, checked-in migration fixtures are executed; no request input.
		require.NoError(t, e)
	}
	_, err = db.Exec(`CREATE TABLE settings(id BIGSERIAL PRIMARY KEY,key TEXT UNIQUE NOT NULL,value TEXT NOT NULL,updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`)
	require.NoError(t, err)
	return db
}
func TestTutorialPostgresPublicationCASAndSharedAssets(t *testing.T) {
	db := tutorialPostgresTestDB(t)
	var err error
	ctx := context.Background()
	repo := NewTutorialRepository(db)
	svc := service.NewTutorialService(repo)
	a := &service.Tutorial{Slug: "first", Title: "First", ContentMarkdown: "# Original", CreatedBy: 1, UpdatedBy: 1, IsPublic: true}
	require.NoError(t, svc.Create(ctx, a))
	require.EqualValues(t, 1, a.Version)
	asset := &service.TutorialAsset{TutorialID: &a.ID, Kind: "image", StorageKey: "shared.png", PublicURL: "/api/v1/tutorial-assets/shared.png", OriginalName: "a.png", Label: "a", MIMEType: "image/png", SizeBytes: 12, SHA256: "abc", CreatedBy: 1}
	require.NoError(t, svc.CreateAsset(ctx, asset))
	originalAssetID := asset.ID
	a.ContentMarkdown = "# Original\n\n![image](/api/v1/tutorial-assets/shared.png)"
	require.NoError(t, svc.Update(ctx, a))
	require.EqualValues(t, 2, a.Version)
	require.NoError(t, svc.SetStatus(ctx, a.ID, "published", 1, a.Version))
	published, err := svc.GetByID(ctx, a.ID, false)
	require.NoError(t, err)
	require.Contains(t, published.ContentMarkdown, "Original")
	require.EqualValues(t, 3, published.Version)
	draft, err := svc.GetByID(ctx, a.ID, true)
	require.NoError(t, err)
	stale := *draft
	draft.Title = "Draft only"
	draft.ContentMarkdown = "# Changed"
	require.NoError(t, svc.Update(ctx, draft))
	unchanged, err := svc.GetByID(ctx, a.ID, false)
	require.NoError(t, err)
	require.Equal(t, "First", unchanged.Title)
	require.Contains(t, unchanged.ContentMarkdown, "Original")
	require.ErrorIs(t, svc.Update(ctx, &stale), service.ErrTutorialConflict)
	second := &service.Tutorial{Slug: "second", Title: "Second", ContentMarkdown: "# Another\n\n![shared](/api/v1/tutorial-assets/shared.png)", CreatedBy: 1, UpdatedBy: 1}
	require.NoError(t, svc.Create(ctx, second))
	duplicate := *asset
	duplicate.ID = 0
	duplicate.TutorialID = &second.ID
	require.NoError(t, svc.CreateAsset(ctx, &duplicate))
	require.Equal(t, originalAssetID, duplicate.ID)
	linked, err := svc.ListAssets(ctx, second.ID)
	require.NoError(t, err)
	require.Len(t, linked, 1)
	require.Equal(t, originalAssetID, linked[0].ID)
	require.ErrorIs(t, svc.DeleteAsset(ctx, asset.ID), service.ErrTutorialAssetInUse)
	refs, err := svc.AssetReferences(ctx, asset.ID)
	require.NoError(t, err)
	require.Len(t, refs, 2)
	allowed, err := svc.CanReadAsset(ctx, asset.StorageKey, a.ID, published.Version, "published")
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = svc.CanReadAsset(ctx, asset.StorageKey, a.ID, draft.Version, "published")
	require.NoError(t, err)
	require.False(t, allowed)
	revisions, err := svc.Revisions(ctx, a.ID)
	require.NoError(t, err)
	require.Len(t, revisions, 4)
	require.Empty(t, revisions[0].Snapshot.ContentMarkdown)
	restored, err := svc.Restore(ctx, a.ID, revisions[len(revisions)-1].ID, draft.Version, 1)
	require.NoError(t, err)
	require.Equal(t, "# Original", restored.ContentMarkdown)
	stillPublished, err := svc.GetByID(ctx, a.ID, false)
	require.NoError(t, err)
	require.Equal(t, published.Version, stillPublished.Version)
	list, total, err := svc.List(ctx, service.TutorialListParams{Page: 1, PageSize: 20, PublicOnly: true})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, list, 1)
	require.Empty(t, list[0].ContentMarkdown)
	require.ErrorIs(t, svc.SetStatus(ctx, a.ID, "offline", 1, draft.Version), service.ErrTutorialConflict)
	require.NoError(t, svc.SetStatus(ctx, a.ID, "offline", 1, restored.Version))
	_, err = svc.GetByID(ctx, a.ID, false)
	require.ErrorIs(t, err, service.ErrTutorialNotFound)
	require.ErrorIs(t, svc.Delete(ctx, a.ID, 1), service.ErrTutorialConflict)
	final, err := svc.GetByID(ctx, a.ID, true)
	require.NoError(t, err)
	require.NoError(t, svc.Delete(ctx, a.ID, final.Version))
	_, err = svc.GetByID(ctx, a.ID, true)
	require.ErrorIs(t, err, service.ErrTutorialNotFound)
}

func TestTutorialPostgresReservesPublishedSlugs(t *testing.T) {
	db := tutorialPostgresTestDB(t)
	ctx := context.Background()
	svc := service.NewTutorialService(NewTutorialRepository(db))
	first := &service.Tutorial{Slug: "original", Title: "First", ContentMarkdown: "# First", CreatedBy: 1, UpdatedBy: 1}
	require.NoError(t, svc.Create(ctx, first))
	require.NoError(t, svc.SetStatus(ctx, first.ID, "published", 1, first.Version))
	draft, err := svc.GetByID(ctx, first.ID, true)
	require.NoError(t, err)
	draft.Slug = "renamed"
	require.NoError(t, svc.Update(ctx, draft))
	second := &service.Tutorial{Slug: "original", Title: "Second", ContentMarkdown: "# Second", CreatedBy: 1, UpdatedBy: 1}
	require.ErrorIs(t, svc.Create(ctx, second), service.ErrTutorialSlugReserved, "the published path must remain reserved while a new slug is only a draft")
	old, err := svc.GetBySlug(ctx, "original", false)
	require.NoError(t, err)
	require.Equal(t, first.ID, old.ID)
	require.NoError(t, svc.SetStatus(ctx, first.ID, "offline", 1, draft.Version))
	require.ErrorIs(t, svc.Create(ctx, second), service.ErrTutorialSlugReserved, "offline snapshots retain their path until republished with the new slug")
	current, err := svc.GetByID(ctx, first.ID, true)
	require.NoError(t, err)
	require.NoError(t, svc.SetStatus(ctx, first.ID, "published", 1, current.Version))
	require.NoError(t, svc.Create(ctx, second))
	require.NoError(t, svc.SetStatus(ctx, second.ID, "published", 1, second.Version))
	old, err = svc.GetBySlug(ctx, "original", false)
	require.NoError(t, err)
	require.Equal(t, second.ID, old.ID)
	renamed, err := svc.GetBySlug(ctx, "renamed", false)
	require.NoError(t, err)
	require.Equal(t, first.ID, renamed.ID)
}
func TestTutorialPostgresProtectsSupportOnlyQRCodeAsset(t *testing.T) {
	db := tutorialPostgresTestDB(t)
	ctx := context.Background()
	svc := service.NewTutorialService(NewTutorialRepository(db))
	doc := &service.Tutorial{Slug: "asset-owner", Title: "Owner", ContentMarkdown: "# No body image reference", CreatedBy: 1, UpdatedBy: 1}
	require.NoError(t, svc.Create(ctx, doc))
	asset := &service.TutorialAsset{TutorialID: &doc.ID, Kind: "image", StorageKey: "support.png", PublicURL: "/api/v1/tutorial-assets/support.png", OriginalName: "qr.png", Label: "QR", MIMEType: "image/png", SizeBytes: 12, SHA256: "abc", CreatedBy: 1}
	require.NoError(t, svc.CreateAsset(ctx, asset))
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	settings := service.NewSettingService(NewSettingRepository(client), nil)
	require.NoError(t, settings.UpdateTutorialSupport(ctx, service.TutorialSupport{QQQRURL: asset.PublicURL}))
	require.ErrorIs(t, svc.DeleteAsset(ctx, asset.ID), service.ErrTutorialAssetInUse)
	refs, err := svc.AssetReferences(ctx, asset.ID)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.Equal(t, "售后设置", refs[0].Title)
	require.NoError(t, settings.UpdateTutorialSupport(ctx, service.TutorialSupport{}))
	require.NoError(t, svc.DeleteAsset(ctx, asset.ID))
	require.ErrorIs(t, settings.UpdateTutorialSupport(ctx, service.TutorialSupport{QQQRURL: asset.PublicURL}), service.ErrTutorialInvalidInput)
	saved, err := settings.GetTutorialSupport(ctx)
	require.NoError(t, err)
	require.Empty(t, saved.QQQRURL, "a stale settings form must not recreate a dangling QR reference")
}
