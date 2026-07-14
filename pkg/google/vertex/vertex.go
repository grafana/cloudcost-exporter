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
	// Prompt-cache read/write apply to Claude-on-Vertex SKUs only.
	tokenTypeInput      = "input"
	tokenTypeOutput     = "output"
	tokenTypeCacheRead  = "cache_read"
	tokenTypeCacheWrite = "cache_write"

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
	vertexRerankCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix, subsystem, utils.SearchUnitCostSuffix,
		"Vertex AI reranking cost in USD per 1k ranking requests.",
		[]string{"project_id", "region", "gen_ai_request_model", "family", "price_tier"},
	)
)

// Config configures the Vertex AI collector.
type Config struct {
	ProjectId    string // ProjectId is the auth project, emitted as the single project_id label (mirrors Bedrock's account_id).
	FamilyFilter string // FamilyFilter is a regex matched against the family label; only matching families are emitted. Mirrors Bedrock's --aws.bedrock.families.
}

// Collector collects Vertex AI pricing metrics.
type Collector struct {
	pricingMap *PricingMap
	logger     *slog.Logger
	// projectID is the single project_id label value stamped on every series. Vertex pricing is
	// project-independent, so it carries one billing-scope project like Bedrock's single account_id
	// rather than duplicating every price across a list of projects.
	projectID string
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

	// Claude-on-Vertex prices are account-scoped (not in the public catalog). Resolve the billing
	// account from the auth project so it needs no configuration, the way the AWS collectors derive
	// the account from STS. Best-effort: if it cannot be resolved, Claude is left unpriced.
	billingAccount, err := gcpClient.GetProjectBillingAccount(ctx, config.ProjectId)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelWarn, "could not resolve billing account; Claude-on-Vertex pricing will be unavailable",
			slog.String("projectId", config.ProjectId), slog.String("error", err.Error()))
		billingAccount = ""
	}

	pm, err := NewPricingMap(ctx, logger, gcpClient, familyFilter, billingAccount)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize pricing map: %w", err)
	}

	go func() {
		// Layer in account-scoped Claude prices off the startup path: New returned with the catalog
		// already populated, so this only adds Claude (skipped when no billing account was resolved).
		if billingAccount != "" {
			if err := pm.Populate(ctx); err != nil {
				logger.Error("failed to load Claude-on-Vertex pricing", "error", err)
			}
		}
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
		projectID:  config.ProjectId,
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
	ch <- vertexRerankCostDesc
	return nil
}

// Name implements provider.Collector.
func (c *Collector) Name() string {
	return subsystem
}

// Collect emits Vertex AI pricing metrics. Prices are project-independent, so every series carries
// the single project_id (the auth project), mirroring the single account_id on the Bedrock metrics.
func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	snapshot := c.pricingMap.Snapshot()
	project := c.projectID

	emitTokenCost(ch, vertexTokenCostDesc, project, snapshot.tokenInput, tokenTypeInput)
	emitTokenCost(ch, vertexTokenCostDesc, project, snapshot.tokenOutput, tokenTypeOutput)
	emitTokenCost(ch, vertexTokenCostDesc, project, snapshot.tokenCacheRead, tokenTypeCacheRead)
	emitTokenCost(ch, vertexTokenCostDesc, project, snapshot.tokenCacheWrite, tokenTypeCacheWrite)
	emitTokenCost(ch, vertexCharacterCostDesc, project, snapshot.charInput, tokenTypeInput)
	emitTokenCost(ch, vertexCharacterCostDesc, project, snapshot.charOutput, tokenTypeOutput)

	for region, models := range snapshot.reranking {
		for model, price := range models {
			ch <- prometheus.MustNewConstMetric(vertexRerankCostDesc, prometheus.GaugeValue,
				price, project, region, model, familyFromModelID(model), rerankPriceTier)
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
	case strings.HasPrefix(model, "claude"):
		return "anthropic" // Claude-on-Vertex, priced from the account-scoped billing API
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
