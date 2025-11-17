package gcs

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"github.com/grafana/cloudcost-exporter/pkg/provider"

	"github.com/prometheus/client_golang/prometheus"
)

// This data was pulled from https://console.cloud.google.com/billing/01330B-0FCEED-DEADF1/pricing?organizationId=803894190427&project=grafanalabs-global on 2023-07-28
// @pokom purposefully left out three discounts that don't fit:
// 1. Region Standard Tagging Class A Operations
// 2. Region Standard Tagging Class B Operations
// 3. Duplicated Regional Standard Class B Operations
// Filter on `Service Description: storage` and `Sku Description: operations`
// TODO: Pull this data directly from BigQuery
var operationsDiscountMap = map[string]map[string]map[string]float64{
	"region": {
		"archive": {
			"class-a": 0.190,
			"class-b": 0.190,
		},
		"coldline": {
			"class-a": 0.595,
			"class-b": 0.190,
		},
		"nearline": {
			"class-a": 0.190,
			"class-b": 0.190,
		},
		"standard": {
			"class-a": 0.190,
			"class-b": 0.190,
		},
		"regional": {
			"class-a": 0.190,
			"class-b": 0.190,
		},
	},
	"multi-region": {
		"coldline": {
			"class-a": 0.795,
			"class-b": 0.190,
		},
		"nearline": {
			"class-a": 0.595,
			"class-b": 0.190,
		},
		"standard": {
			"class-a": 0.595,
			"class-b": 0.190,
		},
		"multi_regional": {
			"class-a": 0.595,
			"class-b": 0.190,
		},
	},
	"dual-region": {
		"standard": {
			"class-a": 0.595,
			"class-b": 0.190,
		},
		"multi_regional": {
			"class-a": 0.595,
			"class-b": 0.190,
		},
	},
}

const (
	collectorName = "GCS"
	gibMonthly    = "gibibyte month"
	gibDay        = "gibibyte day"
)

type Collector struct {
	Projects   []string
	interval   time.Duration
	nextScrape time.Time
	metrics    *metrics.Metrics
	gcpClient  client.Client
}

func (c *Collector) Describe(_ chan<- *prometheus.Desc) error {
	return nil
}

func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	return c.collectMetrics(ctx, ch)
}

type Config struct {
	ProjectId      string
	Projects       string
	ScrapeInterval time.Duration
}

func New(config *Config, gcpClient client.Client) (*Collector, error) {
	if config.ProjectId == "" {
		return nil, fmt.Errorf("projectID cannot be empty")
	}

	projects := strings.Split(config.Projects, ",")
	if len(projects) == 1 && projects[0] == "" {
		log.Printf("No bucket projects specified, defaulting to %s", config.ProjectId)
		projects = []string{config.ProjectId}
	}

	return &Collector{
		Projects: projects,
		interval: config.ScrapeInterval,
		// Set nextScrape to the current time minus the scrape interval so that the first scrape will run immediately
		nextScrape: time.Now().Add(-config.ScrapeInterval),
		metrics:    metrics.NewMetrics(),
		gcpClient:  gcpClient,
	}, nil
}

func (c *Collector) Name() string {
	return collectorName
}

// Register is called when the collector is created and is responsible for registering the metrics with the registry
func (c *Collector) Register(registry provider.Registry) error {
	log.Printf("Registering GCS metrics")
	registry.MustRegister(c.metrics.StorageGauge)
	registry.MustRegister(c.metrics.StorageDiscountGauge)
	registry.MustRegister(c.metrics.OperationsDiscountGauge)
	registry.MustRegister(c.metrics.OperationsGauge)
	registry.MustRegister(c.metrics.BucketInfo)
	registry.MustRegister(c.metrics.BucketListHistogram)
	registry.MustRegister(c.metrics.BucketListStatus)
	registry.MustRegister(c.metrics.NextScrapeGauge)
	return nil
}

// CollectMetrics is by `c.Collect` and can likely be refactored directly into `c.Collect`
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	if err := c.collectMetrics(context.Background(), ch); err != nil {
		return 0
	}
	return 1
}

// collectMetrics performs the actual collection work
func (c *Collector) collectMetrics(ctx context.Context, ch chan<- prometheus.Metric) error {
	log.Printf("Collecting GCS metrics")
	now := time.Now()

	// If the nextScrape time is in the future, return nil and do not scrape
	// Billing API calls are free in GCP, just use this logic so metrics are similar to AWS
	if c.nextScrape.After(now) {
		// TODO: We should stuff in logic here to update pricing data if it's been more than 24 hours
		return nil
	}
	c.nextScrape = time.Now().Add(c.interval)
	c.metrics.NextScrapeGauge.Set(float64(c.nextScrape.Unix()))
	exporterOperationsDiscounts(c.metrics)
	if err := c.gcpClient.ExportRegionalDiscounts(ctx, c.metrics); err != nil {
		log.Printf("Error exporting regional discounts: %v", err)
	}

	if err := c.gcpClient.ExportBucketInfo(ctx, c.Projects, c.metrics); err != nil {
		log.Printf("Error exporting bucket info: %v", err)
	}

	serviceName, err := c.gcpClient.GetServiceName(ctx, "Cloud Storage")
	if err != nil {
		log.Printf("Error getting service name: %v", err)
		return err
	}
	c.gcpClient.ExportGCPCostData(ctx, serviceName, c.metrics)
	return nil
}

func exporterOperationsDiscounts(m *metrics.Metrics) {
	for locationType, locationMap := range operationsDiscountMap {
		for storageClass, storageClassmap := range locationMap {
			for opsClass, discount := range storageClassmap {
				m.OperationsDiscountGauge.WithLabelValues(locationType, strings.ToUpper(storageClass), opsClass).Set(discount)
			}
		}
	}
}
