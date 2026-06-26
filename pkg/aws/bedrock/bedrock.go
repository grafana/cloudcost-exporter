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

	// token_type label values. Prompt-cache read/write are input-side operations and so are
	// emitted on the token metric alongside input/output.
	tokenTypeInput      = "input"
	tokenTypeOutput     = "output"
	tokenTypeCacheRead  = "cache_read"
	tokenTypeCacheWrite = "cache_write"

	// directionSearch routes a SKU to the search-unit metric. It is not a token_type value.
	directionSearch = "search"

	// region_tier label values: in-region vs cross-region inference.
	regionTierIn    = "in"
	regionTierCross = "cross"

	// quota_tier label values: the on-demand quota a price applies to.
	quotaTierStandard         = "standard"
	quotaTierBatch            = "batch"
	quotaTierFlex             = "flex"
	quotaTierPriority         = "priority"
	quotaTierLatencyOptimized = "latency_optimized"

	// cache_ttl label values, set only for cache_write (empty otherwise).
	cacheTTL5m = "5m"
	cacheTTL1h = "1h"

	// compositeKeySep separates the fields encoded in the pricingstore usagetype key:
	// family|model_id|token_type|region_tier|quota_tier|cache_ttl
	compositeKeySep    = "|"
	compositeKeyFields = 6

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
	// TokenCostDesc carries every per-token price (input, output, and prompt-cache read/write),
	// distinguished by orthogonal labels rather than separate metric names or a composed price
	// tier, so downstream joins key on (model_id, token_type, region_tier) directly.
	TokenCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.TokenCostSuffix,
		"The cost of AWS Bedrock tokens in USD per 1000 tokens, by token_type",
		[]string{"account_id", "region", "model_id", "family", "token_type", "region_tier", "quota_tier", "cache_ttl"},
	)
	SearchUnitCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.SearchUnitCostSuffix,
		"The cost of AWS Bedrock search units in USD per 1000 search units (e.g. Cohere Rerank)",
		[]string{"account_id", "region", "model_id", "family", "region_tier", "quota_tier"},
	)
)

// pricePoint is the decomposed identity of a single Bedrock price. It is encoded into the
// pricingstore usagetype key (the store's per-region map key, which must stay unique per price)
// and expanded back into metric labels at collection time.
type pricePoint struct {
	family     string
	modelID    string
	tokenType  string // input|output|cache_read|cache_write; empty marks a search price
	regionTier string // in|cross
	quotaTier  string // standard|batch|flex|priority|latency_optimized
	cacheTTL   string // 5m|1h; empty for non-cache
}

// isSearch reports whether this is a search-unit price. Search SKUs carry no token_type, so an
// empty token_type is the marker; token prices always set one (input/output/cache_read/cache_write).
func (p pricePoint) isSearch() bool { return p.tokenType == "" }

func (p pricePoint) encode() string {
	return strings.Join([]string{p.family, p.modelID, p.tokenType, p.regionTier, p.quotaTier, p.cacheTTL}, compositeKeySep)
}

