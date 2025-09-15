package rds

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	subsystem = "aws_rds"
)

type Metrics struct {
	// HourlyCost measures the hourly cost of RDS databases in $/h, per region and class.
	HourlyCost *prometheus.GaugeVec

	// RequestCount is a counter that tracks the number of requests made to the AWS Cost Explorer API
	RequestCount prometheus.Counter

	// RequestErrorsCount is a counter that tracks the number of errors when making requests to the AWS Cost Explorer API
	RequestErrorsCount prometheus.Counter

	// NextScrapeGauge is a gauge that tracks the next time the exporter will scrape AWS billing data
	NextScrape prometheus.Gauge
}

func NewMetrics() Metrics {
	return Metrics{
		HourlyCost: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "db_by_location_usd_per_hour"),
			Help: "Hourly cost of RDS databases by region, class, and tier. Cost represented in USD/(h)",
		},
			[]string{"region", "tier", "az", "engine", "location_type"},
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
	regions           []types.Region
	regionMap         map[string]client.Client
	scrapeInterval    time.Duration
	NextComputeScrape time.Time
	NextStorageScrape time.Time
	logger            *slog.Logger
	Client            client.Client
	pricingMap        map[string]float64
	metrics           Metrics
}

type Config struct {
	Regions        []types.Region
	RegionMap      map[string]client.Client
	Client         client.Client
	ScrapeInterval time.Duration
	Logger         *slog.Logger
}

const (
	serviceName = "rds"
)

// New creates an rds collector
func New(ctx context.Context, config *Config) *Collector {
	return &Collector{
		pricingMap:     make(map[string]float64),
		regions:        config.Regions,
		regionMap:      config.RegionMap,
		scrapeInterval: config.ScrapeInterval,
		logger:         config.Logger.With("logger", serviceName),
		Client:         config.Client,
		metrics:        NewMetrics(),
	}
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	ctx := context.Background()
	var instances = []rdsTypes.DBInstance{}
	for _, region := range c.regions {
		regionName := *region.RegionName
		regionClient, ok := c.regionMap[regionName]
		if !ok {
			c.logger.Warn("no client found for region", "region", regionName)
			continue
		}

		is, err := regionClient.ListRDSInstances(ctx)
		if err != nil {
			c.logger.Error("error listing RDS instances", "region", regionName, "error", err)
			continue
		}

		instances = append(instances, is...)
	}

	for _, instance := range instances {
		// we need to get the region from the availability zone as there is no field for region
		var az = *instance.AvailabilityZone
		var region = az[:len(az)-1]
		depOption := multiOrSingleAZ(*instance.MultiAZ)
		locationType := isOutpostsInstance(instance) // outposts locations have a different unit price
		createPricingKey := createPricingKey(az, region, depOption, locationType)
		if _, ok := c.pricingMap[createPricingKey]; !ok {
			fmt.Println("new key", createPricingKey)
			v, err := c.Client.GetRDSUnitData(ctx, *instance.DBInstanceClass, region, depOption, *instance.Engine, locationType)
			if err != nil {
				c.logger.Error("error listing rds prices", "error", err)
				return err
			}
			hourlyPrice, err := validateRDSPriceData(ctx, v)
			fmt.Println("hourlyPrice", hourlyPrice)
			if err != nil {
				c.logger.Error("error validating RDS price data", "error", err)
				return err
			}
			c.pricingMap[createPricingKey] = hourlyPrice
			fmt.Println("pricingMap", c.pricingMap)
		}

		fmt.Printf("instance: %f \n", c.pricingMap[createPricingKey])

		ch <- prometheus.MustNewConstMetric(
			c.metrics.HourlyCost.WithLabelValues(region, depOption, az, *instance.Engine, locationType).Desc(),
			prometheus.GaugeValue,
			c.pricingMap[createPricingKey],
			region,
			depOption,
			az,
			*instance.Engine,
			locationType,
		)
	}
	return nil
}

func multiOrSingleAZ(multiAZ bool) string {
	// listInstances api returns true if the instance is in a multi-az deployment
	// but the pricing API expects a string
	if multiAZ {
		return "Multi-AZ"
	}
	return "Single-AZ"
}

func isOutpostsInstance(instance rdsTypes.DBInstance) string {
	if instance.DBSubnetGroup != nil {
		for _, subnet := range instance.DBSubnetGroup.Subnets {
			// If SubnetOutpost.Arn is not null, the subnet is on Outposts
			if subnet.SubnetOutpost != nil && subnet.SubnetOutpost.Arn != nil {
				return "AWS Outposts"
			}
		}
	}
	return "AWS Region"
}

func createPricingKey(az, region, depOption, locationType string) string {
	return fmt.Sprintf("%s-%s-%s-%s", az, region, depOption, locationType)
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Register(registry provider.Registry) error {
	registry.MustRegister(c.metrics.HourlyCost)
	// registry.MustRegister(c.metrics.RequestCount)
	// registry.MustRegister(c.metrics.RequestErrorsCount)
	// registry.MustRegister(c.metrics.NextScrape)
	return nil
}
