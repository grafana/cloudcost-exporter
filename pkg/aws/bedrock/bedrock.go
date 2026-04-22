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
		logger = config.Logger.With("collector", serviceName)
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

// Endpoint vs. filter region. The AWS Pricing API is only served from us-east-1 and
// ap-south-1, so the *client* passed in must be pinned to one of those regions (see
// pkg/aws/aws.go where the Bedrock pricing client is created against us-east-1).
//
// The `region` argument passed into the returned PriceFetchFunc is a different thing:
// pkg/aws/client.listBedrockPrices -> listServicePrices applies it as a `regionCode`
// TermMatch filter on GetProducts, so each invocation returns only that region's SKUs.
// The pricingstore fans out one call per configured region; do NOT replace this with a
// single call, or the resulting snapshot will lose regional separation.
func newPriceFetcher(pricingClient client.Client) pricingstore.PriceFetchFunc {
	return func(ctx context.Context, region string) ([]string, error) {
		rawItems, err := pricingClient.ListBedrockPrices(ctx, region)
		if err != nil {
			return nil, err
		}
		return preprocessBedrockPrices(rawItems), nil
	}
}

func preprocessBedrockPrices(rawItems []string) []string {
	result := make([]string, 0, len(rawItems))
	for _, raw := range rawItems {
		if processed, ok := encodeBedrockPriceJSON(raw); ok {
			result = append(result, processed)
		}
	}
	return result
}

// Returns ok=false for non-text-token types, parse failures, or unrecognized usagetype formats.
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
	// remove this filter to emit all model families
	if family != "anthropic" && family != "amazon" {
		return "", false
	}
	attrs.UsageType = strings.Join([]string{family, direction, modelID, priceTier}, compositeKeySep)

	modified, err := json.Marshal(&info)
	if err != nil {
		return "", false
	}
	return string(modified), true
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
		return "on_demand_batch"
	case strings.HasSuffix(lower, "-flex"):
		return "on_demand_flex"
	case strings.HasSuffix(lower, "-priority"):
		return "on_demand_priority"
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