func decodePricePoint(key string) (pricePoint, bool) {
	parts := strings.SplitN(key, compositeKeySep, compositeKeyFields)
	if len(parts) != compositeKeyFields {
		return pricePoint{}, false
	}
	return pricePoint{
		family:     parts[0],
		modelID:    parts[1],
		tokenType:  parts[2],
		regionTier: parts[3],
		quotaTier:  parts[4],
		cacheTTL:   parts[5],
	}, true
}

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
			point, ok := decodePricePoint(compositeKey)
			if !ok {
				c.logger.LogAttrs(ctx, slog.LevelWarn, "malformed Bedrock pricing key, skipping",
					slog.String("region", region),
					slog.String("key", compositeKey))
				continue
			}

			if point.isSearch() {
				ch <- prometheus.MustNewConstMetric(SearchUnitCostDesc, prometheus.GaugeValue, price,
					c.accountID, region, point.modelID, point.family, point.regionTier, point.quotaTier)
			} else {
				ch <- prometheus.MustNewConstMetric(TokenCostDesc, prometheus.GaugeValue, price,
					c.accountID, region, point.modelID, point.family, point.tokenType, point.regionTier, point.quotaTier, point.cacheTTL)
			}
		}
	}

	return ctx.Err()
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- TokenCostDesc
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

	slug, suffix := parseBedrockModelID(attrs.UsageType)
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
	if direction == directionSearch {
		if err := scaleSearchUnitPrices(&info); err != nil {
			return "", false, fmt.Errorf("scaling search unit prices for %q: %w", attrs.UsageType, err)
		}
	}

	regionTier, quotaTier := standardTier(suffix)
	point := pricePoint{
		family:     family,
		modelID:    modelID,
		regionTier: regionTier,
		quotaTier:  quotaTier,
	}
	// Standard SKUs carry no prompt-cache prices (classifyInferenceType skips "prompt cache"),
	// so token_type is just the inference direction. Search SKUs leave token_type empty, which
	// marks them as search at collection time.
	if direction != directionSearch {
		point.tokenType = direction
	}

	attrs.UsageType = point.encode()

	modified, err := json.Marshal(&info)
	if err != nil {
		return "", false, fmt.Errorf("marshalling SKU %q: %w", attrs.UsageType, err)
	}
	return string(modified), true, nil
}

// classifyByUsageType is a fallback for SKUs where inferenceType is absent.
func classifyByUsageType(usagetype string) (direction string, ok bool) {
	if strings.Contains(strings.ToLower(usagetype), "searchunits") {
		return directionSearch, true
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
		return tokenTypeInput, true
	}
	if strings.Contains(lower, "output tokens") {
		return tokenTypeOutput, true
	}
	if strings.Contains(lower, "search unit") || strings.Contains(lower, "rerank") {
		return directionSearch, true
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

// parseBedrockModelID splits a standard usagetype into the model slug and the trailing
// qualifier suffix (everything after the token-type marker, e.g. "-batch",
// "-cross-region-global"). Returns an empty slug if no token-type marker is present.
func parseBedrockModelID(usagetype string) (modelID, suffix string) {
	slug := usagetype
	if i := strings.Index(usagetype, "-"); i >= 0 {
		slug = usagetype[i+1:]
	}

	for _, marker := range tokenTypeMarkers {
		if idx := strings.Index(slug, marker); idx >= 0 {
			return slug[:idx], slug[idx+len(marker):]
		}
	}
	return "", ""
}

// standardTier decodes a standard usagetype qualifier suffix into region and quota tiers.
// Cross-region and a quota qualifier can co-occur, so each is captured independently.
func standardTier(suffix string) (regionTier, quotaTier string) {
	lower := strings.ToLower(suffix)

	regionTier = regionTierIn
	if strings.Contains(lower, "cross-region") {
		regionTier = regionTierCross
	}

	quotaTier = quotaTierStandard
	switch {
	case strings.Contains(lower, "batch"):
		quotaTier = quotaTierBatch
	case strings.Contains(lower, "flex"):
		quotaTier = quotaTierFlex
	case strings.Contains(lower, "priority"):
		quotaTier = quotaTierPriority
	case strings.Contains(lower, "latency"):
		quotaTier = quotaTierLatencyOptimized
	}
	return regionTier, quotaTier
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
	cacheTokenType, cacheTTL, skipCache := marketplaceCacheOp(attrs.UsageType)
	if skipCache {
		// Storage (per token-hour, a different unit) is an expected skip; anything else is an
		// unrecognized cache shape worth surfacing so we add it rather than mislabel it.
		if !strings.Contains(strings.ToLower(attrs.UsageType), "storage") {
			return "", false, fmt.Errorf("unrecognized cache SKU, skipping: %q", attrs.UsageType)
		}
		return "", false, nil
	}

	regionTier, quotaTier := marketplaceTier(attrs.UsageType)
	point := pricePoint{
		family:     family,
		modelID:    modelID,
		regionTier: regionTier,
		quotaTier:  quotaTier,
	}
	if cacheTokenType != "" {
		point.tokenType = cacheTokenType
		point.cacheTTL = cacheTTL
	} else {
		direction, ok := classifyMarketplaceUsageType(attrs.UsageType)
		if !ok {
			return "", false, nil
		}
		// Search SKUs leave token_type empty (that marks them as search); token SKUs set it.
		if direction != directionSearch {
			point.tokenType = direction
		}
	}

	isSearch := point.isSearch()
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
			if isSearch {
				// Marketplace publishes $/search unit; metric is $/1K search units.
				converted = price * searchUnitsPerKilo
			} else {
				// Marketplace publishes $/1M tokens; metric is $/1K tokens.
				converted = price / marketplacePerMillionToPerKilo
			}
			pd.PricePerUnit["USD"] = strconv.FormatFloat(converted, 'f', -1, 64)
		}
	}

	attrs.UsageType = point.encode()

	modified, err := json.Marshal(&info)
	if err != nil {
		return "", false, fmt.Errorf("marshalling marketplace SKU %q: %w", attrs.UsageType, err)
	}
	return string(modified), true, nil
}

