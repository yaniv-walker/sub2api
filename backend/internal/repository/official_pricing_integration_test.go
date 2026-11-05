//go:build integration

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// Independent model documents must survive concurrent settings upserts and a
// fresh service instance. This uses the production Ent repository and PostgreSQL.
func TestOfficialPricingConcurrentModelPersistence(t *testing.T) {
	ctx := context.Background()
	// Concurrent writes need independent pooled connections, not one SQL tx.
	client := testEntClient(t)
	repo := NewSettingRepository(client)
	t.Cleanup(func() {
		for i := 0; i < 12; i++ {
			_ = service.NewOfficialPricingService(repo, nil, nil, nil).Delete(context.Background(), "openai", fmt.Sprintf("official-test-%d", i))
		}
	})
	svc := service.NewOfficialPricingService(repo, nil, nil, nil)
	errs := make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			value := float64(index) * 1e-6
			_, err := svc.Save(ctx, "openai", fmt.Sprintf("official-test-%d", index), service.OfficialPrice{InputPrice: &value})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	reloaded := service.NewOfficialPricingService(repo, nil, nil, nil)
	for i := 0; i < 12; i++ {
		entry, err := reloaded.Get(ctx, "openai", fmt.Sprintf("official-test-%d", i))
		require.NoError(t, err)
		require.Equal(t, float64(i)*1e-6, *entry.Pricing.InputPrice)
	}
	require.NoError(t, reloaded.Delete(ctx, "openai", "official-test-0"))
	other, err := reloaded.Get(ctx, "openai", "official-test-1")
	require.NoError(t, err)
	require.NotNil(t, other.Override)
}
