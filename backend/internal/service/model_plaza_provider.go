package service

// ProvideModelPlazaService connects automatic support discovery and editable
// reference pricing without changing the gateway's billing dependencies.
func ProvideModelPlazaService(channels ChannelRepository, groups GroupRepository, pricing *PricingService, billing *BillingService, resolver *ModelPricingResolver, official *OfficialPricingService, accounts AccountRepository) *ModelPlazaService {
	svc := NewModelPlazaService(channels, groups, pricing, billing, resolver)
	svc.officialPricing = official
	svc.accountRepo = accounts
	return svc
}