// multiHyphen matches runs of two or more hyphens, produced when a servicename contains a
// " - " separator (the surrounding spaces each become a hyphen alongside the literal one).
var multiHyphen = regexp.MustCompile(`-{2,}`)

// cacheWriteTTL matches the TTL token in a cache-write usagetype (e.g. "1h", "5m"). A bare write
// carries no token and defaults to 5 minutes; an unrecognized token means a TTL we do not model.
var cacheWriteTTL = regexp.MustCompile(`\d+[hm]`)

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
		return tokenTypeInput, true
	case strings.Contains(lower, "outputtokencount"), strings.Contains(lower, "output_tokens"):
		return tokenTypeOutput, true
	case strings.Contains(lower, "search_units"):
		return directionSearch, true
	default:
		return "", false
	}
}

// marketplaceTier decodes a marketplace usagetype into region and quota tiers. Cross-region
// ("_global") and a quota qualifier ("_batch", "_priority", "_flex", "latencyoptimized") are
// captured independently, so a SKU carrying both keeps both.
func marketplaceTier(usagetype string) (regionTier, quotaTier string) {
	lower := strings.ToLower(usagetype)

	regionTier = regionTierIn
	if strings.Contains(lower, "_global") {
		regionTier = regionTierCross
	}

	quotaTier = quotaTierStandard
	switch {
	case strings.Contains(lower, "_batch"):
		quotaTier = quotaTierBatch
	case strings.Contains(lower, "_priority"):
		quotaTier = quotaTierPriority
	case strings.Contains(lower, "_flex"):
		quotaTier = quotaTierFlex
	case strings.Contains(lower, "latencyoptimized"):
		quotaTier = quotaTierLatencyOptimized
	}
	return regionTier, quotaTier
}

// marketplaceCacheOp classifies a marketplace cache usagetype into a token_type and cache TTL.
// Returns ("", "", false) for non-cache SKUs. skip=true marks cache SKUs we do not model
// (storage, unrecognized shapes, unrecognized TTLs), so the caller drops them rather than
// mislabel them. Reads are a single rate (no TTL); writes split by 5-minute vs 1-hour TTL.
func marketplaceCacheOp(usagetype string) (tokenType, cacheTTL string, skip bool) {
	lower := strings.ToLower(usagetype)
	if !strings.Contains(lower, "cache") {
		return "", "", false
	}
	switch {
	case strings.Contains(lower, "cacheread") || strings.Contains(lower, "cache_read"):
		return tokenTypeCacheRead, "", false
	case strings.Contains(lower, "cachewrite") || strings.Contains(lower, "cache_write"):
		// Recognize the write TTL explicitly. AWS bills the bare write (no TTL token) at the
		// 5-minute default and tags the 1-hour write with "1h". Any other TTL token is one we do
		// not model yet, so skip it and surface it rather than silently labeling it a 5m write.
		switch cacheWriteTTL.FindString(lower) {
		case "", cacheTTL5m:
			return tokenTypeCacheWrite, cacheTTL5m, false
		case cacheTTL1h:
			return tokenTypeCacheWrite, cacheTTL1h, false
		default:
			return "", "", true
		}
	default:
		// Cache storage (per token-hour, a different unit) and any cache shape that is neither a
		// read nor a write: drop it rather than mislabel it as a 5-minute write.
		return "", "", true
	}
}
