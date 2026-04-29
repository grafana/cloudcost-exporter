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
		utils.TokenInputCostSuffix,
		"The cost of AWS Bedrock input tokens in USD per 1000 tokens",
		[]string{"account_id", "region", "model_id", "family", "price_tier"},
	)
	OutputTokenCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		utils.TokenOutputCostSuffix,
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
	Logger        *slog.Logger
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

func New(ctx context.Context, config *Config) (*Collector, error) {
	logger := slog.Default()
	if config.Logger != nil {
		logger = config.Logger.With("collector", serviceName)
	}

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
		marketplace, err := pricingClient.ListBedrockMarketplacePrices(ctx, region)
		if err != nil {
			return nil, err
		}
		result := preprocessBedrockPrices(standard, familyFilter, logger)
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

	modelID, priceTier := parseBedrockModelID(attrs.UsageType)
	if modelID == "" {
		return "", false, nil
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
	modelID := strings.ReplaceAll(strings.TrimSuffix(attrs.ServiceName, marketplaceSuffix), " ", "_")
	if modelID == "" {
		return "", false, nil
	}

	family := familyFromServiceName(attrs.ServiceName)
	if !familyFilter.MatchString(family) {
		return "", false, nil
	}

	direction, ok := classifyMarketplaceUsageType(attrs.UsageType)
	if !ok {
		return "", false, nil
	}

	priceTier := extractMarketplacePriceTier(attrs.UsageType)

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

// familyFromServiceName extracts the provider family from a marketplace servicename.
// E.g. "Claude Sonnet 4.6 (Amazon Bedrock Edition)" → "anthropic".
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
		if i := strings.Index(lower, " "); i >= 0 {
			return lower[:i]
		}
		return lower
	}
}

// classifyMarketplaceUsageType determines the metric direction from the usagetype suffix.
// The AmazonBedrockFoundationModels usagetype encodes direction in the segment after "MP:region_".
func classifyMarketplaceUsageType(usagetype string) (direction string, ok bool) {
	lower := strings.ToLower(usagetype)
	// lctx (long context) SKUs share the direction/tier of standard SKUs and would overwrite
	// their prices; skip until long context is modelled as its own price tier.
	for _, skip := range []string{"image", "video", "audio", "provisionedthroughput", "created_image", "request", "cache", "lctx"} {
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
func extractMarketplacePriceTier(usagetype string) string {
	lower := strings.ToLower(usagetype)
	isCrossRegion := strings.Contains(lower, "_global")

	var quotaTier string
	switch {
	case strings.Contains(lower, "_batch"):
		quotaTier = priceTierOnDemandBatch
	case strings.Contains(lower, "_priority"):
		quotaTier = priceTierOnDemandPriority
	case strings.Contains(lower, "_flex"):
		quotaTier = priceTierOnDemandFlex
	}

	if isCrossRegion && quotaTier != "" {
		return priceTierCrossRegion + "_" + strings.TrimPrefix(quotaTier, "on_demand_")
	}
	if isCrossRegion {
		return priceTierCrossRegion
	}
	if quotaTier != "" {
		return quotaTier
	}
	return priceTierOnDemand
}
