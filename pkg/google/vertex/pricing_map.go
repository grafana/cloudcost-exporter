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
	// discoveryEngineServiceName is the GCP Billing API service name for Discovery Engine,
	// which hosts the Ranking API used for reranking.
	// NOTE: Must be verified against the live GCP Billing API.
	discoveryEngineServiceName = "Cloud Discovery Engine"
)

var (
	// tokenInputRegex matches Vertex AI input token/character SKU descriptions for any model family.
	// Examples: "Gemini 1.5 Flash Input tokens", "Claude 3.5 Sonnet Input tokens",
	//           "Gemini Embedding 001 Input characters"
	// NOTE: Exact SKU description strings must be verified against the live GCP Billing API.
	tokenInputRegex = regexp.MustCompile(`(?i)^(.+?)\s+Input\s+(tokens?|characters?)$`)
	// tokenOutputRegex matches Vertex AI output token/character SKU descriptions for any model family.
	// Examples: "Gemini 1.5 Flash Output tokens", "Claude 3.5 Sonnet Output tokens"
	tokenOutputRegex = regexp.MustCompile(`(?i)^(.+?)\s+Output\s+(tokens?|characters?)$`)
	// computeRegex matches custom training/prediction compute SKU descriptions.
	// Example: "Custom Training n1-standard-4 running in us-central1"
	// Example: "Spot Custom Prediction n1-highmem-8 running in europe-west1"
	// NOTE: Exact SKU description strings must be verified against the live GCP Billing API.
	computeRegex = regexp.MustCompile(`(?i)^(Spot\s+)?Custom\s+(Training|Prediction)\s+(\S+)\s+running\s+in`)
	// rerankRegex matches Discovery Engine Ranking API SKU descriptions.
	// Example: "Semantic Ranker API Ranking Requests"
	// NOTE: Exact SKU description strings must be verified against the live GCP Billing API.
	rerankRegex = regexp.MustCompile(`(?i)^(.+?)\s+[Rr]anking\s+[Rr]equests?$`)
)

// TokenPricing holds per-1k-token prices for a Gemini model.
type TokenPricing struct {
	InputPer1kTokens  float64
	OutputPer1kTokens float64
}

// ComputePricing holds per-hour prices for a Vertex AI compute node.
type ComputePricing struct {
	OnDemandPerHour float64
	SpotPerHour     float64
}

// Snapshot is an immutable view of the Vertex AI pricing data.
type Snapshot struct {
	// tokens[region][model] = TokenPricing
	tokens map[string]map[string]*TokenPricing
	// compute[region][machineType][useCase] = ComputePricing
	compute map[string]map[string]map[string]*ComputePricing
	// reranking[region][model] = price per 1k ranking requests (USD)
	reranking map[string]map[string]float64
}

// PricingMap stores Vertex AI pricing and refreshes atomically.
type PricingMap struct {
	gcpClient client.Client
	current   atomic.Pointer[Snapshot]
}

// NewPricingMap initialises and populates a PricingMap.
func NewPricingMap(ctx context.Context, gcpClient client.Client) (*PricingMap, error) {
	pm := &PricingMap{gcpClient: gcpClient}
	if err := pm.Populate(ctx); err != nil {
		return nil, err
	}
	return pm, nil
}

// Snapshot returns an immutable copy of the current pricing data.
func (pm *PricingMap) Snapshot() Snapshot {
	if s := pm.current.Load(); s != nil {
		return *s
	}
	return Snapshot{}
}

// Populate fetches the latest Vertex AI SKUs and updates the pricing map.
// Discovery Engine SKUs (reranking) are fetched non-fatally; reranking metrics
// are omitted if the service is unavailable.
func (pm *PricingMap) Populate(ctx context.Context) error {
	serviceName, err := pm.gcpClient.GetServiceName(ctx, vertexAIServiceName)
	if err != nil {
		return fmt.Errorf("failed to get Vertex AI service name: %w", err)
	}
	skus := pm.gcpClient.GetPricing(ctx, serviceName)
	if len(skus) == 0 {
		return fmt.Errorf("no SKUs found for Vertex AI service")
	}

	if deSvcName, err := pm.gcpClient.GetServiceName(ctx, discoveryEngineServiceName); err != nil {
		slog.Warn("failed to get Discovery Engine service name, reranking metrics will be unavailable", "error", err)
	} else {
		skus = append(skus, pm.gcpClient.GetPricing(ctx, deSvcName)...)
	}

	return pm.ParseSkus(skus)
}

