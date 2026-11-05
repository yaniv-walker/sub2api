//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountModelCatalogCASAndSaveEvent(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := newAccountRepositoryWithSQL(client, integrationDB, nil)
	a := &service.Account{Name: "catalog-integration", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{}}
	require.NoError(t, repo.Create(ctx, a))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM scheduler_outbox WHERE account_id = $1", a.ID)
		_ = client.Account.DeleteOneID(a.ID).Exec(ctx)
	})
	var trigger bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COALESCE((payload->>'model_catalog_sync')::boolean, false) FROM scheduler_outbox WHERE account_id=$1 ORDER BY id DESC LIMIT 1", a.ID).Scan(&trigger))
	require.True(t, trigger, "a successful save must enqueue automatic discovery")
	before, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	next := service.AccountModelCatalogSnapshot{Models: []string{"discovered"}, Status: "ready", SyncedAt: time.Now().UTC().Format(time.RFC3339Nano), ConfigUpdatedAt: before.UpdatedAt}
	applied, err := repo.CompareAndSwapModelCatalog(ctx, before, next)
	require.NoError(t, err)
	require.True(t, applied)
	loaded, err := repo.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, before.UpdatedAt, loaded.UpdatedAt)
	require.Equal(t, []string{"discovered"}, loaded.GetModelCatalogSnapshot().Models)
	applied, err = repo.CompareAndSwapModelCatalog(ctx, before, next)
	require.NoError(t, err)
	require.False(t, applied, "the previous snapshot cannot overwrite the winner")
	_, err = client.Account.UpdateOneID(a.ID).SetName("new configuration").Save(ctx)
	require.NoError(t, err)
	applied, err = repo.CompareAndSwapModelCatalog(ctx, loaded, next)
	require.NoError(t, err)
	require.False(t, applied, "old configuration revisions cannot publish a discovery")
}
