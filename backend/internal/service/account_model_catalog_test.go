//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func catalogAccount(models ...string) Account {
	a := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, GroupIDs: []int64{10}}
	a.Extra = map[string]any{AccountModelCatalogExtraKey: AccountModelCatalogSnapshot{Models: models, SyncedAt: "2026-11-05T00:00:00Z", Status: "ready"}}
	return a
}

func TestModelPlazaAccountCatalogMappingAndRestrictions(t *testing.T) {
	a := catalogAccount("upstream-a", "native", "blocked")
	a.Credentials = map[string]any{"model_mapping": map[string]any{"public-a": "upstream-a", "native": "native", "missing": "absent"}}
	ch := Channel{Status: StatusActive, GroupIDs: []int64{10}, RestrictModels: true,
		ModelMapping: map[string]map[string]string{PlatformOpenAI: {"shop-alias": "public-a"}},
		ModelPricing: []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"shop-alias", "native"}}},
	}
	models := ModelPlazaAccountModels([]Account{a}, []Channel{ch})
	require.Equal(t, []string{"native", "shop-alias"}, catalogModelNames(models[10]))
	require.Empty(t, models[99])
	// Losing one of two accounts does not remove a model supported by the other.
	b := catalogAccount("extra")
	b.ID = 2
	models = ModelPlazaAccountModels([]Account{a, b}, nil)
	require.Equal(t, []string{"extra", "native", "public-a"}, catalogModelNames(models[10]))
	a.Status = StatusDisabled
	models = ModelPlazaAccountModels([]Account{a, b}, nil)
	require.Equal(t, []string{"extra"}, catalogModelNames(models[10]))
}

func TestModelPlazaCatalogEmptySuccessDoesNotFallBack(t *testing.T) {
	a := catalogAccount()
	a.Credentials = map[string]any{"model_mapping": map[string]any{"old-model": "old-model"}}
	require.Empty(t, AccountCatalogModels(&a), "a successful empty upstream directory must remove old models")
	delete(a.Extra, AccountModelCatalogExtraKey)
	require.Contains(t, AccountCatalogModels(&a), "old-model", "before first successful discovery preserve local mapping fallback")
}

type modelCatalogTestRepo struct {
	AccountRepository
	account *Account
	saved   *AccountModelCatalogSnapshot
	applied bool
}

