package s3

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/prometheus/client_golang/prometheus"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

// HoursInMonth is the average hours in a month, used to calculate the cost of storage
// If we wanted to be clever, we can get the number of hours in the current month
// 365.25 * 24 / 12 ~= 730.5
const (
	// This needs to line up with yace so we can properly join the data in PromQL
	StandardLabel = "StandardStorage"
	subsystem     = "aws_s3"
)

// Metrics exported by this collector.
type Metrics struct {
	// StorageGauge measures the cost of storage in $/GiB, per region and class.
	StorageGauge *prometheus.GaugeVec

	// OperationsGauge measures the cost of operations in $/1k requests
	OperationsGauge *prometheus.GaugeVec

	// NextScrapeGauge is a gauge that tracks the next time the exporter will scrape AWS billing data
	NextScrapeGauge prometheus.Gauge
}

// NewMetrics returns a new Metrics instance.
func NewMetrics() Metrics {
	return Metrics{
		StorageGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "storage_by_location_usd_per_gibyte_hour"),
			Help: "Storage cost of S3 objects by region, class, and tier. Cost represented in USD/(GiB*h)",
		},
			[]string{"account_id", "region", "class"},
		),

		OperationsGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "operation_by_location_usd_per_krequest"),
			Help: "Operation cost of S3 objects by region, class, and tier. Cost represented in USD/(1k req)",
		},
			[]string{"account_id", "region", "class", "tier"},
		),

		NextScrapeGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "next_scrape"),
			Help: "The next time the exporter will scrape AWS billing data. Can be used to trigger alerts if now - nextScrape > interval",
		}),
	}
}

// Collector is the AWS implementation of the Collector interface
// It is responsible for registering and collecting metrics
type Collector struct {
	client      client.Client
	regions     []string
	interval    time.Duration
	nextScrape  time.Time
	metrics     Metrics
	billingData *client.BillingData
	m           sync.RWMutex
	accountID   string
}

// Describe is used to register the metrics with the Prometheus client
func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	return nil
}

// Collect is the function that will be called by the Prometheus client anytime a scrape is performed.
func (c *Collector) Collect(ctx context.Context, _ chan<- prometheus.Metric) error {
	c.m.Lock()
	defer c.m.Unlock()
	now := time.Now()
	if c.billingData == nil || now.After(c.nextScrape) {
		endDate := time.Now().AddDate(0, 0, -1)
		startDate := endDate.AddDate(0, 0, -30)
		billingData, err := c.client.GetBillingData(ctx, startDate, endDate)
		if err != nil {
			slog.Error("Error getting billing data", "error", err)
			return err
		}
		c.billingData = billingData
		c.nextScrape = time.Now().Add(c.interval)
		c.metrics.NextScrapeGauge.Set(float64(c.nextScrape.Unix()))
	}
	exportMetrics(c.billingData, c.metrics, c.accountID)
	return nil
}

// New creates a new Collector with a client and scrape interval defined.
func New(ctx context.Context, scrapeInterval time.Duration, client client.Client, accountID string) (*Collector, error) {
	awsRegions, err := client.DescribeRegions(ctx, false)
	if err != nil {
		slog.Warn("failed to describe regions for S3 collector", "error", err)
	}
	regions := make([]string, 0, len(awsRegions))
	for _, r := range awsRegions {
		if r.RegionName != nil {
			regions = append(regions, *r.RegionName)
		}
	}
	return &Collector{
		client:   client,
		regions:  regions,
		interval: scrapeInterval,
		// Initially Set nextScrape to the current time minus the scrape interval so that the first scrape will run immediately
		nextScrape: time.Now().Add(-scrapeInterval),
		metrics:    NewMetrics(),
		m:          sync.RWMutex{},
		accountID:  accountID,
	}, nil
}

func (c *Collector) Regions() []string {
	return c.regions
}

func (c *Collector) Name() string {
	return "S3"
}

// Register is called prior to the first collection. It registers any custom metric that needs to be exported for AWS billing data
func (c *Collector) Register(registry provider.Registry) error {
	registry.MustRegister(c.metrics.StorageGauge)
	registry.MustRegister(c.metrics.OperationsGauge)
	registry.MustRegister(c.metrics.NextScrapeGauge)
	registry.MustRegister(c.client.Metrics()...)

	return nil
}

// exportMetrics will iterate over the S3BillingData and export the metrics to prometheus
func exportMetrics(s3BillingData *client.BillingData, m Metrics, accountID string) {
	slog.Info("Exporting metrics", "regions", len(s3BillingData.Regions))
	for region, pricingModel := range s3BillingData.Regions {
		for component, pricing := range pricingModel.Model {
			switch component {
			case "Requests-Tier1":
				m.OperationsGauge.WithLabelValues(accountID, region, StandardLabel, "1").Set(pricing.UnitCost)
			case "Requests-Tier2":
				m.OperationsGauge.WithLabelValues(accountID, region, StandardLabel, "2").Set(pricing.UnitCost)
			case "TimedStorage":
				m.StorageGauge.WithLabelValues(accountID, region, StandardLabel).Set(pricing.UnitCost)
			}
		}
	}
}
