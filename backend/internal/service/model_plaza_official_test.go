//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelPlazaOfficialOverridePreservesConfiguredFieldsAndZero(t *testing.T) {
	base := &ChannelModelPricing{BillingMode: BillingModeToken, InputPrice: testPtrFloat64(3e-6), OutputPrice: testPtrFloat64(8e-6)}
	manual := &OfficialPrice{InputPrice: testPtrFloat64(0), OutputPrice: testPtrFloat64(12e-6)}
	configured := &ChannelModelPricing{OutputPrice: testPtrFloat64(8e-6)}
	model := PlazaModel{Pricing: base}
	applyPlazaOfficialOverride(&model, &Group{}, manual, configured)
	require.Equal(t, 0.0, *model.Pricing.InputPrice)
	require.Equal(t, 8e-6, *model.Pricing.OutputPrice)
	require.Equal(t, 3e-6, *base.InputPrice, "shared pricing must not be mutated")
}

func TestModelPlazaOfficialOverrideKeepsUnmaintainedFieldsAndTimePricing(t *testing.T) {
	model := PlazaModel{Pricing: &ChannelModelPricing{InputPrice: testPtrFloat64(2e-6), CacheReadPrice: testPtrFloat64(1e-7)}, TimePricing: &TimePricingSchedule{Timezone: "Asia/Shanghai"}}
	applyPlazaOfficialOverride(&model, &Group{}, &OfficialPrice{InputPrice: testPtrFloat64(4e-6)}, nil)
	require.Equal(t, 4e-6, *model.Pricing.InputPrice)
	require.Equal(t, 1e-7, *model.Pricing.CacheReadPrice)
	require.Equal(t, "Asia/Shanghai", model.TimePricing.Timezone)
}

func TestModelPlazaOfficialOverrideLadderAndFlatGroup(t *testing.T) {
	limit := 100000
	manual := &OfficialPrice{InputPrice: testPtrFloat64(2e-6), Intervals: []PricingInterval{{MaxTokens: &limit, InputPrice: testPtrFloat64(2e-6)}, {MinTokens: limit, InputPrice: testPtrFloat64(4e-6)}}}
	model := PlazaModel{}
	applyPlazaOfficialOverride(&model, &Group{LongContextPricingEnabled: true}, manual, nil)
	require.Len(t, model.Pricing.Intervals, 2)
	require.Equal(t, ContextPricingBasisWholeRequest, model.LongContextBasis)
	flat := PlazaModel{}
	applyPlazaOfficialOverride(&flat, &Group{}, manual, nil)
	require.Empty(t, flat.Pricing.Intervals)
	require.Equal(t, 2e-6, *flat.Pricing.InputPrice)
}

func TestModelPlazaOfficialOverrideExplicitEmptyLadder(t *testing.T) {
	model := PlazaModel{Pricing: &ChannelModelPricing{Intervals: []PricingInterval{{InputPrice: testPtrFloat64(1e-6)}}}, LongContextBasis: ContextPricingBasisWholeRequest}
	applyPlazaOfficialOverride(&model, &Group{LongContextPricingEnabled: true}, &OfficialPrice{Intervals: []PricingInterval{}}, nil)
	require.Empty(t, model.Pricing.Intervals)
	require.Empty(t, model.LongContextBasis)
}

func TestModelPlazaOfficialOverrideDoesNotChangeExplicitMediaMode(t *testing.T) {
	model := PlazaModel{Pricing: &ChannelModelPricing{BillingMode: BillingModeImage, PerRequestPrice: testPtrFloat64(.2)}}
	applyPlazaOfficialOverride(&model, &Group{}, &OfficialPrice{BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(.1)}, &ChannelModelPricing{BillingMode: BillingModeImage, PerRequestPrice: testPtrFloat64(.2)})
	require.Equal(t, BillingModeImage, model.Pricing.BillingMode)
	require.Equal(t, .2, *model.Pricing.PerRequestPrice)
}

func TestModelPlazaOfficialOverrideLadderRetainsExplicitOutput(t *testing.T) {
	limit := 100000
	model := PlazaModel{Pricing: &ChannelModelPricing{InputPrice: testPtrFloat64(2e-6), OutputPrice: testPtrFloat64(8e-6)}}
	manual := &OfficialPrice{Intervals: []PricingInterval{{MaxTokens: &limit, InputPrice: testPtrFloat64(3e-6)}, {MinTokens: limit, InputPrice: testPtrFloat64(6e-6)}}}
	applyPlazaOfficialOverride(&model, &Group{LongContextPricingEnabled: true}, manual, &ChannelModelPricing{OutputPrice: testPtrFloat64(8e-6)})
	require.Equal(t, 3e-6, *model.Pricing.InputPrice)
	for _, tier := range model.Pricing.Intervals {
		require.NotNil(t, tier.OutputPrice)
		require.Equal(t, 8e-6, *tier.OutputPrice)
	}
}

func TestModelPlazaOfficialOverridePreservesAutomaticTierRatio(t *testing.T) {
	model := PlazaModel{Pricing: &ChannelModelPricing{InputPrice: testPtrFloat64(2e-6), Intervals: []PricingInterval{{InputPrice: testPtrFloat64(2e-6)}, {InputPrice: testPtrFloat64(4e-6)}}}}
	applyPlazaOfficialOverride(&model, &Group{LongContextPricingEnabled: true}, &OfficialPrice{InputPrice: testPtrFloat64(5e-6)}, nil)
	require.Equal(t, 5e-6, *model.Pricing.Intervals[0].InputPrice)
	require.Equal(t, 10e-6, *model.Pricing.Intervals[1].InputPrice)
}
