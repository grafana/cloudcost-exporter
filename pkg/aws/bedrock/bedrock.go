package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/prometheus/client_golang/prometheus"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/grafana/cloudcost-exporter/pkg/aws/pricingstore"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

const (
	subsystem   = "aws_bedrock"
	serviceName = "bedrock"

	priceTierOnDemand         = "on_demand"
	priceTierOnDemandBatch    = "on_demand_batch"
	priceTierOnDemandFlex     = "on_demand_flex"
	priceTierOnDemandPriority = "on_demand_priority"
	priceTierCrossRegion      = "cross_region"

	// Prompt-caching operations, emitted as price_tier values on the input metric. Reads are a
	// single rate; writes split by cache TTL (5-minute default vs 1-hour).
	priceTierCacheRead    = "cache_read"
	priceTierCacheWrite5m = "cache_write_5m"
	priceTierCacheWrite1h = "cache_write_1h"

	// compositeKeySep separates the four fields encoded in the pricingstore usagetype key:
	// family|direction|model_id|price_tier
	compositeKeySep = "|"

	// searchUnitsPerKilo converts from per-unit pricing (as published by the AWS Pricing API)
	// to per-1000-units pricing (as emitted by SearchUnitCostDesc).
	searchUnitsPerKilo = 1000.0

	// marketplacePerMillionToPerKilo converts marketplace token pricing ($/1M tokens) to $/1K tokens.
	// $/1M ÷ 1000 = $/1K.
	marketplacePerMillionToPerKilo = 1000.0

	// marketplaceSuffix is appended to every model name in the AmazonBedrockFoundationModels API.
	marketplaceSuffix = " (Amazon Bedrock Edition)"
)

var (
	InputTokenCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.InputTokenCostSuffix,
		"The cost of AWS Bedrock input tokens in USD per 1000 tokens",
		[]string{"account_id", "region", "model_id", "family", "price_tier"},
	)
	OutputTokenCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.OutputTokenCostSuffix,
		"The cost of AWS Bedrock output tokens in USD per 1000 tokens",
		[]string{"account_id", "region", "model_id", "family", "price_tier"},
	)
	SearchUnitCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.SearchUnitCostSuffix,
		"The cost of AWS Bedrock search units in USD per 1000 search units (e.g. Cohere Rerank)",
		[]string{"account_id", "region", "model_id", "family", "price_tier"},
	)
)

type bedrockProductInfo struct {
	Product struct {
		Attributes struct {
			UsageType     string `json:"usagetype"`
			RegionCode    string `json:"regionCode"`
			InferenceType string `json:"inferenceType"`
			Provider      string `json:"provider"`
			Model         string `json:"model"`
		} `json:"attributes"`
	} `json:"product"`
	Terms struct {
		OnDemand map[string]struct {
			PriceDimensions map[string]struct {
				PricePerUnit map[string]string `json:"pricePerUnit"`
			} `json:"priceDimensions"`
		} `json:"OnDemand"`
	} `json:"terms"`
}

// bedrockMarketplaceProductInfo represents a SKU from the AmazonBedrockFoundationModels
// service code. Unlike bedrockProductInfo, it has no inferenceType or provider — the model
// and family are encoded in servicename, and the direction is encoded in the usagetype suffix.
type bedrockMarketplaceProductInfo struct {
	Product struct {
		Attributes struct {
			UsageType   string `json:"usagetype"`
			RegionCode  string `json:"regionCode"`
			ServiceName string `json:"servicename"`
		} `json:"attributes"`
	} `json:"product"`
	Terms struct {
		OnDemand map[string]struct {
			PriceDimensions map[string]struct {
				PricePerUnit map[string]string `json:"pricePerUnit"`
			} `json:"priceDimensions"`
		} `json:"OnDemand"`
	} `json:"terms"`
}

type Config struct {
	Regions       []ec2types.Region
	PricingClient client.Client
	AccountID     string
	FamilyFilter  string // regex matched against the family label; see --aws.bedrock.families flag
}

type Collector struct {
	pricingStore pricingstore.PricingStoreRefresher
	regions      []string
	logger       *slog.Logger
	accountID    string
	familyFilter *regexp.Regexp
}

