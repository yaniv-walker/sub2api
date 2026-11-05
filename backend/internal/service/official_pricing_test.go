package service

import (
	"context"
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type officialPricingTestRepo struct {
	SettingRepository
	mu         sync.Mutex
	values     map[string]string
	batchReads int
}

func (r *officialPricingTestRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batchReads++
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := r.values[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

func TestOfficialPricingBatchUsesOneReadAndOriginalIdentity(t *testing.T) {
	repo := &officialPricingTestRepo{values: map[string]string{}}
	svc := NewOfficialPricingService(repo, nil, nil, nil)
	zero := 0.0
	_, err := svc.Save(context.Background(), "openai", "model", OfficialPrice{InputPrice: &zero})
	require.NoError(t, err)
	ids := []OfficialPricingIdentity{{Platform: "openai", Model: "MODEL"}, {Platform: "anthropic", Model: "model"}}
	entries, err := svc.GetMany(context.Background(), ids)
	require.NoError(t, err)
	require.Equal(t, 1, repo.batchReads)
	require.Equal(t, 0.0, *entries[ids[0]].Pricing.InputPrice)
	require.Nil(t, entries[ids[1]].Override)
}

func (r *officialPricingTestRepo) GetValue(_ context.Context, k string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.values[k]
	if !ok {
		return "", ErrSettingNotFound
	}
	return v, nil
}
func (r *officialPricingTestRepo) Set(_ context.Context, k, v string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[k] = v
	return nil
}
func (r *officialPricingTestRepo) Delete(_ context.Context, k string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.values, k)
	return nil
}
func (r *officialPricingTestRepo) GetAll(_ context.Context) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]string{}
	for k, v := range r.values {
		out[k] = v
	}
	return out, nil
}

func TestOfficialPricingPartialOverridePreservesZeroAndBilling(t *testing.T) {
	ctx := context.Background()
	repo := &officialPricingTestRepo{values: map[string]string{}}
	pricing := &PricingService{pricingData: map[string]*LiteLLMModelPricing{"test-model": {InputCostPerToken: 2e-6, OutputCostPerToken: 8e-6, LiteLLMProvider: "openai"}}}
	billing := &BillingService{pricingService: pricing}
	svc := NewOfficialPricingService(repo, pricing, billing, nil)
	zero := 0.0
	_, err := svc.Save(ctx, "openai", " TEST-MODEL ", OfficialPrice{InputPrice: &zero})
	require.NoError(t, err)
	entry, err := svc.Get(ctx, "openai", "test-model")
	require.NoError(t, err)
	require.Equal(t, "manual", entry.Source)
	require.Equal(t, 0.0, *entry.Pricing.InputPrice)
	require.Equal(t, 8e-6, *entry.Pricing.OutputPrice)
	require.Equal(t, 2e-6, *entry.Automatic.InputPrice)
	billed, err := billing.GetModelPricing("test-model")
	require.NoError(t, err)
	require.Equal(t, 2e-6, billed.InputPricePerToken)
	other, err := svc.Get(ctx, "anthropic", "test-model")
	require.NoError(t, err)
	require.Nil(t, other.Override)
	require.NoError(t, svc.Delete(ctx, "openai", "test-model"))
	restored, err := svc.Get(ctx, "openai", "test-model")
	require.NoError(t, err)
	require.Equal(t, 2e-6, *restored.Pricing.InputPrice)
}

func TestOfficialPricingWholeTierReplacementAndIndependentModels(t *testing.T) {
	ctx := context.Background()
	repo := &officialPricingTestRepo{values: map[string]string{}}
	svc := NewOfficialPricingService(repo, nil, nil, nil)
	zero := 0.0
	tiers := []PricingInterval{{MinTokens: 0, InputPrice: &zero}}
	_, err := svc.Save(ctx, "openai", "first", OfficialPrice{Intervals: tiers})
	require.NoError(t, err)
	_, err = svc.Save(ctx, "openai", "second", OfficialPrice{InputPrice: &zero})
	require.NoError(t, err)
	items, err := svc.List(ctx, "openai", "")
	require.NoError(t, err)
	require.Len(t, items, 2)
	_, err = svc.Save(ctx, "openai", "first", OfficialPrice{Intervals: []PricingInterval{}})
	require.NoError(t, err)
	first, err := svc.Get(ctx, "openai", "first")
	require.NoError(t, err)
	require.NotNil(t, first.Pricing.Intervals)
	require.Empty(t, first.Pricing.Intervals)
	require.NoError(t, svc.Delete(ctx, "openai", "first"))
	second, err := svc.Get(ctx, "openai", "second")
	require.NoError(t, err)
	require.Equal(t, 0.0, *second.Pricing.InputPrice)
}

func TestOfficialPricingRejectsInvalidValuesAndIdentity(t *testing.T) {
	svc := NewOfficialPricingService(&officialPricingTestRepo{values: map[string]string{}}, nil, nil, nil)
	for _, value := range []float64{-1, math.Inf(1), math.NaN()} {
		_, err := svc.Save(context.Background(), "openai", "model", OfficialPrice{InputPrice: &value})
		require.Error(t, err)
	}
	_, err := svc.Save(context.Background(), "made-up", "model", OfficialPrice{})
	require.Error(t, err)
	_, err = svc.Save(context.Background(), "openai", "model*", OfficialPrice{})
	require.Error(t, err)
	max := 10
	_, err = svc.Save(context.Background(), "openai", "model", OfficialPrice{Intervals: []PricingInterval{{MinTokens: 0, MaxTokens: &max}, {MinTokens: 5}}})
	require.Error(t, err)
}

func TestOfficialPricingTierDocumentReplacesWithoutMutatingAutomatic(t *testing.T) {
	oldPrice, newPrice := 2e-6, 9e-6
	max := 100
	base := &OfficialPrice{InputPrice: &oldPrice, Intervals: []PricingInterval{{MinTokens: 0, MaxTokens: &max, InputPrice: &oldPrice}, {MinTokens: 100, InputPrice: &newPrice}}}
	empty := mergeOfficialPrice(base, &OfficialPrice{Intervals: []PricingInterval{}})
	require.NotNil(t, empty.Intervals)
	require.Empty(t, empty.Intervals)
	require.Len(t, base.Intervals, 2)
	replaced := mergeOfficialPrice(base, &OfficialPrice{Intervals: []PricingInterval{{MinTokens: 0, InputPrice: &newPrice}}})
	require.Len(t, replaced.Intervals, 1)
	require.Equal(t, newPrice, *replaced.Intervals[0].InputPrice)
	inherited := mergeOfficialPrice(base, &OfficialPrice{InputPrice: &newPrice})
	require.Len(t, inherited.Intervals, 2)
	require.Equal(t, newPrice, *inherited.InputPrice)
	require.Equal(t, newPrice, *inherited.Intervals[0].InputPrice)
	require.InDelta(t, newPrice*newPrice/oldPrice, *inherited.Intervals[1].InputPrice, 1e-15)
	require.Equal(t, oldPrice, *base.Intervals[0].InputPrice)
	require.Equal(t, oldPrice, *base.InputPrice)
}