// ParseSkus parses the provided SKUs and updates the pricing map atomically.
// Unknown SKUs are logged at debug level and skipped.
func (pm *PricingMap) ParseSkus(skus []*billingpb.Sku) error {
	snap := &Snapshot{
		tokens:    make(map[string]map[string]*TokenPricing),
		compute:   make(map[string]map[string]map[string]*ComputePricing),
		reranking: make(map[string]map[string]float64),
	}

	for _, sku := range skus {
		if sku == nil {
			continue
		}
		desc := sku.GetDescription()
		regions := skuRegions(sku)

		if matches := tokenInputRegex.FindStringSubmatch(desc); len(matches) > 0 {
			model := normalizeModelName(matches[1])
			price := normalizeToPerK(priceFromSku(sku), sku)
			for _, region := range regions {
				if region == "" {
					continue
				}
				if snap.tokens[region] == nil {
					snap.tokens[region] = make(map[string]*TokenPricing)
				}
				if snap.tokens[region][model] == nil {
					snap.tokens[region][model] = &TokenPricing{}
				}
				snap.tokens[region][model].InputPer1kTokens = price
			}
			continue
		}

		if matches := tokenOutputRegex.FindStringSubmatch(desc); len(matches) > 0 {
			model := normalizeModelName(matches[1])
			price := normalizeToPerK(priceFromSku(sku), sku)
			for _, region := range regions {
				if region == "" {
					continue
				}
				if snap.tokens[region] == nil {
					snap.tokens[region] = make(map[string]*TokenPricing)
				}
				if snap.tokens[region][model] == nil {
					snap.tokens[region][model] = &TokenPricing{}
				}
				snap.tokens[region][model].OutputPer1kTokens = price
			}
			continue
		}

		if matches := computeRegex.FindStringSubmatch(desc); len(matches) > 0 {
			isSpot := strings.TrimSpace(matches[1]) != ""
			useCase := strings.ToLower(matches[2])
			machineType := strings.ToLower(matches[3])
			price := priceFromSku(sku)
			for _, region := range regions {
				if region == "" {
					continue
				}
				if snap.compute[region] == nil {
					snap.compute[region] = make(map[string]map[string]*ComputePricing)
				}
				if snap.compute[region][machineType] == nil {
					snap.compute[region][machineType] = make(map[string]*ComputePricing)
				}
				if snap.compute[region][machineType][useCase] == nil {
					snap.compute[region][machineType][useCase] = &ComputePricing{}
				}
				cp := snap.compute[region][machineType][useCase]
				if isSpot {
					cp.SpotPerHour = price
				} else {
					cp.OnDemandPerHour = price
				}
			}
			continue
		}

		if matches := rerankRegex.FindStringSubmatch(desc); len(matches) > 0 {
			model := normalizeModelName(matches[1])
			price := normalizeToPerK(priceFromSku(sku), sku)
			for _, region := range regions {
				if region == "" {
					continue
				}
				if snap.reranking[region] == nil {
					snap.reranking[region] = make(map[string]float64)
				}
				snap.reranking[region][model] = price
			}
			continue
		}

		slog.Debug("skipping unknown Vertex AI SKU", "description", desc)
	}

	pm.current.Store(snap)
	return nil
}

// priceFromSku extracts the unit price from a SKU's last tiered rate.
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

// normalizeModelName converts a model name from a SKU description to a canonical slug.
// Example: "1.5 Flash" → "1.5-flash"
func normalizeModelName(raw string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), " ", "-"))
}