func New(ctx context.Context, config *Config, logger *slog.Logger) (*Collector, error) {
	logger = logger.With("collector", serviceName)

	familyFilter, err := regexp.Compile(config.FamilyFilter)
	if err != nil {
		return nil, fmt.Errorf("invalid bedrock family filter %q: %w", config.FamilyFilter, err)
	}

	pricingStore, err := pricingstore.NewPricingStore(ctx, logger, config.Regions, newPriceFetcher(config.PricingClient, familyFilter, logger))
	if err != nil {
		return nil, fmt.Errorf("failed to create pricing store: %w", err)
	}

	go func(ctx context.Context) {
		priceTicker := time.NewTicker(pricingstore.PriceRefreshInterval)
		defer priceTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-priceTicker.C:
				logger.LogAttrs(ctx, slog.LevelInfo, "refreshing pricing map")
				if err := pricingStore.PopulatePricingMap(ctx); err != nil {
					logger.Error("error refreshing pricing map", "error", err)
				}
			}
		}
	}(ctx)

	regions := make([]string, 0, len(config.Regions))
	for _, r := range config.Regions {
		if r.RegionName != nil {
			regions = append(regions, *r.RegionName)
		}
	}

	return &Collector{
		pricingStore: pricingStore,
		regions:      regions,
		logger:       logger,
		accountID:    config.AccountID,
		familyFilter: familyFilter,
	}, nil
}

func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	snapshot := c.pricingStore.Snapshot()

	for region, regionSnap := range snapshot.Regions() {
		for compositeKey, price := range regionSnap.Entries() {
			parts := strings.SplitN(compositeKey, compositeKeySep, 4)
			if len(parts) != 4 {
				c.logger.LogAttrs(ctx, slog.LevelWarn, "malformed Bedrock pricing key, skipping",
					slog.String("region", region),
					slog.String("key", compositeKey))
				continue
			}
			family, direction, modelID, priceTier := parts[0], parts[1], parts[2], parts[3]
			labelVals := []string{c.accountID, region, modelID, family, priceTier}

			switch direction {
			case "input":
				ch <- prometheus.MustNewConstMetric(InputTokenCostDesc, prometheus.GaugeValue, price, labelVals...)
			case "output":
				ch <- prometheus.MustNewConstMetric(OutputTokenCostDesc, prometheus.GaugeValue, price, labelVals...)
			case "search":
				ch <- prometheus.MustNewConstMetric(SearchUnitCostDesc, prometheus.GaugeValue, price, labelVals...)
			default:
				c.logger.LogAttrs(ctx, slog.LevelWarn, "unknown direction in Bedrock pricing key, skipping",
					slog.String("region", region),
					slog.String("direction", direction))
			}
		}
	}

	return ctx.Err()
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- InputTokenCostDesc
	ch <- OutputTokenCostDesc
	ch <- SearchUnitCostDesc
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Regions() []string {
	return c.regions
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}

// Endpoint vs. filter region. The AWS Pricing API is only served from us-east-1 and
// ap-south-1, so the *client* passed in must be pinned to one of those regions (see
// pkg/aws/aws.go where the Bedrock pricing client is created against us-east-1).
//
// The `region` argument passed into the returned PriceFetchFunc is a different thing:
// pkg/aws/client.listBedrockPrices -> listServicePrices applies it as a `regionCode`
// TermMatch filter on GetProducts, so each invocation returns only that region's SKUs.
// The pricingstore fans out one call per configured region; do NOT replace this with a
// single call, or the resulting snapshot will lose regional separation.
func newPriceFetcher(pricingClient client.Client, familyFilter *regexp.Regexp, logger *slog.Logger) pricingstore.PriceFetchFunc {
	return func(ctx context.Context, region string) ([]string, error) {
		standard, err := pricingClient.ListBedrockPrices(ctx, region)
		if err != nil {
			return nil, err
		}
		result := preprocessBedrockPrices(standard, familyFilter, logger)

		// Marketplace pricing is best-effort. A failure fetching AmazonBedrockFoundationModels
		// must not drop the standard AmazonBedrock pricing, so log and continue with the
		// standard SKUs rather than failing the whole collector.
		marketplace, err := pricingClient.ListBedrockMarketplacePrices(ctx, region)
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelWarn, "failed to fetch Bedrock marketplace pricing, continuing with standard pricing only",
				slog.String("region", region),
				slog.String("error", err.Error()))
			return result, nil
		}
		result = append(result, preprocessBedrockMarketplacePrices(marketplace, familyFilter, logger)...)
		return result, nil
	}
}