func (r *modelCatalogTestRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func (r *modelCatalogTestRepo) ListActive(context.Context) ([]Account, error) { return nil, nil }
func (r *modelCatalogTestRepo) CompareAndSwapModelCatalog(_ context.Context, _ *Account, next AccountModelCatalogSnapshot) (bool, error) {
	r.saved = &next
	return r.applied, nil
}

func TestAccountModelCatalogFailureRetainsLastSuccess(t *testing.T) {
	a := catalogAccount("known-good")
	r := &modelCatalogTestRepo{account: &a, applied: true}
	q := NewAccountModelCatalogQueue()
	q.fetch = func(context.Context, *Account) (*UpstreamModelCatalog, error) {
		return nil, errors.New("private credential must not be persisted")
	}
	q.repo = r
	q.syncAccount(context.Background(), 1, 0)
	require.NotNil(t, r.saved)
	require.Equal(t, []string{"known-good"}, r.saved.Models)
	require.Equal(t, "error", r.saved.Status)
	require.Equal(t, "upstream_model_sync_failed", r.saved.Error)
	require.Equal(t, "2026-11-05T00:00:00Z", r.saved.SyncedAt)
}

func TestAccountModelCatalogFetchUsesNonPersistingClone(t *testing.T) {
	a := catalogAccount("old")
	r := &modelCatalogTestRepo{account: &a, applied: true}
	q := NewAccountModelCatalogQueue()
	q.repo = r
	q.fetch = func(ctx context.Context, clone *Account) (*UpstreamModelCatalog, error) {
		require.Equal(t, a.ID, clone.ID, "OAuth token caches must retain account identity")
		require.True(t, modelCatalogPreview(ctx))
		return &UpstreamModelCatalog{Models: []string{"new", "new"}}, nil
	}
	q.syncAccount(context.Background(), 1, 0)
	require.Equal(t, []string{"new"}, r.saved.Models)
	require.Equal(t, int64(1), a.ID)
}

func TestAccountModelCatalogQueueDeduplicatesAndStops(t *testing.T) {
	q := NewAccountModelCatalogQueue()
	for range 20 {
		q.Enqueue(10)
	}
	require.Len(t, q.pending, 1)
	q.Stop()
	done := make(chan struct{})
	go func() { q.Enqueue(11); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("enqueue after shutdown blocked")
	}
}

func catalogModelNames(models []SupportedModel) []string {
	names := make([]string, 0, len(models))
	for _, m := range models {
		names = append(names, m.Name)
	}
	return names
}

func TestAccountModelCatalogStaleFetchIsDiscarded(t *testing.T) {
	a := catalogAccount("old")
	r := &modelCatalogTestRepo{account: &a, applied: true}
	q := NewAccountModelCatalogQueue()
	q.repo = r
	q.fetch = func(context.Context, *Account) (*UpstreamModelCatalog, error) {
		q.Enqueue(a.ID) // a newer successful save arrived while the request ran
		return &UpstreamModelCatalog{Models: []string{"stale"}}, nil
	}
	q.syncAccount(context.Background(), a.ID, 0)
	require.Nil(t, r.saved)
	require.Len(t, q.pending, 1)
	q.Stop()
}

func TestAccountModelCatalogTimeoutRetainsModelsAndRecordsFailure(t *testing.T) {
	a := catalogAccount("known-good")
	r := &modelCatalogTestRepo{account: &a, applied: true}
	q := NewAccountModelCatalogQueue()
	defer q.Stop()
	q.repo = r
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.fetch = func(context.Context, *Account) (*UpstreamModelCatalog, error) {
		cancel()
		return nil, context.DeadlineExceeded
	}
	q.syncAccount(ctx, a.ID, 0)
	require.NotNil(t, r.saved)
	require.Equal(t, "error", r.saved.Status)
	require.Equal(t, []string{"known-good"}, r.saved.Models)
}

func TestAccountModelCatalogConcurrentConfigurationChangeRetries(t *testing.T) {
	a := catalogAccount("old")
	r := &modelCatalogTestRepo{account: &a, applied: false}
	q := NewAccountModelCatalogQueue()
	q.repo = r
	q.fetch = func(context.Context, *Account) (*UpstreamModelCatalog, error) {
		return &UpstreamModelCatalog{Models: []string{"new"}}, nil
	}
	q.syncAccount(context.Background(), a.ID, 0)
	require.Len(t, q.pending, 1, "CAS rejection must retry the latest configuration")
	q.Stop()
}

func TestAccountModelCatalogOnlyExplicitSaveEventsEnqueueDiscovery(t *testing.T) {
	cache := newBulkEventSnapshotCache()
	repo := newBulkEventAccountRepo(&Account{ID: 1, Platform: PlatformOpenAI, GroupIDs: []int64{10}})
	svc := newBulkEventTestService(cache, repo)
	q := NewAccountModelCatalogQueue()
	defer q.Stop()
	svc.SetModelCatalogQueue(q)
	event := SchedulerOutboxEvent{EventType: SchedulerOutboxEventAccountBulkChanged, Payload: bulkEventPayload([]int64{1}, []int64{10})}
	require.NoError(t, svc.handleOutboxEvent(context.Background(), event, make(map[batchSeenKey]struct{})))
	require.Empty(t, q.pending, "ordinary scheduler observations must not call upstream")
	event.Payload["model_catalog_sync"] = true
	require.NoError(t, svc.handleOutboxEvent(context.Background(), event, make(map[batchSeenKey]struct{})))
	require.Len(t, q.pending, 1, "administrator save events must request discovery")
}
