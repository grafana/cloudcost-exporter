package s3

import (
	"context"
	"fmt"
	"log"
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
			[]string{"region", "class"},
		),

		OperationsGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "operation_by_location_usd_per_krequest"),
			Help: "Operation cost of S3 objects by region, class, and tier. Cost represented in USD/(1k req)",
		},
			[]string{"region", "class", "tier"},
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
	interval    time.Duration
	nextScrape  time.Time
	metrics     Metrics
	billingData *client.BillingData
	m           sync.RWMutex
}

// Describe is used to register the metrics with the Prometheus client
func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	return nil
}

// Collect is the function that will be called by the Prometheus client anytime a scrape is performed.
func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	up := c.CollectMetrics(ch)
	if up == 0 {
		return fmt.Errorf("error collecting metrics")
	}
	return nil
}

// New creates a new Collector with a client and scrape interval defined.
func New(scrapeInterval time.Duration, client client.Client) *Collector {
	return &Collector{
		client:   client,
		interval: scrapeInterval,
		// Initially Set nextScrape to the current time minus the scrape interval so that the first scrape will run immediately
		nextScrape: time.Now().Add(-scrapeInterval),
		metrics:    NewMetrics(),
		m:          sync.RWMutex{},
	}
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

// CollectMetrics is the function that will be called by the Prometheus client anytime a scrape is performed.
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	c.m.Lock()
	defer c.m.Unlock()
	now := time.Now()
	// :fire: Checking scrape interval is to _mitigate_ expensive API calls to the cost explorer API
	if c.billingData == nil || now.After(c.nextScrape) {
		endDate := time.Now().AddDate(0, 0, -1)
		// Current assumption is that we're going to pull 30 days worth of billing data
		startDate := endDate.AddDate(0, 0, -30)
		billingData, err := c.client.GetBillingData(context.Background(), startDate, endDate)
		if err != nil {
			log.Printf("Error getting billing data: %v\n", err)
			return 0
		}
		c.billingData = billingData
		c.nextScrape = time.Now().Add(c.interval)
		c.metrics.NextScrapeGauge.Set(float64(c.nextScrape.Unix()))
	}

	exportMetrics(c.billingData, c.metrics)
	return 1.0
}

// exportMetrics will iterate over the S3BillingData and export the metrics to prometheus
func exportMetrics(s3BillingData *client.BillingData, m Metrics) {
	log.Printf("Exporting metrics for %d regions\n", len(s3BillingData.Regions))
	for region, pricingModel := range s3BillingData.Regions {
		for component, pricing := range pricingModel.Model {
			switch component {
			case "Requests-Tier1":
				m.OperationsGauge.WithLabelValues(region, StandardLabel, "1").Set(pricing.UnitCost)
			case "Requests-Tier2":
				m.OperationsGauge.WithLabelValues(region, StandardLabel, "2").Set(pricing.UnitCost)
			case "TimedStorage":
				m.StorageGauge.WithLabelValues(region, StandardLabel).Set(pricing.UnitCost)
			}
		}
	}
}