func preprocessBedrockPrices(rawItems []string, familyFilter *regexp.Regexp, logger *slog.Logger) []string {
	result := make([]string, 0, len(rawItems))
	for _, raw := range rawItems {
		processed, ok, err := encodeBedrockPriceJSON(raw, familyFilter)
		if err != nil {
			logger.Warn("skipping Bedrock standard pricing SKU", "error", err)
		}
		if ok {
			result = append(result, processed)
		}
	}
	return result
}

// Returns ok=false with a nil error for intentional skips (unrecognised type, family filter).
// Returns ok=false with a non-nil error for unexpected failures (bad JSON, price parse error).
func encodeBedrockPriceJSON(raw string, familyFilter *regexp.Regexp) (string, bool, error) {
	var info bedrockProductInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return "", false, fmt.Errorf("unmarshalling SKU: %w", err)
	}

	attrs := &info.Product.Attributes
	direction, ok := classifyInferenceType(attrs.InferenceType)
	if !ok {
		// Fallback for SKUs where inferenceType is absent (e.g. AmazonRerank-v1-searchunits).
		direction, ok = classifyByUsageType(attrs.UsageType)
		if !ok {
			return "", false, nil
		}
	}

	slug, priceTier := parseBedrockModelID(attrs.UsageType)
	if slug == "" {
		return "", false, nil
	}

	// Prefer the human-readable `model` attribute, normalized to the same lowercase-hyphen slug
	// the marketplace source uses (e.g. "Claude 3 Sonnet" -> "claude-3-sonnet"), so model_id is
	// uniform across both sources. Append the modality segment (e.g. -speech/-text) so models
	// that AWS prices per modality under one `model` name (Nova Sonic) stay distinct instead of
	// colliding on one key. SKUs without a `model` attribute (some Titan, Rerank) fall back to
	// the normalized usagetype slug, keeping the same lowercase-hyphen style.
	modelID := normalizeModelID(slug)
	if normalized := normalizeModelID(attrs.Model); normalized != "" {
		modelID = normalized + modalitySuffix(attrs.UsageType)
	}

	family := normalizeProvider(attrs.Provider)
	if !familyFilter.MatchString(family) {
		return "", false, nil
	}

	// The Pricing API publishes search unit prices per single unit; scale to per-1k to match
	// the SearchUnitCostDesc metric name.
	if direction == "search" {
		if err := scaleSearchUnitPrices(&info); err != nil {
			return "", false, fmt.Errorf("scaling search unit prices for %q: %w", attrs.UsageType, err)
		}
	}

	attrs.UsageType = strings.Join([]string{family, direction, modelID, priceTier}, compositeKeySep)

	modified, err := json.Marshal(&info)
	if err != nil {
		return "", false, fmt.Errorf("marshalling SKU %q: %w", attrs.UsageType, err)
	}
	return string(modified), true, nil
}

// classifyByUsageType is a fallback for SKUs where inferenceType is absent.
func classifyByUsageType(usagetype string) (direction string, ok bool) {
	if strings.Contains(strings.ToLower(usagetype), "searchunits") {
		return "search", true
	}
	return "", false
}

// scaleSearchUnitPrices multiplies all USD prices in the SKU by searchUnitsPerKilo.
// PricePerUnit is a map (reference type) so the modification is in-place.
func scaleSearchUnitPrices(info *bedrockProductInfo) error {
	for _, term := range info.Terms.OnDemand {
		for _, pd := range term.PriceDimensions {
			usd, ok := pd.PricePerUnit["USD"]
			if !ok {
				continue
			}
			price, err := strconv.ParseFloat(usd, 64)
			if err != nil {
				return err
			}
			pd.PricePerUnit["USD"] = strconv.FormatFloat(price*searchUnitsPerKilo, 'f', -1, 64)
		}
	}
	return nil
}

