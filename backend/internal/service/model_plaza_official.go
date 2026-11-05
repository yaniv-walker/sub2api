package service

// applyPlazaOfficialOverride changes only the plaza's reference-based quote.
// Explicit channel/group fields win, and callers still apply the existing rates.
// Cached channel cards and the billing resolver are never modified.
func applyPlazaOfficialOverride(model *PlazaModel, group *Group, manual *OfficialPrice, configured *ChannelModelPricing) {
	if manual == nil {
		return
	}
	price := ChannelModelPricing{BillingMode: BillingModeToken}
	if model.Pricing != nil {
		price = model.Pricing.Clone()
	}
	explicit := ChannelModelPricing{}
	if configured != nil {
		explicit = configured.Clone()
	}
	if manual.BillingMode != "" && explicit.BillingMode == "" {
		price.BillingMode = manual.BillingMode
	}
	// A complete manual ladder replaces the automatic ladder, never explicit tiers.
	if manual.Intervals != nil && len(explicit.Intervals) == 0 {
		price.Intervals = append([]PricingInterval{}, manual.Intervals...)
	}
	fields := []struct {
		value      *float64
		configured *float64
		target     **float64
		interval   func(*PricingInterval) **float64
	}{
		{manual.InputPrice, explicit.InputPrice, &price.InputPrice, func(p *PricingInterval) **float64 { return &p.InputPrice }},
		{manual.OutputPrice, explicit.OutputPrice, &price.OutputPrice, func(p *PricingInterval) **float64 { return &p.OutputPrice }},
		{manual.CacheWritePrice, explicit.CacheWritePrice, &price.CacheWritePrice, func(p *PricingInterval) **float64 { return &p.CacheWritePrice }},
		{manual.CacheWrite1hPrice, explicit.CacheWrite1hPrice, &price.CacheWrite1hPrice, func(p *PricingInterval) **float64 { return &p.CacheWrite1hPrice }},
		{manual.CacheReadPrice, explicit.CacheReadPrice, &price.CacheReadPrice, func(p *PricingInterval) **float64 { return &p.CacheReadPrice }},
		{manual.PerRequestPrice, explicit.PerRequestPrice, &price.PerRequestPrice, func(p *PricingInterval) **float64 { return &p.PerRequestPrice }},
	}
	for _, field := range fields {
		if field.value == nil || field.configured != nil {
			continue
		}
		old := *field.target
		*field.target = field.value
		if manual.Intervals != nil {
			continue
		}
		for i := range price.Intervals {
			target := field.interval(&price.Intervals[i])
			if i < len(explicit.Intervals) && *field.interval(&explicit.Intervals[i]) != nil {
				continue
			}
			// Preserve automatic long-context ratios when only the base is edited.
			if old != nil && *old > 0 && *target != nil {
				value := **target / *old * *field.value
				*target = &value
			} else {
				*target = field.value
			}
		}
	}
	if manual.ImageInputPrice != nil && explicit.ImageInputPrice == nil {
		price.ImageInputPrice = manual.ImageInputPrice
	}
	if manual.ImageOutputPrice != nil && explicit.ImageOutputPrice == nil {
		price.ImageOutputPrice = manual.ImageOutputPrice
	}
	if manual.Intervals != nil && len(explicit.Intervals) == 0 {
		for _, field := range fields {
			for i := range price.Intervals {
				target := field.interval(&price.Intervals[i])
				if field.configured != nil {
					*target = field.configured
				} else if *target == nil {
					*target = *field.target
				}
			}
			if len(price.Intervals) > 0 && field.configured == nil && field.value == nil {
				*field.target = *field.interval(&price.Intervals[0])
			}
		}
		model.LongContextBasis = ""
		if price.BillingMode == "" || price.BillingMode == BillingModeToken {
			if group != nil && group.LongContextPricingEnabled && len(price.Intervals) > 1 {
				model.LongContextBasis = ContextPricingBasisWholeRequest
			} else {
				price.Intervals = nil
			}
		}
	}
	model.Pricing = plazaImageDisplayPricing(&price, group)
}

func plazaOfficialPriceDTO(price *OfficialPrice) *PlazaOfficialPricing {
	if price == nil {
		return nil
	}
	return &PlazaOfficialPricing{
		InputPrice: price.InputPrice, OutputPrice: price.OutputPrice,
		CacheWritePrice: price.CacheWritePrice, CacheWrite1hPrice: price.CacheWrite1hPrice,
		CacheReadPrice: price.CacheReadPrice, Intervals: price.Intervals,
	}
}
