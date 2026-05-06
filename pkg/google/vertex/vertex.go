package vertex

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	subsystem            = "gcp_vertex"
	PriceRefreshInterval = 24 * time.Hour
)

var (
	vertexTokenInputDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix, subsystem, utils.InputTokenCostSuffix,
		"Vertex AI input cost in USD per 1k tokens, for models billed by token. Character-billed models use the character metric.",
		[]string{"model_id", "family", "region"},
	)
	vertexTokenOutputDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix, subsystem, utils.OutputTokenCostSuffix,
		"Vertex AI output cost in USD per 1k tokens, for models billed by token. Character-billed models use the character metric.",
		[]string{"model_id", "family", "region"},
	)
	vertexCharacterInputDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix, subsystem, utils.CharacterInputCostSuffix,
		"Vertex AI input character cost in USD per 1k characters (models billed per character, e.g. translation models).",
		[]string{"model_id", "family", "region"},
	)
	vertexCharacterOutputDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix, subsystem, utils.CharacterOutputCostSuffix,
		"Vertex AI output character cost in USD per 1k characters (models billed per character, e.g. translation models).",
		[]string{"model_id", "family", "region"},
	)
	vertexComputeCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix, subsystem, utils.InstanceTotalCostSuffix,
		"Vertex AI custom training and online prediction node cost in USD per hour.",
		[]string{"machine_type", "use_case", "region", "price_tier"},
	)
	vertexRerankCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix, subsystem, utils.SearchUnitCostSuffix,
		"Vertex AI reranking cost in USD per 1k ranking requests.",
		[]string{"model_id", "family", "region"},
	)
)

// Collector collects Vertex AI pricing metrics.
type Collector struct {
	gcpClient  client.Client
	pricingMap *PricingMap
	logger     *slog.Logger
}

// New creates and initialises a Vertex AI Collector.
// Pricing is fetched at construction time; an error means the collector cannot be used.
func New(ctx context.Context, logger *slog.Logger, gcpClient client.Client) (*Collector, error) {
	logger = logger.With("collector", subsystem)

	pm, err := NewPricingMap(ctx, gcpClient)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize pricing map: %w", err)
	}

	go func() {
		ticker := time.NewTicker(PriceRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := pm.Populate(ctx); err != nil {
					logger.Error("failed to refresh vertex pricing", "error", err)
				}
			}
		}
	}()

	return &Collector{
		gcpClient:  gcpClient,
		pricingMap: pm,
		logger:     logger,
	}, nil
}

// Register implements provider.Collector.
func (c *Collector) Register(_ provider.Registry) error {
	return nil
}

// Describe implements provider.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- vertexTokenInputDesc
	ch <- vertexTokenOutputDesc
	ch <- vertexCharacterInputDesc
	ch <- vertexCharacterOutputDesc
	ch <- vertexComputeCostDesc
	ch <- vertexRerankCostDesc
	return nil
}

// Name implements provider.Collector.
func (c *Collector) Name() string {
	return subsystem
}

// Collect emits Vertex AI pricing metrics.
func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	snapshot := c.pricingMap.Snapshot()
	for region, models := range snapshot.tokens {
		for model, pricing := range models {
			family := familyFromModelID(model)
			ch <- prometheus.MustNewConstMetric(vertexTokenInputDesc, prometheus.GaugeValue,
				pricing.InputPer1kTokens, model, family, region)
			ch <- prometheus.MustNewConstMetric(vertexTokenOutputDesc, prometheus.GaugeValue,
				pricing.OutputPer1kTokens, model, family, region)
		}
	}
	for region, models := range snapshot.characters {
		for model, pricing := range models {
			family := familyFromModelID(model)
			ch <- prometheus.MustNewConstMetric(vertexCharacterInputDesc, prometheus.GaugeValue,
				pricing.InputPer1kChars, model, family, region)
			ch <- prometheus.MustNewConstMetric(vertexCharacterOutputDesc, prometheus.GaugeValue,
				pricing.OutputPer1kChars, model, family, region)
		}
	}
	for region, machines := range snapshot.compute {
		for machineType, useCases := range machines {
			for useCase, pricing := range useCases {
				ch <- prometheus.MustNewConstMetric(vertexComputeCostDesc, prometheus.GaugeValue,
					pricing.OnDemandPerHour, machineType, useCase, region, "on_demand")
				if pricing.SpotPerHour > 0 {
					ch <- prometheus.MustNewConstMetric(vertexComputeCostDesc, prometheus.GaugeValue,
						pricing.SpotPerHour, machineType, useCase, region, "spot")
				}
			}
		}
	}
	for region, models := range snapshot.reranking {
		for model, price := range models {
			ch <- prometheus.MustNewConstMetric(vertexRerankCostDesc, prometheus.GaugeValue,
				price, model, familyFromModelID(model), region)
		}
	}
	return ctx.Err()
}

// familyFromModelID derives the model provider family from a normalised model ID.
// Gemini, Gemma, and Discovery Engine reranking models are Google's; Claude models are
// Anthropic's. Some Claude SKUs carry a billing-category prefix (e.g. "ai-dev-tools:-claude-*"),
// so the check uses Contains rather than HasPrefix. Model Garden 3rd-party models embed the
// model name inside a long prefix, so Contains is used throughout. Unknown prefixes return
// "unknown" rather than assuming a provider.
func familyFromModelID(model string) string {
	switch {
	case strings.HasPrefix(model, "gemini"):
		return "google"
	case strings.Contains(model, "gemma"):
		return "google" // Gemma is Google DeepMind's open model family
	case strings.Contains(model, "claude"):
		return "anthropic"
	case strings.HasPrefix(model, "semantic"):
		return "google" // Discovery Engine reranking is a Google service
	case strings.Contains(model, "deepseek"):
		return "deepseek"
	case strings.Contains(model, "llama"):
		return "meta"
	case strings.Contains(model, "qwen"):
		return "alibaba"
	case strings.Contains(model, "minimax"):
		return "minimax"
	case strings.Contains(model, "kimi"):
		return "moonshot"
	default:
		return "unknown"
	}
}