func classifyInferenceType(inferenceType string) (direction string, ok bool) {
	lower := strings.ToLower(inferenceType)
	if strings.HasPrefix(lower, "prompt cache") {
		return "", false
	}
	for _, media := range []string{"image", "video", "audio"} {
		if strings.Contains(lower, media) {
			return "", false
		}
	}
	if strings.Contains(lower, "input tokens") || strings.Contains(lower, "text input token") {
		return "input", true
	}
	if strings.Contains(lower, "output tokens") {
		return "output", true
	}
	if strings.Contains(lower, "search unit") || strings.Contains(lower, "rerank") {
		return "search", true
	}
	return "", false
}

// tokenTypeMarkers are usagetype substrings that mark the boundary between model ID and
// token type. Checked in order so longer matches take priority over shorter ones.
var tokenTypeMarkers = []string{
	"-text-input-tokens",
	"-input-tokens",
	"-output-tokens",
	"-search-units",
	"-searchunits", // Rerank usagetype format (e.g. AmazonRerank-v1-searchunits)
}

func parseBedrockModelID(usagetype string) (modelID, priceTier string) {
	slug := usagetype
	if i := strings.Index(usagetype, "-"); i >= 0 {
		slug = usagetype[i+1:]
	}

	for _, marker := range tokenTypeMarkers {
		if idx := strings.Index(slug, marker); idx >= 0 {
			tierSuffix := slug[idx+len(marker):]
			return slug[:idx], extractPriceTier(tierSuffix)
		}
	}
	return "", priceTierOnDemand
}

func extractPriceTier(suffix string) string {
	lower := strings.ToLower(suffix)
	if strings.Contains(lower, "cross-region") {
		return priceTierCrossRegion
	}
	switch {
	case strings.HasSuffix(lower, "-batch"):
		return priceTierOnDemandBatch
	case strings.HasSuffix(lower, "-flex"):
		return priceTierOnDemandFlex
	case strings.HasSuffix(lower, "-priority"):
		return priceTierOnDemandPriority
	default:
		return priceTierOnDemand
	}
}

// Empty provider maps to "amazon" (Nova, Titan, and other Amazon-developed models).
func normalizeProvider(provider string) string {
	if provider == "" {
		return "amazon"
	}
	return strings.ReplaceAll(strings.ToLower(provider), " ", "_")
}

func preprocessBedrockMarketplacePrices(rawItems []string, familyFilter *regexp.Regexp, logger *slog.Logger) []string {
	result := make([]string, 0, len(rawItems))
	for _, raw := range rawItems {
		processed, ok, err := encodeBedrockMarketplacePriceJSON(raw, familyFilter)
		if err != nil {
			logger.Warn("skipping Bedrock marketplace pricing SKU", "error", err)
		}
		if ok {
			result = append(result, processed)
		}
	}
	return result
}

