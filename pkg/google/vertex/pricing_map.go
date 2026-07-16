package vertex

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync/atomic"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
)

const (
	vertexAIServiceName = "Vertex AI"
	// discoveryEngineServiceName is the GCP Billing API service name for Vertex AI Search,
	// which hosts the Ranking API used for reranking.
	discoveryEngineServiceName = "Vertex AI Search"

	// modalityGroup matches only the text modality in Vertex AI SKU descriptions. Audio,
	// image, and video inputs are multimodal inputs priced separately from text tokens and
	// are not represented by these metrics. Matching all modalities would cause each
	// modality's price to overwrite the previous one, leaving the emitted value
	// non-deterministic.
	modalityGroup = `(?:text)`
)

// rerankModel is the gen_ai_request_model label for the reranker. GCP catalogs the price as a
// service SKU ("Vertex AI Search: Ranking") with no model name, so this recognizable slug (the
// Semantic Ranker the Assistant uses) stands in; familyFromModelID maps the "semantic" prefix to
// google.
const rerankModel = "semantic-ranker"

var (
	// rerankRegex matches the Vertex AI Search Ranking (Semantic Ranker) SKU. Verified against the
	// live catalog: the SKU description is exactly "Vertex AI Search: Ranking" (GenAppBuilder), not
	// the "... Ranking Requests" form assumed previously.
	rerankRegex = regexp.MustCompile(`(?i)^Vertex AI Search: Ranking$`)

	// agentBuilderPrefix marks Agent Builder ("AI Dev Tools: ...") token SKUs. They price a
	// different product and share the Discovery Engine service, so they are skipped to keep them out
	// of the token metric.
	agentBuilderPrefix = "AI Dev Tools:"
)

// skuPattern maps a compiled regex to the billing direction, billing type, and price tier.
type skuPattern struct {
	re          *regexp.Regexp
	direction   string // "input" or "output"
	billingType string // "token" or "char"
	tier        string
}

func mustCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(strings.ReplaceAll(pattern, "{mod}", modalityGroup))
}

