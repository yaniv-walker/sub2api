package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// OfficialPrice is a reference-price document, never an input to gateway billing.
// Token prices use USD/token; PerRequestPrice uses USD/image or USD/request.
// Nil fields inherit the automatic catalog; a non-nil empty Intervals replaces
// the entire automatic tier table with no tiers. Explicit zero is preserved.
type OfficialPrice struct {
	BillingMode       BillingMode       `json:"billing_mode,omitempty"`
	InputPrice        *float64          `json:"input_price"`
	OutputPrice       *float64          `json:"output_price"`
	CacheWritePrice   *float64          `json:"cache_write_price"`
	CacheWrite1hPrice *float64          `json:"cache_write_1h_price"`
	CacheReadPrice    *float64          `json:"cache_read_price"`
	ImageInputPrice   *float64          `json:"image_input_price"`
	ImageOutputPrice  *float64          `json:"image_output_price"`
	PerRequestPrice   *float64          `json:"per_request_price"`
	Intervals         []PricingInterval `json:"intervals"`
}

type OfficialPricingEntry struct {
	Platform  string         `json:"platform"`
	Model     string         `json:"model"`
	Source    string         `json:"source"`
	Override  *OfficialPrice `json:"override"`
	Automatic *OfficialPrice `json:"automatic"`
	Pricing   *OfficialPrice `json:"pricing"`
}

type OfficialPricingIdentity struct {
	Platform string
	Model    string
}