// encodeBedrockMarketplacePriceJSON processes a SKU from AmazonBedrockFoundationModels.
// It re-encodes the SKU with the composite key in usagetype so pricingstore can store it.
// Prices are converted from $/1M tokens to $/1K tokens; search unit prices from $/unit to $/1K units.
// Returns ok=false with a nil error for intentional skips; non-nil error for unexpected failures.
func encodeBedrockMarketplacePriceJSON(raw string, familyFilter *regexp.Regexp) (string, bool, error) {
	var info bedrockMarketplaceProductInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return "", false, fmt.Errorf("unmarshalling marketplace SKU: %w", err)
	}

	attrs := &info.Product.Attributes
	modelID := normalizeModelID(strings.TrimSuffix(attrs.ServiceName, marketplaceSuffix))
	if modelID == "" {
		return "", false, nil
	}

	family := familyFromServiceName(attrs.ServiceName)
	if !familyFilter.MatchString(family) {
		return "", false, nil
	}

	// Cache read/write are input-token operations; storage is per token-hour (a different unit)
	// so it is skipped. Non-cache SKUs fall through to the normal direction classifier.
	cacheOp, skipCache := marketplaceCacheOp(attrs.UsageType)
	if skipCache {
		// Storage (per token-hour, a different unit) is an expected skip; anything else is an
		// unrecognized cache shape worth surfacing so we add it rather than mislabel it.
		if !strings.Contains(strings.ToLower(attrs.UsageType), "storage") {
			return "", false, fmt.Errorf("unrecognized cache SKU, skipping: %q", attrs.UsageType)
		}
		return "", false, nil
	}
	direction := "input"
	if cacheOp == "" {
		var ok bool
		direction, ok = classifyMarketplaceUsageType(attrs.UsageType)
		if !ok {
			return "", false, nil
		}
	}

	priceTier := extractMarketplacePriceTier(attrs.UsageType, cacheOp)

	for _, term := range info.Terms.OnDemand {
		for _, pd := range term.PriceDimensions {
			usd, ok := pd.PricePerUnit["USD"]
			if !ok {
				continue
			}
			price, err := strconv.ParseFloat(usd, 64)
			if err != nil {
				return "", false, fmt.Errorf("parsing price for %q: %w", attrs.UsageType, err)
			}
			var converted float64
			if direction == "search" {
				// Marketplace publishes $/search unit; metric is $/1K search units.
				converted = price * searchUnitsPerKilo
			} else {
				// Marketplace publishes $/1M tokens; metric is $/1K tokens.
				converted = price / marketplacePerMillionToPerKilo
			}
			pd.PricePerUnit["USD"] = strconv.FormatFloat(converted, 'f', -1, 64)
		}
	}

	attrs.UsageType = strings.Join([]string{family, direction, modelID, priceTier}, compositeKeySep)

	modified, err := json.Marshal(&info)
	if err != nil {
		return "", false, fmt.Errorf("marshalling marketplace SKU %q: %w", attrs.UsageType, err)
	}
	return string(modified), true, nil
}

// multiHyphen matches runs of two or more hyphens, produced when a servicename contains a
// " - " separator (the surrounding spaces each become a hyphen alongside the literal one).
var multiHyphen = regexp.MustCompile(`-{2,}`)