// skuPatterns is the ordered lookup table for Vertex AI token/character SKU descriptions.
// More specific patterns must appear before generic ones to prevent the lazy (.+?) from
// capturing too much.
var skuPatterns = []skuPattern{
	// Gemini output — "Thinking" prefix style (most specific first)
	{mustCompile(`(?i)^(.+?)\s+Thinking\s+` + modalityGroup + `\s+Output\s+Priority\s+\(Long\)\s+-\s+Predictions$`), "output", "token", "thinking_priority_long_context"},
	{mustCompile(`(?i)^(.+?)\s+Thinking\s+` + modalityGroup + `\s+Output\s+Priority\s+-\s+Predictions$`), "output", "token", "thinking_priority"},
	{mustCompile(`(?i)^(.+?)\s+Thinking\s+` + modalityGroup + `\s+Output\s+Flex\s+\(Long\)\s+-\s+Predictions$`), "output", "token", "thinking_flex_long_context"},
	{mustCompile(`(?i)^(.+?)\s+Thinking\s+` + modalityGroup + `\s+Output\s+Flex\s+-\s+Predictions$`), "output", "token", "thinking_flex"},
	{mustCompile(`(?i)^(.+?)\s+Thinking\s+` + modalityGroup + `\s+Output\s+\(Long\)\s+-\s+Batch\s+Predictions$`), "output", "token", "thinking_batch_long_context"},
	{mustCompile(`(?i)^(.+?)\s+Thinking\s+` + modalityGroup + `\s+Output\s+-\s+Batch\s+Predictions$`), "output", "token", "thinking_batch"},
	{mustCompile(`(?i)^(.+?)\s+Thinking\s+` + modalityGroup + `\s+Output\s+\(Long\)\s+-\s+Predictions$`), "output", "token", "thinking_long_context"},
	{mustCompile(`(?i)^(.+?)\s+Thinking\s+` + modalityGroup + `\s+Output\s+-\s+Predictions$`), "output", "token", "thinking"},

	// Gemini output — "(Thinking On...)" parenthetical style
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+Priority\s+\(Thinking\s+On\s+and\s+Long\)\s+-\s+Predictions$`), "output", "token", "thinking_priority_long_context"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+Priority\s+\(Thinking\s+On\)\s+-\s+Predictions$`), "output", "token", "thinking_priority"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+\(Thinking\s+On\s+and\s+Long\)\s+-\s+Batch\s+Predictions$`), "output", "token", "thinking_batch_long_context"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+\(Thinking\s+On\s+and\s+Long\)\s+-\s+Predictions$`), "output", "token", "thinking_long_context"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+\(Thinking\s+On\)\s+-\s+Batch\s+Predictions$`), "output", "token", "thinking_batch"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+\(Thinking\s+On\)\s+-\s+Predictions$`), "output", "token", "thinking"},

	// Gemini output — Live (before generic)
	{mustCompile(`(?i)^(.+?)\s+Live\s+` + modalityGroup + `\s+Output\s+-\s+Predictions$`), "output", "token", "live"},

	// Gemini output — Priority / Flex (before generic)
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+Priority\s+\(Long\)\s+-\s+Predictions$`), "output", "token", "priority_long_context"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+Priority\s+-\s+Predictions$`), "output", "token", "priority"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+Flex\s+\(Long\)\s+-\s+Predictions$`), "output", "token", "flex_long_context"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+Flex\s+-\s+Predictions$`), "output", "token", "flex"},

	// Gemini output — standard
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+\(Long\)\s+-\s+Batch\s+Predictions$`), "output", "token", "batch_long_context"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+Long\s+Context\s+-\s+Batch\s+Predictions$`), "output", "token", "batch_long_context"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+-\s+Batch\s+Predictions$`), "output", "token", "batch"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+\(Long\)\s+-\s+Predictions$`), "output", "token", "long_context"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Output\s+-\s+Predictions$`), "output", "token", "on_demand"},

	// Gemini input — Live (before generic)
	{mustCompile(`(?i)^(.+?)\s+Live\s+` + modalityGroup + `\s+Input\s+-\s+Predictions$`), "input", "token", "live"},

	// Gemini input — 1.5 cached style (before generic)
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Cached\s+Input\s+\(Long\)\s+-\s+Predictions$`), "input", "token", "cached_long_context"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Cached\s+Input\s+-\s+Predictions$`), "input", "token", "cached"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Input\s+Cache\s+Storage\s+-\s+Predictions$`), "input", "token", "cache_storage"},

	// Gemini input — Priority (before generic)
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Input\s+Priority\s+\(Long\)\s+-\s+Predictions$`), "input", "token", "priority_long_context"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Input\s+Priority\s+-\s+Predictions$`), "input", "token", "priority"},

	// Gemini input — standard
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Input\s+\(Long\)\s+-\s+Batch\s+Predictions$`), "input", "token", "batch_long_context"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Input\s+Long\s+Context\s+-\s+Batch\s+Predictions$`), "input", "token", "batch_long_context"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Input\s+-\s+Batch\s+Predictions$`), "input", "token", "batch"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Input\s+\(Long\)\s+-\s+Predictions$`), "input", "token", "long_context"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Input\s+-\s+Predictions$`), "input", "token", "on_demand"},

	// Gemini caching 2.0+ style ("Input" before modality) — more specific before less specific
	{mustCompile(`(?i)^(.+?)\s+Input\s+` + modalityGroup + `\s+Caching\s+Storage$`), "input", "token", "cache_storage"},
	{mustCompile(`(?i)^(.+?)\s+Input\s+` + modalityGroup + `\s+Caching\s+Batch\s+\(Long\)$`), "input", "token", "cached_batch_long_context"},
	{mustCompile(`(?i)^(.+?)\s+Input\s+` + modalityGroup + `\s+Caching\s+Batch$`), "input", "token", "cached_batch"},
	{mustCompile(`(?i)^(.+?)\s+Input\s+` + modalityGroup + `\s+Caching\s+Flex\s+\(Long\)$`), "input", "token", "cached_flex_long_context"},
	{mustCompile(`(?i)^(.+?)\s+Input\s+` + modalityGroup + `\s+Caching\s+Flex$`), "input", "token", "cached_flex"},
	{mustCompile(`(?i)^(.+?)\s+Input\s+` + modalityGroup + `\s+Caching\s+Priority\s+\(Long\)$`), "input", "token", "cached_priority_long_context"},
	{mustCompile(`(?i)^(.+?)\s+Input\s+` + modalityGroup + `\s+Caching\s+Priority$`), "input", "token", "cached_priority"},
	{mustCompile(`(?i)^(.+?)\s+Input\s+` + modalityGroup + `\s+Caching\s+\(Long\)$`), "input", "token", "cached_long_context"},
	{mustCompile(`(?i)^(.+?)\s+Input\s+` + modalityGroup + `\s+Caching$`), "input", "token", "cached"},

	// Gemini caching alternate style ("modality" before "Input Caching")
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Input\s+Caching\s+Storage$`), "input", "token", "cache_storage"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Input\s+Caching\s+Priority\s+\(Long\)$`), "input", "token", "cached_priority_long_context"},
	{mustCompile(`(?i)^(.+?)\s+` + modalityGroup + `\s+Input\s+Caching\s+Priority$`), "input", "token", "cached_priority"},

	// MaaS token format — specific before generic
	{regexp.MustCompile(`(?i)^(.+?)\s+Batch\s+Input\s+Tokens?$`), "input", "token", "batch"},
	{regexp.MustCompile(`(?i)^(.+?)\s+Batch\s+Output\s+Tokens?$`), "output", "token", "batch"},
	{regexp.MustCompile(`(?i)^(.+?)\s+Cached(?:\s+Text)?\s+Input\s+Tokens?$`), "input", "token", "cached"},
	{regexp.MustCompile(`(?i)^(.+?)\s+Input\s+Tokens?$`), "input", "token", "on_demand"},
	{regexp.MustCompile(`(?i)^(.+?)\s+Output\s+Tokens?$`), "output", "token", "on_demand"},

	// Character-billed models
	{regexp.MustCompile(`(?i)^(.+?)\s+Input\s+Characters?$`), "input", "char", "on_demand"},
	{regexp.MustCompile(`(?i)^(.+?)\s+Output\s+Characters?$`), "output", "char", "on_demand"},
}

// Snapshot is an immutable view of the Vertex AI pricing data.
type Snapshot struct {
	// tokenInput[region][model][tier] = price per 1k input tokens (only set if a SKU exists)
	tokenInput map[string]map[string]map[string]float64
	// tokenOutput[region][model][tier] = price per 1k output tokens (only set if a SKU exists)
	tokenOutput map[string]map[string]map[string]float64
	// tokenCacheRead[region][model][tier] = price per 1k prompt-cache read tokens (Claude-on-Vertex)
	tokenCacheRead map[string]map[string]map[string]float64
	// tokenCacheWrite[region][model][tier] = price per 1k prompt-cache write tokens (Claude-on-Vertex)
	tokenCacheWrite map[string]map[string]map[string]float64
	// charInput[region][model][tier] = price per 1k input characters (only set if a SKU exists)
	charInput map[string]map[string]map[string]float64
	// charOutput[region][model][tier] = price per 1k output characters (only set if a SKU exists)
	charOutput map[string]map[string]map[string]float64
	// reranking[region][model] = price per 1k ranking requests (USD)
	reranking map[string]map[string]float64
}

// PricingMap stores Vertex AI pricing and refreshes atomically.
type PricingMap struct {
	gcpClient client.Client
	logger    *slog.Logger
	current   atomic.Pointer[Snapshot]
	// familyFilter, when non-nil, drops any model whose family does not match before it enters the
	// map. A nil filter (e.g. in tests) keeps everything.
	familyFilter *regexp.Regexp
	// billingAccount, when non-empty, enables the account-scoped Claude-on-Vertex price fetch.
	billingAccount string
}

// NewPricingMap initialises and populates a PricingMap.
func NewPricingMap(ctx context.Context, logger *slog.Logger, gcpClient client.Client, familyFilter *regexp.Regexp, billingAccount string) (*PricingMap, error) {
	pm := &PricingMap{gcpClient: gcpClient, logger: logger, familyFilter: familyFilter, billingAccount: billingAccount}
	// Catalog only, to keep construction (and thus startup) fast; account-scoped Claude prices are
	// layered in by the background refresh, off the startup path.
	if err := pm.PopulateCatalog(ctx); err != nil {
		return nil, err
	}
	return pm, nil
}

// familyAllowed reports whether a model's family passes the configured family filter.
func (pm *PricingMap) familyAllowed(model string) bool {
	if pm.familyFilter == nil {
		return true
	}
	return pm.familyFilter.MatchString(familyFromModelID(model))
}

// Snapshot returns an immutable copy of the current pricing data.
func (pm *PricingMap) Snapshot() Snapshot {
	if s := pm.current.Load(); s != nil {
		return *s
	}
	return Snapshot{}
}

// PopulateCatalog fetches the public Cloud Catalog SKUs (Gemini and other Model Garden models,
// translation, reranking) and stores them. It excludes the account-scoped Claude prices, which are
// slow to fetch, so it stays fast enough to run on the startup path.
func (pm *PricingMap) PopulateCatalog(ctx context.Context) error {
	skus, err := pm.fetchCatalogSKUs(ctx)
	if err != nil {
		return err
	}
	pm.current.Store(pm.buildSnapshot(skus))
	return nil
}

// Populate refreshes the full pricing map: the public catalog plus account-scoped Claude prices.
// Used by the background refresh loop, where the slower Claude fetch is off the startup path.
func (pm *PricingMap) Populate(ctx context.Context) error {
	skus, err := pm.fetchCatalogSKUs(ctx)
	if err != nil {
		return err
	}
	snap := pm.buildSnapshot(skus)
	pm.applyClaudePrices(ctx, snap)
	pm.current.Store(snap)
	return nil
}

// fetchCatalogSKUs collects the Vertex AI SKUs plus the Discovery Engine SKUs (reranking). Discovery
// Engine is non-fatal: reranking is omitted if that service is unavailable.
func (pm *PricingMap) fetchCatalogSKUs(ctx context.Context) ([]*billingpb.Sku, error) {
	serviceName, err := pm.gcpClient.GetServiceName(ctx, vertexAIServiceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get Vertex AI service name: %w", err)
	}
	skus := pm.gcpClient.GetPricing(ctx, serviceName)
	if len(skus) == 0 {
		return nil, fmt.Errorf("no SKUs found for Vertex AI service")
	}

	if deSvcName, err := pm.gcpClient.GetServiceName(ctx, discoveryEngineServiceName); err != nil {
		pm.logger.Warn("failed to get Discovery Engine service name, reranking metrics will be unavailable", "error", err)
	} else {
		skus = append(skus, pm.gcpClient.GetPricing(ctx, deSvcName)...)
	}
	return skus, nil
}

// applyClaudePrices layers account-scoped Claude-on-Vertex prices into snap. Best-effort: skipped
// when no billing account is configured, and a fetch failure is logged rather than propagated so
// the catalog prices already in snap are preserved.
func (pm *PricingMap) applyClaudePrices(ctx context.Context, snap *Snapshot) {
	if pm.billingAccount == "" {
		return
	}
	prices, err := pm.gcpClient.ListBillingAccountPrices(ctx, pm.billingAccount, claudeDisplayPrefix)
	if err != nil {
		pm.logger.Warn("failed to fetch Claude-on-Vertex prices, they will be unavailable", "error", err)
		return
	}
	for _, p := range prices {
		pm.applyClaudeAccountPrice(snap, p)
	}
}

// ParseSkus parses the provided SKUs and updates the pricing map atomically.
// Unknown SKUs are logged at debug level and skipped.
func (pm *PricingMap) ParseSkus(skus []*billingpb.Sku) error {
	pm.current.Store(pm.buildSnapshot(skus))
	return nil
}

// buildSnapshot parses SKUs into a Snapshot without publishing it, so callers can layer in
// account-scoped Claude prices before storing atomically.
func (pm *PricingMap) buildSnapshot(skus []*billingpb.Sku) *Snapshot {
	snap := &Snapshot{
		tokenInput:      make(map[string]map[string]map[string]float64),
		tokenOutput:     make(map[string]map[string]map[string]float64),
		tokenCacheRead:  make(map[string]map[string]map[string]float64),
		tokenCacheWrite: make(map[string]map[string]map[string]float64),
		charInput:       make(map[string]map[string]map[string]float64),
		charOutput:      make(map[string]map[string]map[string]float64),
		reranking:       make(map[string]map[string]float64),
	}

	for _, sku := range skus {
		if sku == nil {
			continue
		}
		desc := strings.TrimSpace(sku.GetDescription())
		regions := skuRegions(sku)

		// Agent Builder token SKUs share the Discovery Engine service but price a different product;
		// skip them so they do not leak into the token metric as junk models.
		if strings.HasPrefix(desc, agentBuilderPrefix) {
			continue
		}

		matched := false
		for _, pat := range skuPatterns {
			matches := pat.re.FindStringSubmatch(desc)
			if len(matches) == 0 {
				continue
			}
			model := normalizeModelName(matches[1])
			// Recognize the SKU (so it isn't logged as unknown) but skip storing a filtered family.
			if pm.familyAllowed(model) {
				price := normalizeToPerK(priceFromSku(sku), sku)
				var target map[string]map[string]map[string]float64
				switch {
				case pat.direction == "input" && pat.billingType == "token":
					target = snap.tokenInput
				case pat.direction == "output" && pat.billingType == "token":
					target = snap.tokenOutput
				case pat.direction == "input" && pat.billingType == "char":
					target = snap.charInput
				default:
					target = snap.charOutput
				}
				applyPrice(target, model, pat.tier, price, regions)
			}
			matched = true
			break
		}
		if matched {
			continue
		}

		if rerankRegex.MatchString(desc) {
			if !pm.familyAllowed(rerankModel) {
				continue
			}
			price := normalizeToPerK(priceFromSku(sku), sku)
			for _, region := range regions {
				if region == "" {
					continue
				}
				if snap.reranking[region] == nil {
					snap.reranking[region] = make(map[string]float64)
				}
				snap.reranking[region][rerankModel] = price
			}
			continue
		}

		pm.logger.Debug("skipping unknown Vertex AI SKU", "description", desc)
	}

	return snap
}

// claudeDisplayPrefix matches Claude-on-Vertex account-scoped SKU display names
// (e.g. "Claude Sonnet 4.6 — Input Tokens — global — Context Window Size from 0 to 200000 Tokens").
const claudeDisplayPrefix = "Claude "

// claudeContextBand captures the lower bound of the "Context Window Size from X to Y Tokens" clause.
// A non-zero lower bound marks the long-context (>200k) band.
var claudeContextBand = regexp.MustCompile(`context window size from (\d+) to`)

// applyClaudeAccountPrice parses one account-scoped Claude SKU and stores it. The display name is
// em-dash delimited: "{model} — {token kind} — {region} — Context Window Size from {lo} to {hi}
// Tokens". Model normalizes to Sigil's form (claude-sonnet-4-6); input/output and prompt-cache
// read/write route to their token maps; the context band and cache-write TTL fold into price_tier.
// SKUs missing the expected structure (e.g. legacy Claude 3 formats) are skipped.
func (pm *PricingMap) applyClaudeAccountPrice(snap *Snapshot, p client.BillingAccountPrice) {
	parts := strings.Split(p.Description, "—")
	if len(parts) < 3 {
		return
	}
	model := normalizeClaudeModel(parts[0])
	if !pm.familyAllowed(model) {
		return
	}

	kind := strings.ToLower(parts[1])
	var target map[string]map[string]map[string]float64
	var tokenType string
	switch {
	case strings.Contains(kind, "cache read"):
		tokenType, target = tokenTypeCacheRead, snap.tokenCacheRead
	case strings.Contains(kind, "cache write"):
		tokenType, target = tokenTypeCacheWrite, snap.tokenCacheWrite
	case strings.Contains(kind, "output"):
		tokenType, target = tokenTypeOutput, snap.tokenOutput
	case strings.Contains(kind, "input"):
		tokenType, target = tokenTypeInput, snap.tokenInput
	default:
		return
	}

	region := strings.TrimSpace(parts[2])
	if region == "" {
		region = "global"
	}
	// Prices are per UnitQuantity of usage (Claude token SKUs are per-1M); scale to per-1k.
	perK := 0.0
	if p.UnitQuantity > 0 {
		perK = p.USDPerUnit * 1000 / p.UnitQuantity
	}
	applyPrice(target, model, claudeTier(strings.ToLower(p.Description), tokenType), perK, []string{region})
}

// normalizeClaudeModel converts a Claude SKU model segment into a slug matching Sigil's
// gen_ai_request_model. Dots and spaces both become hyphens, so "Claude Sonnet 4.6" and the
// space-munged "Claude Opus 4 8" both normalize correctly ("claude-sonnet-4-6", "claude-opus-4-8").
func normalizeClaudeModel(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.NewReplacer(".", "-", " ", "-").Replace(s)
	return strings.Trim(s, "-")
}

// claudeTier composes price_tier from the dimensions not held by other labels: the context-window
// band (standard, or long_context above 200k) and, for cache writes, the TTL (5-minute is the base;
// 1-hour is marked with a _1h suffix).
func claudeTier(lowerDesc, tokenType string) string {
	tier := "on_demand"
	if m := claudeContextBand.FindStringSubmatch(lowerDesc); len(m) == 2 && m[1] != "0" {
		tier = "long_context"
	}
	if tokenType == tokenTypeCacheWrite && strings.Contains(lowerDesc, "3600 second") {
		tier += "_1h"
	}
	return tier
}

// applyPrice writes a price into the target region/model/tier map for each region.
// Only regions with a non-empty name are written.
func applyPrice(target map[string]map[string]map[string]float64, model, tier string, price float64, regions []string) {
	for _, region := range regions {
		if region == "" {
			continue
		}
		if target[region] == nil {
			target[region] = make(map[string]map[string]float64)
		}
		if target[region][model] == nil {
			target[region][model] = make(map[string]float64)
		}
		target[region][model][tier] = price
	}
}

// priceFromSku extracts the unit price from the last tiered rate of a SKU.
// Vertex AI SKUs today use flat pricing: the first tier is a $0 free allowance and the
// last tier is the steady-state paid rate. Taking the last rate is correct for this structure.
// If GCP introduces volume discounts on a SKU (descending price at higher tiers), the last
// rate would be the discounted tier rather than the base rate. Re-evaluate this if such SKUs appear.
func priceFromSku(sku *billingpb.Sku) float64 {
	if sku == nil || len(sku.GetPricingInfo()) == 0 {
		return 0
	}
	expression := sku.GetPricingInfo()[0].GetPricingExpression()
	if expression == nil || len(expression.GetTieredRates()) == 0 {
		return 0
	}
	rate := expression.GetTieredRates()[len(expression.GetTieredRates())-1].GetUnitPrice()
	if rate == nil {
		return 0
	}
	return float64(rate.GetUnits()) + float64(rate.GetNanos())/1e9
}

// normalizeToPerK scales the price to per-1k units.
// GCP SKUs with a UsageUnit starting with "k" (e.g. "k{char}")
// are already per-1k units. Otherwise the price is per-unit and is multiplied by 1000.
func normalizeToPerK(price float64, sku *billingpb.Sku) float64 {
	if sku == nil || len(sku.GetPricingInfo()) == 0 {
		return price * 1000
	}
	expression := sku.GetPricingInfo()[0].GetPricingExpression()
	if expression == nil {
		return price * 1000
	}
	usageUnit := strings.ToLower(expression.GetUsageUnit())
	if strings.HasPrefix(usageUnit, "k") {
		return price
	}
	return price * 1000
}

// skuRegions returns the list of regions a SKU applies to.
// Falls back to ["global"] for SKUs with no region information (e.g. Gemini token SKUs).
func skuRegions(sku *billingpb.Sku) []string {
	if sku == nil {
		return nil
	}
	if len(sku.GetServiceRegions()) > 0 {
		return sku.GetServiceRegions()
	}
	if geo := sku.GetGeoTaxonomy(); geo != nil && len(geo.GetRegions()) > 0 {
		return geo.GetRegions()
	}
	return []string{"global"}
}

// modelGardenMaaSPrefix is the billing prefix GCP prepends to some Model Garden
// Model-as-a-Service SKUs. It appears on one token direction (input or output) but
// not the other, causing the same model to normalize to two different IDs.
const modelGardenMaaSPrefix = "Cloud Vertex AI Model Garden Model as a Service "

// normalizeModelName converts a model name from a SKU description to a canonical slug.
// The Model Garden MaaS prefix is stripped first so that input and output SKUs for
// the same model share the same ID.
// Example: "1.5 Flash" → "1.5-flash"
// Example: "Cloud Vertex AI Model Garden Model as a Service Llama 4 Maverick" → "llama-4-maverick"
func normalizeModelName(raw string) string {
	stripped := strings.TrimPrefix(raw, modelGardenMaaSPrefix)
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(stripped), " ", "-"))
}