// GetMany loads manual rows in a single query. Keys in the result preserve the
// caller's identities, while persisted lookup keys normalize model spelling.
func (s *OfficialPricingService) GetMany(ctx context.Context, identities []OfficialPricingIdentity) (map[OfficialPricingIdentity]*OfficialPricingEntry, error) {
	keys := make([]string, 0, len(identities))
	normalized := make(map[OfficialPricingIdentity]OfficialPricingIdentity, len(identities))
	seen := map[string]bool{}
	for _, id := range identities {
		platform, model, err := officialPricingIdentity(id.Platform, id.Model)
		if err != nil {
			return nil, err
		}
		normalized[id] = OfficialPricingIdentity{Platform: platform, Model: model}
		key := officialPriceSettingKey(platform, model)
		if !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	rawByKey := map[string]string{}
	if s.repo != nil && len(keys) > 0 {
		var err error
		rawByKey, err = s.repo.GetMultiple(ctx, keys)
		if err != nil {
			return nil, fmt.Errorf("read official pricing: %w", err)
		}
	}
	out := make(map[OfficialPricingIdentity]*OfficialPricingEntry, len(identities))
	for id, norm := range normalized {
		entry, err := s.entry(ctx, norm.Platform, norm.Model, rawByKey[officialPriceSettingKey(norm.Platform, norm.Model)])
		if err != nil {
			return nil, err
		}
		out[id] = entry
	}
	return out, nil
}

type officialPriceDocument struct {
	Platform string        `json:"platform"`
	Model    string        `json:"model"`
	Pricing  OfficialPrice `json:"pricing"`
}

const officialPriceSettingPrefix = "model_official_price:"

// OfficialPricingService keeps manual references separate from PricingService,
// so catalog refreshes and billing cannot silently consume operator reference edits.
// Each model owns one settings row: saving one never rewrites another model.
// Reads are uncached, making admin writes visible across server instances.
type OfficialPricingService struct {
	repo     SettingRepository
	pricing  *PricingService
	billing  *BillingService
	resolver *ModelPricingResolver
}

func NewOfficialPricingService(repo SettingRepository, pricing *PricingService, billing *BillingService, resolver *ModelPricingResolver) *OfficialPricingService {
	return &OfficialPricingService{repo: repo, pricing: pricing, billing: billing, resolver: resolver}
}

func officialPricingIdentity(platform, model string) (string, string, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	model = strings.ToLower(strings.TrimSpace(model))
	if !domain.IsConcretePlatform(platform) || model == "" || len(model) > 512 || strings.Contains(model, "*") || strings.IndexFunc(model, unicode.IsControl) >= 0 {
		return "", "", infraerrors.BadRequest("INVALID_OFFICIAL_PRICE_IDENTITY", "A supported routing platform and exact model ID are required")
	}
	return platform, model, nil
}

func officialPriceSettingKey(platform, model string) string {
	sum := sha256.Sum256([]byte(platform + "\x00" + model))
	return officialPriceSettingPrefix + hex.EncodeToString(sum[:])
}

func (s *OfficialPricingService) Get(ctx context.Context, platform, model string) (*OfficialPricingEntry, error) {
	platform, model, err := officialPricingIdentity(platform, model)
	if err != nil {
		return nil, err
	}
	raw := ""
	if s.repo != nil {
		var readErr error
		raw, readErr = s.repo.GetValue(ctx, officialPriceSettingKey(platform, model))
		if readErr != nil && !errors.Is(readErr, ErrSettingNotFound) {
			return nil, fmt.Errorf("read official pricing: %w", readErr)
		}
	}
	return s.entry(ctx, platform, model, raw)
}

func (s *OfficialPricingService) entry(ctx context.Context, platform, model, raw string) (*OfficialPricingEntry, error) {
	entry := &OfficialPricingEntry{Platform: platform, Model: model, Source: "missing", Automatic: s.automatic(ctx, model)}
	if raw != "" {
		var doc officialPriceDocument
		if err := json.Unmarshal([]byte(raw), &doc); err != nil {
			return nil, fmt.Errorf("decode official pricing: %w", err)
		}
		if doc.Platform != platform || doc.Model != model {
			return nil, fmt.Errorf("official pricing identity mismatch")
		}
		entry.Override = &doc.Pricing
	}
	entry.Pricing = mergeOfficialPrice(entry.Automatic, entry.Override)
	if entry.Override != nil {
		entry.Source = "manual"
	} else if entry.Automatic != nil {
		entry.Source = "automatic"
	}
	return entry, nil
}

func (s *OfficialPricingService) Resolve(ctx context.Context, platform, model string) (*OfficialPrice, error) {
	entry, err := s.Get(ctx, platform, model)
	if err != nil {
		return nil, err
	}
	return entry.Pricing, nil
}

func (s *OfficialPricingService) Save(ctx context.Context, platform, model string, p OfficialPrice) (*OfficialPricingEntry, error) {
	platform, model, err := officialPricingIdentity(platform, model)
	if err != nil {
		return nil, err
	}
	if err := validateOfficialPrice(p); err != nil {
		return nil, infraerrors.BadRequest("INVALID_OFFICIAL_PRICE", err.Error())
	}
	if s.repo == nil {
		return nil, fmt.Errorf("official pricing repository is unavailable")
	}
	raw, err := json.Marshal(officialPriceDocument{Platform: platform, Model: model, Pricing: p})
	if err != nil {
		return nil, err
	}
	if err := s.repo.Set(ctx, officialPriceSettingKey(platform, model), string(raw)); err != nil {
		return nil, err
	}
	return s.Get(ctx, platform, model)
}

func (s *OfficialPricingService) Delete(ctx context.Context, platform, model string) error {
	platform, model, err := officialPricingIdentity(platform, model)
	if err != nil {
		return err
	}
	if s.repo == nil {
		return fmt.Errorf("official pricing repository is unavailable")
	}
	return s.repo.Delete(ctx, officialPriceSettingKey(platform, model))
}

func (s *OfficialPricingService) List(ctx context.Context, platform, search string) ([]*OfficialPricingEntry, error) {
	items, _, err := s.ListPage(ctx, platform, search, 200)
	return items, err
}

// ListPage bounds automatic tier resolution as large catalogs can contain
// thousands of models. Search is applied before the limit, including manuals.
func (s *OfficialPricingService) ListPage(ctx context.Context, platform, search string, limit int) ([]*OfficialPricingEntry, int, error) {
	platform, _, err := officialPricingIdentity(platform, "listing")
	if err != nil {
		return nil, 0, err
	}
	if limit < 1 || limit > 200 {
		limit = 200
	}
	models := map[string]struct{}{}
	rawByKey := map[string]string{}
	if s.pricing != nil {
		if spec, ok := domain.LookupPlatform(platform); ok && spec.LiteLLMProvider != "" {
			for _, model := range s.pricing.ListModelNamesByProvider(spec.LiteLLMProvider) {
				models[strings.ToLower(model)] = struct{}{}
			}
		}
	}
	if s.repo != nil {
		settings, err := s.repo.GetAll(ctx)
		if err != nil {
			return nil, 0, err
		}
		for key, raw := range settings {
			if !strings.HasPrefix(key, officialPriceSettingPrefix) {
				continue
			}
			var doc officialPriceDocument
			if err := json.Unmarshal([]byte(raw), &doc); err != nil {
				return nil, 0, fmt.Errorf("decode official pricing document: %w", err)
			}
			if doc.Platform == platform {
				models[doc.Model] = struct{}{}
				rawByKey[key] = raw
			}
		}
	}
	names := make([]string, 0, len(models))
	search = strings.ToLower(strings.TrimSpace(search))
	for model := range models {
		if strings.Contains(model, search) {
			names = append(names, model)
		}
	}
	sort.Strings(names)
	total := len(names)
	if len(names) > limit {
		names = names[:limit]
	}
	out := make([]*OfficialPricingEntry, 0, len(names))
	for _, model := range names {
		entry, err := s.entry(ctx, platform, model, rawByKey[officialPriceSettingKey(platform, model)])
		if err != nil {
			return nil, 0, err
		}
		out = append(out, entry)
	}
	return out, total, nil
}

func mergeOfficialPrice(base, override *OfficialPrice) *OfficialPrice {
	if base == nil && override == nil {
		return nil
	}
	out := OfficialPrice{}
	if base != nil {
		out = *base
	}
	if override == nil {
		return &out
	}
	if override.BillingMode != "" {
		out.BillingMode = override.BillingMode
	}
	dst := []**float64{&out.InputPrice, &out.OutputPrice, &out.CacheWritePrice, &out.CacheWrite1hPrice, &out.CacheReadPrice, &out.ImageInputPrice, &out.ImageOutputPrice, &out.PerRequestPrice}
	src := []*float64{override.InputPrice, override.OutputPrice, override.CacheWritePrice, override.CacheWrite1hPrice, override.CacheReadPrice, override.ImageInputPrice, override.ImageOutputPrice, override.PerRequestPrice}
	for i, p := range src {
		if p != nil {
			*dst[i] = p
		}
	}
	if override.Intervals != nil {
		out.Intervals = override.Intervals
	} else if base != nil && len(base.Intervals) > 0 {
		// Base edits and their inherited long-context ratios must agree in
		// reference tables as well as in the plaza's multiplied quote.
		out.Intervals = append([]PricingInterval(nil), base.Intervals...)
		for i := range out.Intervals {
			tier := &out.Intervals[i]
			values := []struct {
				before, after *float64
				target        **float64
			}{
				{base.InputPrice, override.InputPrice, &tier.InputPrice},
				{base.OutputPrice, override.OutputPrice, &tier.OutputPrice},
				{base.CacheWritePrice, override.CacheWritePrice, &tier.CacheWritePrice},
				{base.CacheWrite1hPrice, override.CacheWrite1hPrice, &tier.CacheWrite1hPrice},
				{base.CacheReadPrice, override.CacheReadPrice, &tier.CacheReadPrice},
				{base.PerRequestPrice, override.PerRequestPrice, &tier.PerRequestPrice},
			}
			for _, field := range values {
				if field.after == nil {
					continue
				}
				value := *field.after
				if field.before != nil && *field.before > 0 && *field.target != nil {
					value *= **field.target / *field.before
				}
				*field.target = &value
			}
		}
	}
	return &out
}

func (s *OfficialPricingService) automatic(ctx context.Context, model string) *OfficialPrice {
	var out *OfficialPrice
	if s.billing != nil {
		if p, err := s.billing.GetModelPricing(model); err == nil && p != nil {
			out = &OfficialPrice{BillingMode: BillingModeToken, InputPrice: nonZeroPtr(p.InputPricePerToken), OutputPrice: nonZeroPtr(p.OutputPricePerToken), CacheWritePrice: nonZeroPtr(p.CacheCreationPricePerToken), CacheReadPrice: nonZeroPtr(p.CacheReadPricePerToken), ImageInputPrice: nonZeroPtr(p.ImageInputPricePerToken), ImageOutputPrice: nonZeroPtr(p.ImageOutputPricePerToken)}
			if p.SupportsCacheBreakdown {
				out.CacheWrite1hPrice = nonZeroPtr(p.CacheCreation1hPrice)
			}
			if s.resolver != nil {
				if sched, err := s.billing.ResolveContextPricingSchedule(ctx, s.resolver, ContextPricingScheduleInput{Model: model}); err == nil && sched != nil && len(sched.Tiers) > 1 {
					out.Intervals = plazaIntervalsFromTiers(sched.Tiers)
				}
			}
		}
	}
	if s.pricing != nil {
		if p := s.pricing.GetModelPricing(model); p != nil {
			if out == nil {
				out = &OfficialPrice{BillingMode: BillingModeToken}
			}
			out.PerRequestPrice = nonZeroPtr(p.OutputCostPerImage)
			if p.Mode == "image_generation" {
				out.BillingMode = BillingModeImage
			}
			if out.ImageInputPrice == nil {
				out.ImageInputPrice = nonZeroPtr(p.InputCostPerImageToken)
			}
			if out.ImageOutputPrice == nil {
				out.ImageOutputPrice = nonZeroPtr(p.OutputCostPerImageToken)
			}
		}
	}
	return out
}

func validateOfficialPrice(p OfficialPrice) error {
	switch p.BillingMode {
	case "", BillingModeToken, BillingModeImage, BillingModePerRequest, BillingModeVideo:
	default:
		return fmt.Errorf("unsupported billing mode")
	}
	validate := func(values ...*float64) error {
		for _, v := range values {
			if v != nil && (*v < 0 || math.IsNaN(*v) || math.IsInf(*v, 0)) {
				return fmt.Errorf("prices must be finite non-negative numbers")
			}
		}
		return nil
	}
	if err := validate(p.InputPrice, p.OutputPrice, p.CacheWritePrice, p.CacheWrite1hPrice, p.CacheReadPrice, p.ImageInputPrice, p.ImageOutputPrice, p.PerRequestPrice); err != nil {
		return err
	}
	if len(p.Intervals) > 100 {
		return fmt.Errorf("at most 100 pricing intervals are supported")
	}
	for i, tier := range p.Intervals {
		if err := validate(tier.InputPrice, tier.OutputPrice, tier.CacheWritePrice, tier.CacheWrite1hPrice, tier.CacheReadPrice, tier.PerRequestPrice); err != nil {
			return err
		}
		if tier.InputMultiplier != nil || tier.OutputMultiplier != nil || tier.CacheWriteMultiplier != nil || tier.CacheReadMultiplier != nil {
			return fmt.Errorf("official tiers require absolute prices")
		}
		if tier.MinTokens < 0 || (tier.MaxTokens != nil && *tier.MaxTokens <= tier.MinTokens) {
			return fmt.Errorf("invalid pricing interval bounds")
		}
		if len(tier.TierLabel) > 120 {
			return fmt.Errorf("pricing interval label is too long")
		}
		if (p.BillingMode == "" || p.BillingMode == BillingModeToken) && i > 0 {
			prev := p.Intervals[i-1]
			if prev.MaxTokens == nil || tier.MinTokens < *prev.MaxTokens {
				return fmt.Errorf("token intervals must be ordered and non-overlapping")
			}
		}
	}
	return nil
}