// normalizeModelID converts a marketplace servicename into a canonical model_id slug:
// lowercase, with spaces replaced by hyphens, so model IDs are uniform across the AI
// pricing collectors.
//
// Parenthesis characters are dropped but their content is kept, so context variants stay
// distinct (e.g. "Claude (100K)" → "claude-100k", not "claude"). Runs of hyphens from
// " - " separators are collapsed, and leading/trailing hyphens are trimmed.
// Example: "Cohere Embed 3 Model - English" → "cohere-embed-3-model-english"
func normalizeModelID(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.NewReplacer("(", "", ")", "").Replace(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = multiHyphen.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// modalitySuffix returns the modality segment of a standard usagetype when present. AWS prices
// some models (Nova Sonic) per modality at different rates while publishing a single `model`
// name, so the modality must be folded into model_id to keep the variants distinct.
func modalitySuffix(usagetype string) string {
	lower := strings.ToLower(usagetype)
	switch {
	case strings.Contains(lower, "-speech-"):
		return "-speech"
	case strings.Contains(lower, "-text-"):
		return "-text"
	default:
		return ""
	}
}

// familyFromServiceName extracts the provider family from a marketplace servicename.
// E.g. "Claude Sonnet 4.6 (Amazon Bedrock Edition)" → "anthropic".
// Unrecognised providers return "unknown" rather than guessing from the first word,
// which keeps the family label bounded and predictable.
func familyFromServiceName(servicename string) string {
	lower := strings.ToLower(servicename)
	switch {
	case strings.HasPrefix(lower, "claude"):
		return "anthropic"
	case strings.HasPrefix(lower, "cohere"):
		return "cohere"
	case strings.HasPrefix(lower, "meta"):
		return "meta"
	case strings.HasPrefix(lower, "jamba"), strings.HasPrefix(lower, "jurassic"):
		return "ai21"
	case strings.HasPrefix(lower, "stable"):
		return "stability"
	case strings.HasPrefix(lower, "palmyra"):
		return "writer"
	case strings.HasPrefix(lower, "twelvelabs"):
		return "twelvelabs"
	default:
		return "unknown"
	}
}

// classifyMarketplaceUsageType determines the metric direction from the usagetype suffix.
// The AmazonBedrockFoundationModels usagetype encodes direction in the segment after "MP:region_".
func classifyMarketplaceUsageType(usagetype string) (direction string, ok bool) {
	lower := strings.ToLower(usagetype)
	// lctx (long context) SKUs share the direction/tier of standard SKUs and would overwrite
	// their prices; skip until long context is modelled as its own price tier. Cache SKUs are
	// handled by the caller before this point.
	for _, skip := range []string{"image", "video", "audio", "provisionedthroughput", "created_image", "request", "lctx"} {
		if strings.Contains(lower, skip) {
			return "", false
		}
	}
	switch {
	case strings.Contains(lower, "inputtokencount"), strings.Contains(lower, "input_tokens"):
		return "input", true
	case strings.Contains(lower, "outputtokencount"), strings.Contains(lower, "output_tokens"):
		return "output", true
	case strings.Contains(lower, "search_units"):
		return "search", true
	default:
		return "", false
	}
}

// extractMarketplacePriceTier derives price tier from the marketplace usagetype suffix.
// Cross-region ("global") and quota-tier ("batch", "priority", "flex") can stack —
// check batch/priority/flex before the global catch-all.
// composeTier builds a price_tier from its parts: an optional cross-region prefix, the base
// operation (on_demand or a cache_* op), and an optional quota tier suffix (batch/flex/priority/
// latency_optimized). Components stack, e.g. cross_region_cache_read, cache_write_1h,
// on_demand_batch. Each distinct combination gets a distinct value, so SKUs that differ only by a
// qualifier do not collide on one key.
func composeTier(crossRegion bool, op, quota string) string {
	if op == "" {
		op = priceTierOnDemand
	}
	parts := make([]string, 0, 3)
	if crossRegion {
		parts = append(parts, priceTierCrossRegion)
	}
	// Drop a redundant on_demand when cross-region already carries the base meaning, matching the
	// established names (cross_region, not cross_region_on_demand).
	if !crossRegion || op != priceTierOnDemand {
		parts = append(parts, op)
	}
	if quota != "" {
		parts = append(parts, quota)
	}
	return strings.Join(parts, "_")
}

// marketplaceCacheOp returns the cache operation for a marketplace usagetype, and whether it is a
// cache-storage SKU. Storage is priced per token-hour (a different unit), so the caller skips it.
// Returns "" for non-cache SKUs. Reads are a single rate; writes split by 5-minute vs 1-hour TTL.
func marketplaceCacheOp(usagetype string) (op string, skip bool) {
	lower := strings.ToLower(usagetype)
	if !strings.Contains(lower, "cache") {
		return "", false
	}
	switch {
	case strings.Contains(lower, "cacheread") || strings.Contains(lower, "cache_read"):
		return priceTierCacheRead, false
	case strings.Contains(lower, "cachewrite") || strings.Contains(lower, "cache_write"):
		if strings.Contains(lower, "1h") {
			return priceTierCacheWrite1h, false
		}
		// AWS bills the bare write (no TTL token) at the 5-minute default.
		return priceTierCacheWrite5m, false
	default:
		// Cache storage (per token-hour, a different unit) and any cache shape that is neither a
		// read nor a write: drop it rather than mislabel it as a 5-minute write.
		return "", true
	}
}

func extractMarketplacePriceTier(usagetype, cacheOp string) string {
	lower := strings.ToLower(usagetype)
	crossRegion := strings.Contains(lower, "_global")

	var quota string
	switch {
	case strings.Contains(lower, "_batch"):
		quota = "batch"
	case strings.Contains(lower, "_priority"):
		quota = "priority"
	case strings.Contains(lower, "_flex"):
		quota = "flex"
	case strings.Contains(lower, "latencyoptimized"):
		quota = "latency_optimized"
	}

	return composeTier(crossRegion, cacheOp, quota)
}
