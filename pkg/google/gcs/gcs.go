package gcs

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"github.com/grafana/cloudcost-exporter/pkg/provider"

	"github.com/prometheus/client_golang/prometheus"
)

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
	regions    []string
	interval   time.Duration
	nextScrape time.Time
	metrics    *metrics.Metrics
	gcpClient  client.Client
	logger     *slog.Logger
}

func (c *Collector) Describe(_ chan<- *prometheus.Desc) error {
	return nil
}

func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	return c.collectMetrics(ctx)
}

type Config struct {
	ProjectId      string
	Projects       string
	ScrapeInterval time.Duration
	Logger         *slog.Logger
}

func New(ctx context.Context, config *Config, gcpClient client.Client) (*Collector, error) {
	if config.ProjectId == "" {
		return nil, fmt.Errorf("projectID cannot be empty")
	}

	logger := config.Logger.With("collector", collectorName)

	projects := strings.Split(config.Projects, ",")
	if len(projects) == 1 && projects[0] == "" {
		logger.LogAttrs(ctx, slog.LevelInfo, "no bucket projects specified, defaulting to project", slog.String("projectId", config.ProjectId))
		projects = []string{config.ProjectId}
	}

	regions := client.RegionsForProjects(gcpClient, projects, logger)

	return &Collector{
		Projects: projects,
		regions:  regions,
		interval: config.ScrapeInterval,
		// Set nextScrape to the current time minus the scrape interval so that the first scrape will run immediately
		nextScrape: time.Now().Add(-config.ScrapeInterval),
		metrics:    metrics.NewMetrics(),
		gcpClient:  gcpClient,
		logger:     logger,
	}, nil
}

func (c *Collector) Regions() []string {
	return c.regions
}

func (c *Collector) Name() string {
	return collectorName
}

// Register is called when the collector is created and is responsible for registering the metrics with the registry
func (c *Collector) Register(registry provider.Registry) error {
	c.logger.Info("Registering GCS metrics")
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

// collectMetrics performs the actual collection work
func (c *Collector) collectMetrics(ctx context.Context) error {
	c.logger.Info("Collecting GCS metrics")
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
		c.logger.LogAttrs(ctx, slog.LevelError, "Error exporting regional discounts", slog.Any("error", err))
	}

	if err := c.gcpClient.ExportBucketInfo(ctx, c.Projects, c.metrics); err != nil {
		c.logger.LogAttrs(ctx, slog.LevelError, "Error exporting bucket info", slog.Any("error", err))
	}

	serviceName, err := c.gcpClient.GetServiceName(ctx, "Cloud Storage")
	if err != nil {
		c.logger.LogAttrs(ctx, slog.LevelError, "Error getting service name", slog.Any("error", err))
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
