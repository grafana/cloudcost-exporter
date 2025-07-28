package rds

import (
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/rds"
	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	pricingClient "github.com/grafana/cloudcost-exporter/pkg/aws/services/pricing"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	subsystem = "aws_rds"
)

type Metrics struct {
	// StorageGauge measures the cost of storage in $/GiB, per region and class.
	DBCost *prometheus.GaugeVec

	// OperationsGauge measures the cost of operations in $/1k requests
	DBOperationsCost *prometheus.GaugeVec

	// RequestCount is a counter that tracks the number of requests made to the AWS Cost Explorer API
	RequestCount prometheus.Counter

	// RequestErrorsCount is a counter that tracks the number of errors when making requests to the AWS Cost Explorer API
	RequestErrorsCount prometheus.Counter

	// NextScrapeGauge is a gauge that tracks the next time the exporter will scrape AWS billing data
	NextScrape prometheus.Gauge
}

func NewMetrics() Metrics {
	return Metrics{
		DBCost: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "storage_by_location_usd_per_gibyte_hour"),
			Help: "Storage cost of RDS databases by region, class, and tier. Cost represented in USD/(GiB*h)",
		},
			[]string{"region", "class"},
		),

		DBOperationsCost: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "operation_by_location_usd_per_krequest"),
			Help: "Operation cost of DB instances by region, class, and tier. Cost represented in USD/(1k req)",
		},
			[]string{"region", "class", "tier"},
		),

		RequestCount: prometheus.NewCounter(prometheus.CounterOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "cost_api_requests_total"),
			Help: "Total number of requests made to the AWS Cost Explorer API",
		}),

		RequestErrorsCount: prometheus.NewCounter(prometheus.CounterOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "cost_api_requests_errors_total"),
			Help: "Total number of errors when making requests to the AWS Cost Explorer API",
		}),

		NextScrape: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "next_scrape"),
			Help: "The next time the exporter will scrape AWS billing data. Can be used to trigger alerts if now - nextScrape > interval",
		}),
	}
}

// Collector is a prometheus collector that collects metrics from AWS RDS clusters.
type Collector struct {
	Regions           []string
	ScrapeInterval    time.Duration
	pricingService    pricingClient.Pricing
	NextComputeScrape time.Time
	NextStorageScrape time.Time
	rdsRegionClients  map[string]rds.Client
	logger            *slog.Logger
}

type Config struct {
	ScrapeInterval time.Duration
	RegionClients  map[string]rds.Client
	Logger         *slog.Logger
}

// New creates an rds collector
func New(config *Config, ps pricingClient.Pricing) *Collector {
	logger := config.Logger.With("logger", "rds")
	return &Collector{
		ScrapeInterval:   config.ScrapeInterval,
		rdsRegionClients: config.RegionClients,
		logger:           logger,
		pricingService:   ps,
	}
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	return nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}
