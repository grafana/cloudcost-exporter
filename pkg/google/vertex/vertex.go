package vertex

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
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

	// gen_ai_token_type label values. Vertex prices input and output separately; the two are
	// emitted on one metric distinguished by this label, matching the Bedrock token metric.
	tokenTypeInput  = "input"
	tokenTypeOutput = "output"

	// rerankPriceTier is the price_tier value for reranking. The Ranking API is a single flat
	// rate with no tiering, so the label is constant. It exists to keep price_tier present on
	// every Vertex metric.
	rerankPriceTier = "on_demand"
)

var (
	// vertexTokenCostDesc carries both input and output token prices, distinguished by
	// gen_ai_token_type, mirroring the Bedrock token metric. price_tier stays a single composed
	// label: Vertex pricing has no cross-region tier and composes quota/caching/long-context/
	// thinking dimensions that do not split cleanly into Bedrock's region_tier/quota_tier.
	vertexTokenCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix, subsystem, utils.TokenCostSuffix,
		"Vertex AI cost in USD per 1k tokens, by gen_ai_token_type, for models billed by token. Character-billed models use the character metric.",
		[]string{"project_id", "region", "gen_ai_request_model", "family", "gen_ai_token_type", "price_tier"},
	)
	vertexCharacterCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix, subsystem, utils.CharacterCostSuffix,
		"Vertex AI cost in USD per 1k characters, by gen_ai_token_type, for models billed per character (e.g. translation models).",
		[]string{"project_id", "region", "gen_ai_request_model", "family", "gen_ai_token_type", "price_tier"},
	)
	vertexComputeCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix, subsystem, utils.InstanceTotalCostSuffix,
		"Vertex AI custom training and online prediction node cost in USD per hour.",
		[]string{"project_id", "machine_type", "use_case", "region", "price_tier"},
	)
	vertexRerankCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix, subsystem, utils.SearchUnitCostSuffix,
		"Vertex AI reranking cost in USD per 1k ranking requests.",
		[]string{"project_id", "region", "gen_ai_request_model", "family", "price_tier"},
	)
)

// Config configures the Vertex AI collector.
type Config struct {
	ProjectId    string // ProjectId is the auth project; used as the project_id fallback when Projects is empty.
	Projects     string // Projects is a comma-separated list of projects to emit prices for.
	FamilyFilter string // FamilyFilter is a regex matched against the family label; only matching families are emitted. Mirrors Bedrock's --aws.bedrock.families.
}

// Collector collects Vertex AI pricing metrics.
type Collector struct {
	pricingMap *PricingMap
	logger     *slog.Logger
	// projects is the list of project_id label values each price series is emitted under. Vertex
	// pricing is project-independent, so a price is repeated once per configured project so
	// downstream rules can join it against per-project usage.
	projects []string
}

// New creates and initialises a Vertex AI Collector.
// Pricing is fetched at construction time; an error means the collector cannot be used.
func New(ctx context.Context, config *Config, logger *slog.Logger, gcpClient client.Client) (*Collector, error) {
	if config.ProjectId == "" {
		return nil, fmt.Errorf("projectID cannot be empty")
	}

	logger = logger.With("collector", subsystem)

	familyFilter, err := regexp.Compile(config.FamilyFilter)
	if err != nil {
		return nil, fmt.Errorf("invalid vertex family filter %q: %w", config.FamilyFilter, err)
	}

	projects := strings.Split(config.Projects, ",")
	if len(projects) == 1 && projects[0] == "" {
		logger.LogAttrs(ctx, slog.LevelInfo, "no vertex projects specified, defaulting to project", slog.String("projectId", config.ProjectId))
		projects = []string{config.ProjectId}
	}

	pm, err := NewPricingMap(ctx, logger, gcpClient, familyFilter)
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
		pricingMap: pm,
		logger:     logger,
		projects:   projects,
	}, nil
}

// Register implements provider.Collector.
func (c *Collector) Register(_ provider.Registry) error {
	return nil
}

// Describe implements provider.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- vertexTokenCostDesc
	ch <- vertexCharacterCostDesc
	ch <- vertexComputeCostDesc
	ch <- vertexRerankCostDesc
	return nil
}

// Name implements provider.Collector.
func (c *Collector) Name() string {
	return subsystem
}

// Collect emits Vertex AI pricing metrics. Prices are project-independent, so each series is
// emitted once per configured project under a project_id label.
func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	snapshot := c.pricingMap.Snapshot()
	for _, project := range c.projects {
		if err := ctx.Err(); err != nil {
			return err
		}

		emitTokenCost(ch, vertexTokenCostDesc, project, snapshot.tokenInput, tokenTypeInput)
		emitTokenCost(ch, vertexTokenCostDesc, project, snapshot.tokenOutput, tokenTypeOutput)
		emitTokenCost(ch, vertexCharacterCostDesc, project, snapshot.charInput, tokenTypeInput)
		emitTokenCost(ch, vertexCharacterCostDesc, project, snapshot.charOutput, tokenTypeOutput)

		for region, machines := range snapshot.compute {
			for machineType, useCases := range machines {
				for useCase, pricing := range useCases {
					if pricing.OnDemandPerHour > 0 {
						ch <- prometheus.MustNewConstMetric(vertexComputeCostDesc, prometheus.GaugeValue,
							pricing.OnDemandPerHour, project, machineType, useCase, region, "on_demand")
					}
					if pricing.SpotPerHour > 0 {
						ch <- prometheus.MustNewConstMetric(vertexComputeCostDesc, prometheus.GaugeValue,
							pricing.SpotPerHour, project, machineType, useCase, region, "spot")
					}
				}
			}
		}

		for region, models := range snapshot.reranking {
			for model, price := range models {
				ch <- prometheus.MustNewConstMetric(vertexRerankCostDesc, prometheus.GaugeValue,
					price, project, region, model, familyFromModelID(model), rerankPriceTier)
			}
		}
	}
	return ctx.Err()
}

// emitTokenCost emits every region/model/tier price in src under desc, tagged with the project and
// gen_ai_token_type. Shared by the token and character metrics, which have identical label shapes.
func emitTokenCost(ch chan<- prometheus.Metric, desc *prometheus.Desc, project string, src map[string]map[string]map[string]float64, tokenType string) {
	for region, models := range src {
		for model, tiers := range models {
			for tier, price := range tiers {
				ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue,
					price, project, region, model, familyFromModelID(model), tokenType, tier)
			}
		}
	}
}

// familyFromModelID derives the model provider family from a normalised model ID.
// Gemini, Gemma, and Discovery Engine reranking models are Google's. Model Garden
// 3rd-party models embed the model name inside a long prefix, so Contains is used
// throughout. Unknown prefixes return "unknown" rather than assuming a provider.
func familyFromModelID(model string) string {
	switch {
	case strings.HasPrefix(model, "gemini"):
		return "google"
	case strings.Contains(model, "gemma"):
		return "google" // Gemma is Google DeepMind's open model family
	case strings.HasPrefix(model, "semantic"):
		return "google" // Discovery Engine reranking is a Google service
	case strings.Contains(model, "translation"):
		return "google" // Cloud Translation models are a Google service
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
