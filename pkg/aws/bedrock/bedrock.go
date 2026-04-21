package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

	priceTierOnDemand    = "on_demand"
	priceTierCrossRegion = "cross_region"

	// compositeKeySep separates the four fields encoded in the pricingstore usagetype key.
	compositeKeySep = "|"
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

type Config struct {
	Regions       []ec2types.Region
	PricingClient client.Client
	Logger        *slog.Logger
	AccountID     string
}

type Collector struct {
	pricingStore pricingstore.PricingStoreRefresher
	regions      []string
	logger       *slog.Logger
	accountID    string
}

func New(ctx context.Context, config *Config) (*Collector, error) {
	logger := slog.Default()
	if config.Logger != nil {
		logger = config.Logger.With("logger", serviceName)
	}

	pricingStore, err := pricingstore.NewPricingStore(ctx, logger, config.Regions, newPriceFetcher(config.PricingClient))
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

// newPriceFetcher returns a PriceFetchFunc that fetches and preprocesses Bedrock pricing data.
// The AWS Pricing API is only available in us-east-1 and ap-south-1; callers must pin the
// client to us-east-1 before passing it here.
func newPriceFetcher(pricingClient client.Client) pricingstore.PriceFetchFunc {
	return func(ctx context.Context, region string) ([]string, error) {
		rawItems, err := pricingClient.ListBedrockPrices(ctx, region)
		if err != nil {
			return nil, err
		}
		return preprocessBedrockPrices(rawItems), nil
	}
}

// preprocessBedrockPrices filters and transforms raw Pricing API JSON strings. Each item's
// usagetype field is replaced with a composite key encoding family, direction, model ID, and
// price tier. Non-text-token SKUs (image, video, audio, cache, guardrail) are dropped.
func preprocessBedrockPrices(rawItems []string) []string {
	result := make([]string, 0, len(rawItems))
	for _, raw := range rawItems {
		if processed, ok := encodeBedrockPriceJSON(raw); ok {
			result = append(result, processed)
		}
	}
	return result
}

// encodeBedrockPriceJSON parses one raw Pricing API JSON string and returns a modified copy
// with the usagetype replaced by a composite key: "<family>|<direction>|<modelID>|<priceTier>".
// Returns ok=false if the entry should be skipped (non-text-token inferenceType, parse failure,
// or unrecognized usagetype format).
func encodeBedrockPriceJSON(raw string) (string, bool) {
	var info bedrockProductInfo
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return "", false
	}

	attrs := &info.Product.Attributes
	direction, ok := classifyInferenceType(attrs.InferenceType)
	if !ok {
		return "", false
	}

	modelID, priceTier := parseBedrockModelID(attrs.UsageType)
	if modelID == "" {
		return "", false
	}

	family := normalizeProvider(attrs.Provider)
	attrs.UsageType = strings.Join([]string{family, direction, modelID, priceTier}, compositeKeySep)

	modified, err := json.Marshal(&info)
	if err != nil {
		return "", false
	}
	return string(modified), true
}

// classifyInferenceType maps the Bedrock Pricing API inferenceType attribute to a direction
// string ("input", "output", "search"). Returns ok=false for non-text-token types (image,
// video, audio, cache, guardrail) that should not be emitted as pricing metrics.
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
}

// parseBedrockModelID extracts the model ID slug and price tier from a Bedrock usagetype string.
// It strips the leading region prefix (e.g. "USE1-") and the trailing token-type suffix, then
// maps the remaining suffix to a price tier label.
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

// extractPriceTier maps the token-type suffix remainder (everything after the marker such as
// "-input-tokens") to a price_tier label value.
func extractPriceTier(suffix string) string {
	lower := strings.ToLower(suffix)
	if strings.Contains(lower, "cross-region") {
		return priceTierCrossRegion
	}
	switch {
	case strings.HasSuffix(lower, "-batch"):
		return "on_demand_batch"
	case strings.HasSuffix(lower, "-flex"):
		return "on_demand_flex"
	case strings.HasSuffix(lower, "-priority"):
		return "on_demand_priority"
	default:
		return priceTierOnDemand
	}
}

// normalizeProvider lowercases the provider attribute and replaces spaces with underscores
// for use as the family label. Amazon-developed models (Nova, Titan, etc.) have an empty
// provider attribute and are mapped to "amazon".
func normalizeProvider(provider string) string {
	if provider == "" {
		return "amazon"
	}
	return strings.ReplaceAll(strings.ToLower(provider), " ", "_")
}
