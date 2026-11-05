package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const AccountModelCatalogExtraKey = "model_square_catalog"

// AccountModelCatalogSnapshot records discovery independently of capability
// enrichment: an upstream model need not publish context windows to be listed.
// Models remains the last successful list when a later attempt fails.
type AccountModelCatalogSnapshot struct {
	Models          []string  `json:"models"`
	ConfigUpdatedAt time.Time `json:"config_updated_at"`
	SyncedAt        string    `json:"synced_at,omitempty"`
	LastAttempt     string    `json:"last_attempt"`
	Status          string    `json:"status"`
	Error           string    `json:"error,omitempty"`
}

func (a *Account) GetModelCatalogSnapshot() *AccountModelCatalogSnapshot {
	if a == nil || a.Extra == nil {
		return nil
	}
	raw, ok := a.Extra[AccountModelCatalogExtraKey]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var snapshot AccountModelCatalogSnapshot
	if json.Unmarshal(b, &snapshot) != nil {
		return nil
	}
	return &snapshot
}

// AccountModelCatalogRepository is optional so the broad account repository
// interface and its existing test doubles do not gain unrelated methods.
type AccountModelCatalogRepository interface {
	CompareAndSwapModelCatalog(context.Context, *Account, AccountModelCatalogSnapshot) (bool, error)
}

// AccountModelCatalogQueue receives durable account-change events from the
// scheduler outbox. It only queues work on that critical path; discovery runs
// on a separate bounded worker and never delays account saves or scheduling.
type AccountModelCatalogQueue struct {
	mu        sync.Mutex
	pending   map[int64]uint64
	versions  map[int64]uint64
	wake      chan struct{}
	ctx       context.Context
	cancel    context.CancelFunc
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup
	repo      AccountRepository
	fetch     func(context.Context, *Account) (*UpstreamModelCatalog, error)
}

func NewAccountModelCatalogQueue() *AccountModelCatalogQueue {
	ctx, cancel := context.WithCancel(context.Background())
	return &AccountModelCatalogQueue{pending: make(map[int64]uint64), versions: make(map[int64]uint64), wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel}
}

func (q *AccountModelCatalogQueue) Enqueue(id int64) {
	if q == nil || id <= 0 {
		return
	}
	q.mu.Lock()
	if q.ctx.Err() != nil {
		q.mu.Unlock()
		return
	}
	q.versions[id]++
	q.pending[id] = q.versions[id]
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *AccountModelCatalogQueue) Start(repo AccountRepository, tests *AccountTestService) {
	if q == nil || repo == nil || tests == nil {
		return
	}
	q.startOnce.Do(func() {
		q.repo, q.fetch = repo, tests.SyncUpstreamModelCatalog
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			q.initialize()
			for {
				select {
				case <-q.ctx.Done():
					return
				case <-q.wake:
				}
				// Merge the create/update and final group-binding events from one save.
				timer := time.NewTimer(300 * time.Millisecond)
				select {
				case <-q.ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				for {
					id, version, ok := q.take()
					if !ok {
						break
					}
					ctx, cancel := context.WithTimeout(q.ctx, 45*time.Second)
					q.syncAccount(ctx, id, version)
					cancel()
					if q.ctx.Err() != nil {
						return
					}
				}
			}
		}()
	})
}

func (q *AccountModelCatalogQueue) Stop() {
	if q == nil {
		return
	}
	q.stopOnce.Do(q.cancel)
	q.wg.Wait()
}

func (q *AccountModelCatalogQueue) initialize() {
	ctx, cancel := context.WithTimeout(q.ctx, 30*time.Second)
	defer cancel()
	accounts, err := q.repo.ListActive(ctx)
	if err != nil {
		slog.Warn("model catalog initialization failed", "error", err)
		return
	}
	for i := range accounts {
		// Also retry failed/disrupted attempts after a process restart. Existing
		// successful snapshots are reused rather than calling every upstream.
		if snapshot := accounts[i].GetModelCatalogSnapshot(); snapshot == nil || snapshot.Status != "ready" || !snapshot.ConfigUpdatedAt.Equal(accounts[i].UpdatedAt) {
			q.Enqueue(accounts[i].ID)
		}
	}
}

func (q *AccountModelCatalogQueue) take() (int64, uint64, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for id, version := range q.pending {
		delete(q.pending, id)
		return id, version, true
	}
	return 0, 0, false
}

func (q *AccountModelCatalogQueue) current(id int64, version uint64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.versions[id] == version
}

func (q *AccountModelCatalogQueue) syncAccount(ctx context.Context, id int64, version uint64) {
	writer, ok := q.repo.(AccountModelCatalogRepository)
	if !ok {
		return
	}
	a, err := q.repo.GetByID(ctx, id)
	if err != nil || a == nil || !a.IsActive() || a.ParentAccountID != nil || a.IsSyntheticUITest() {
		return
	}
	// Keep account identity for OAuth token caches and connection isolation, but
	// prevent the existing synchronizer from persisting observations before CAS.
	clone := *a
	catalog, fetchErr := q.fetch(context.WithValue(ctx, modelCatalogPreviewContextKey{}, true), &clone)
	if q.ctx.Err() != nil || !q.current(id, version) {
		return
	}
	if ctx.Err() != nil {
		fetchErr = ctx.Err()
		// Persist the timeout using a fresh, bounded lifecycle context, while
		// shutdown and stale-save guards still prevent late observations.
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(q.ctx, 5*time.Second)
		defer cancel()
	}
	next := AccountModelCatalogSnapshot{}
	if previous := a.GetModelCatalogSnapshot(); previous != nil {
		next = *previous
	}
	next.LastAttempt = time.Now().UTC().Format(time.RFC3339Nano)
	next.ConfigUpdatedAt = a.UpdatedAt
	if fetchErr != nil || catalog == nil {
		next.Status, next.Error = "error", "upstream_model_sync_failed"
	} else {
		next.Models = dedupeAndSortModelIDs(catalog.Models)
		if next.Models == nil {
			next.Models = []string{}
		}
		next.SyncedAt, next.Status, next.Error = next.LastAttempt, "ready", ""
	}
	applied, saveErr := writer.CompareAndSwapModelCatalog(ctx, a, next)
	if saveErr != nil {
		slog.Warn("model catalog snapshot save failed", "account_id", id, "error", saveErr)
		return
	}
	if !applied {
		// A concurrent save or another process won the compare-and-swap. Reload
		// the current configuration instead of overwriting its newer snapshot.
		q.Enqueue(id)
	}
}

