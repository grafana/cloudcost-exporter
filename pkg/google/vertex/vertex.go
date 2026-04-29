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
		cloudcostexporter.MetricPrefix, subsystem, utils.TokenInputCostSuffix,
		"Vertex AI input token cost in USD per 1k tokens.",
		[]string{"model", "family", "region"},
	)
	vertexTokenOutputDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix, subsystem, utils.TokenOutputCostSuffix,
		"Vertex AI output token cost in USD per 1k tokens.",
		[]string{"model", "family", "region"},
	)
	vertexComputeCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix, subsystem, utils.InstanceTotalCostSuffix,
		"Vertex AI custom training and online prediction node cost in USD per hour.",
		[]string{"machine_type", "use_case", "region", "price_tier"},
	)
	vertexRerankCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix, subsystem, utils.SearchUnitCostSuffix,
		"Vertex AI reranking cost in USD per 1k ranking requests.",
		[]string{"model", "family", "region"},
	)
)

// Config holds configuration for the Vertex AI collector.
type Config struct {
	Projects       string
	ScrapeInterval time.Duration
}

// Collector collects Vertex AI pricing metrics.
type Collector struct {
	gcpClient  client.Client
	config     *Config
	projects   []string
	regions    []string
	pricingMap *PricingMap
	logger     *slog.Logger
}

// New creates and initialises a Vertex AI Collector.
// Pricing is fetched at construction time; an error means the collector cannot be used.
func New(ctx context.Context, config *Config, logger *slog.Logger, gcpClient client.Client) (*Collector, error) {
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

	projects := strings.Split(config.Projects, ",")
	regions := client.RegionsForProjects(gcpClient, projects, logger)

	return &Collector{
		gcpClient:  gcpClient,
		config:     config,
		projects:   projects,
		regions:    regions,
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
// Gemini and Discovery Engine reranking models are Google's; Claude models are Anthropic's.
// Unknown prefixes return "unknown" rather than assuming a provider.
func familyFromModelID(model string) string {
	switch {
	case strings.HasPrefix(model, "gemini"):
		return "google"
	case strings.HasPrefix(model, "claude"):
		return "anthropic"
	case strings.HasPrefix(model, "semantic"):
		return "google" // Discovery Engine reranking is a Google service
	default:
		return "unknown"
	}
}
