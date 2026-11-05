//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type plazaAutomaticAccounts struct {
	AccountRepository
	accounts []Account
}

func (r *plazaAutomaticAccounts) ListActive(context.Context) ([]Account, error) {
	return r.accounts, nil
}

func TestModelPlazaAutomaticModelsUseAccountBindingsNotPriceCatalog(t *testing.T) {
	ctx := context.Background()
	account := catalogAccount("new-model")
	account.GroupIDs = []int64{10}
	repo := &plazaAutomaticAccounts{accounts: []Account{account}}
	pricing := &PricingService{pricingData: map[string]*LiteLLMModelPricing{"unavailable-price-only": {InputCostPerToken: 1e-6, LiteLLMProvider: "openai"}}}
	groups := []Group{{ID: 10, Platform: PlatformOpenAI, RateMultiplier: .25}}
	svc := newPlazaService(nil, groups, pricing)
	svc.accountRepo = repo
	result, err := svc.ListGroups(ctx)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "new-model", result[0].Models[0].Name)
	require.Len(t, result[0].Models, 1, "price catalog must not create fake support")
	repo.accounts[0].GroupIDs = []int64{99}
	result, err = svc.ListGroups(ctx)
	require.NoError(t, err)
	require.Empty(t, result, "moving an account removes the old group association")
}

func TestModelPlazaMaintainedPriceUpdatesQuoteWithoutChangingBilling(t *testing.T) {
	ctx := context.Background()
	repo := &officialPricingTestRepo{values: map[string]string{}}
	pricing := &PricingService{pricingData: map[string]*LiteLLMModelPricing{"test-model": {InputCostPerToken: 2e-6, OutputCostPerToken: 8e-6, LiteLLMProvider: "openai"}}}
	account := catalogAccount("test-model")
	groups := []Group{{ID: 10, Platform: PlatformOpenAI, RateMultiplier: .25}}
	svc := newPlazaServiceWithBilling(nil, groups, nil, pricing)
	svc.accountRepo = &plazaAutomaticAccounts{accounts: []Account{account}}
	svc.officialPricing = NewOfficialPricingService(repo, pricing, svc.billingService, svc.resolver)
	_, err := svc.officialPricing.Save(ctx, PlatformOpenAI, "test-model", OfficialPrice{InputPrice: testPtrFloat64(6e-6)})
	require.NoError(t, err)
	result, err := svc.ListGroups(ctx)
	require.NoError(t, err)
	require.Len(t, result, 1)
	model := result[0].Models[0]
	require.Equal(t, 6e-6, *model.Pricing.InputPrice)
	require.Equal(t, 6e-6, *model.OfficialPricing.InputPrice)
	require.Equal(t, .25, result[0].RateMultiplier, "the old multiplier is not pre-applied")
	actual, err := svc.billingService.GetModelPricing("test-model")
	require.NoError(t, err)
	require.Equal(t, 2e-6, actual.InputPricePerToken)
}