type modelCatalogPreviewContextKey struct{}

func modelCatalogPreview(ctx context.Context) bool {
	preview, _ := ctx.Value(modelCatalogPreviewContextKey{}).(bool)
	return preview
}

// AccountCatalogModels applies the account's existing request support/mapping
// rules to a live directory. Before discovery, local mappings and curated
// platform defaults are an explicit fallback, never the global price catalog.
func AccountCatalogModels(a *Account) []string {
	if a == nil {
		return nil
	}
	snapshot := a.GetModelCatalogSnapshot()
	known := snapshot != nil && snapshot.SyncedAt != ""
	var upstream []string
	if known {
		upstream = append(upstream, snapshot.Models...)
	} else {
		if metadata := a.GetUpstreamModelMetadataSnapshot(); metadata != nil {
			for id := range metadata.Models {
				upstream = append(upstream, id)
			}
		}
		switch a.Platform {
		case PlatformAnthropic:
			upstream = append(upstream, claude.DefaultModelIDs()...)
		case PlatformOpenAI:
			for _, m := range openai.DefaultModels {
				upstream = append(upstream, m.ID)
			}
		case PlatformGemini:
			for _, m := range geminicli.DefaultModels {
				upstream = append(upstream, m.ID)
			}
		}
	}
	upstreamSet := make(map[string]bool, len(upstream))
	for _, model := range upstream {
		upstreamSet[model] = true
	}
	var result []string
	add := func(id string) {
		if id == "" || strings.Contains(id, "*") || !a.IsModelSupported(id) {
			return
		}
		target := id
		if !a.IsOpenAIPassthroughEnabled() {
			target = a.GetMappedModel(id)
		}
		if known && !upstreamSet[target] {
			return
		}
		result = append(result, id)
	}
	for _, id := range upstream {
		add(id)
	}
	for id := range a.GetModelMapping() {
		add(id)
	}
	return dedupeAndSortModelIDs(result)
}

// ModelPlazaAccountModels is a read-only union over current account bindings.
// Calling it from each plaza query naturally removes deleted/disabled/moved
// accounts without a second persisted group-model directory to invalidate.
func ModelPlazaAccountModels(accounts []Account, channels []Channel) map[int64][]SupportedModel {
	byGroup := make(map[int64]map[string]SupportedModel)
	channelByGroup := make(map[int64]*Channel)
	for i := range channels {
		if channels[i].Status != StatusActive {
			continue
		}
		for _, id := range channels[i].GroupIDs {
			channelByGroup[id] = &channels[i]
		}
	}
	for i := range accounts {
		a := &accounts[i]
		if !a.IsActive() || !a.Schedulable || a.ParentAccountID != nil || a.IsSyntheticUITest() {
			continue
		}
		if a.AutoPauseOnExpired && a.ExpiresAt != nil && !a.ExpiresAt.After(time.Now()) {
			continue
		}
		models := AccountCatalogModels(a)
		for _, gid := range a.GroupIDs {
			ch := channelByGroup[gid]
			cache := newEmptyChannelCache()
			if ch != nil {
				expandPricingToCache(cache, ch, gid, a.Platform)
				expandMappingToCache(cache, ch, gid, a.Platform)
			}
			candidates := append([]string(nil), models...)
			if ch != nil {
				for _, m := range ch.SupportedModels() {
					if m.Platform == a.Platform {
						candidates = append(candidates, m.Name)
					}
				}
			}
			allowed := make(map[string]bool, len(models))
			for _, id := range models {
				allowed[id] = true
			}
			for _, id := range candidates {
				mapped := id
				if ch != nil {
					lk := &channelLookup{cache: cache, channel: ch, platform: a.Platform}
					if checkRestricted(lk, gid, id) {
						continue
					}
					mapped = resolveMapping(lk, gid, id).MappedModel
				}
				if !allowed[mapped] {
					continue
				}
				if byGroup[gid] == nil {
					byGroup[gid] = make(map[string]SupportedModel)
				}
				key := a.Platform + ":" + strings.ToLower(id)
				model := SupportedModel{Name: id, Platform: a.Platform}
				if ch != nil {
					model.Pricing = lookupPricingAcrossPlatforms(cache, gid, a.Platform, normalizeChannelPricingModelName(mapped))
				}
				byGroup[gid][key] = model
			}
		}
	}
	out := make(map[int64][]SupportedModel, len(byGroup))
	for gid, models := range byGroup {
		for _, m := range models {
			out[gid] = append(out[gid], m)
		}
		sort.Slice(out[gid], func(i, j int) bool {
			if out[gid][i].Name != out[gid][j].Name {
				return out[gid][i].Name < out[gid][j].Name
			}
			return out[gid][i].Platform < out[gid][j].Platform
		})
	}
	return out
}
