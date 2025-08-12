package natgateway

import (
	"context"
	"log"
	"strings"
	"time"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/prometheus/client_golang/prometheus"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"

	"github.com/grafana/cloudcost-exporter/pkg/google/common"
)

const subsystem = "gcp_natgateway"

// Metrics holds Prometheus metrics for the NAT Gateway collector
type Metrics struct {
	HourlyRateGauge     *prometheus.GaugeVec
	DataProcessingGauge *prometheus.GaugeVec
	NextScrapeGauge     prometheus.Gauge
}

func NewMetrics() *Metrics {
	return &Metrics{
		HourlyRateGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "hourly_rate_usd_per_hour"),
			Help: "Hourly cost of Cloud NAT Gateway by region and project. USD/hour",
		}, []string{"region", "project", "sku_id", "sku_name"}),
		DataProcessingGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "data_processing_usd_per_gb"),
			Help: "Data processing cost of Cloud NAT Gateway by region and project. USD/GB",
		}, []string{"region", "project", "sku_id", "sku_name"}),
		NextScrapeGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "next_scrape"),
			Help: "The next time the exporter will scrape GCP billing data for NAT Gateway",
		}),
	}
}

type Config struct {
	ProjectId      string
	Projects       string // comma-separated list
	ScrapeInterval time.Duration
}

type Collector struct {
	config             *Config
	cloudCatalogClient *billingv1.CloudCatalogClient
	ctx                context.Context
	nextScrape         time.Time
	metrics            *Metrics
}

func New(cfg *Config, cloudCatalogClient *billingv1.CloudCatalogClient) (*Collector, error) {
	ctx := context.Background()
	c := &Collector{
		config:             cfg,
		cloudCatalogClient: cloudCatalogClient,
		ctx:                ctx,
		nextScrape:         time.Now().Add(-cfg.ScrapeInterval),
		metrics:            NewMetrics(),
	}
	return c, nil
}

func (c *Collector) Name() string { return "NATGATEWAY" }

func (c *Collector) Register(registry provider.Registry) error {
	registry.MustRegister(c.metrics.HourlyRateGauge)
	registry.MustRegister(c.metrics.DataProcessingGauge)
	registry.MustRegister(c.metrics.NextScrapeGauge)
	return nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error { return nil }

func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	c.collect()
	return nil
}

// CollectMetrics exists for consistency with other collectors; returns 1 to indicate it ran.
func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	c.collect()
	return 1
}

func (c *Collector) collect() {
	now := time.Now()
	if c.nextScrape.After(now) {
		return
	}
	c.nextScrape = time.Now().Add(c.config.ScrapeInterval)
	c.metrics.NextScrapeGauge.Set(float64(c.nextScrape.Unix()))

	serviceName, err := common.GetServiceName(c.ctx, c.cloudCatalogClient, "Networking")
	if err != nil {
		log.Printf("gcp natgateway: failed to get Compute Engine service: %v", err)
		return
	}

	skus := common.GetPricing(c.ctx, c.cloudCatalogClient, serviceName)

	projects := common.ParseProjects(c.config.ProjectId, c.config.Projects)

	// Reset metrics for a fresh scrape
	c.metrics.HourlyRateGauge.Reset()
	c.metrics.DataProcessingGauge.Reset()

	for _, sku := range skus {
		if sku == nil {
			continue
		}
		desc := strings.ToLower(sku.Description)
		if !isCloudNATSKU(desc) {
			continue
		}

		priceUSD := firstTierPriceUSD(sku)
		if priceUSD == 0 {
			continue
		}

		// service regions are like: [us-central1, europe-west1, ...]
		for _, region := range sku.ServiceRegions {
			for _, project := range projects {
				skuID := sku.GetName()
				skuName := sku.Description
				if isDataProcessing(desc) {
					c.metrics.DataProcessingGauge.WithLabelValues(region, project, skuID, skuName).Set(priceUSD)
				} else {
					c.metrics.HourlyRateGauge.WithLabelValues(region, project, skuID, skuName).Set(priceUSD)
				}
			}
		}
	}
}

// isCloudNATSKU identifies Cloud NAT related SKUs heuristically via description
func isCloudNATSKU(desc string) bool {
	// Normalize to lowercase and look for common phrases in Cloud NAT SKUs
	desc = strings.ToLower(desc)
	return strings.Contains(desc, "cloud nat") || strings.Contains(desc, "nat gateway") || strings.Contains(desc, "cloud nat gateway")
}

func isDataProcessing(desc string) bool {
	desc = strings.ToLower(desc)
	return strings.Contains(desc, "data processed") || strings.Contains(desc, "data processing") || strings.Contains(desc, "egress data")
}

// firstTierPriceUSD extracts the first tier price as USD for a SKU pricing expression.
func firstTierPriceUSD(sku *billingpb.Sku) float64 {
	if sku == nil || sku.PricingInfo == nil || len(sku.PricingInfo) == 0 {
		return 0
	}
	pe := sku.PricingInfo[0].PricingExpression
	if pe == nil || len(pe.TieredRates) == 0 {
		return 0
	}
	tr := pe.TieredRates[0]
	if tr.UnitPrice == nil {
		return 0
	}
	units := float64(tr.UnitPrice.Units)
	nanos := float64(tr.UnitPrice.Nanos) / 1e9
	price := units + nanos
	// Attempt to normalize to per base unit (e.g., per GB or per hour)
	// If BaseUnitConversionFactor is set, use it to get per-base-unit price
	if pe.BaseUnitConversionFactor > 0 {
		price = price / pe.BaseUnitConversionFactor
	}
	return price
}
