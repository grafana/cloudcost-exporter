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
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	subsystem = "aws_rds"
)

// type Metrics struct {
// 	// HourlyCost measures the hourly cost of RDS databases in $/h, per region and class.
// 	HourlyCost *prometheus.GaugeVec

// 	// RequestCount is a counter that tracks the number of requests made to the AWS Cost Explorer API
// 	RequestCount prometheus.Counter

// 	// RequestErrorsCount is a counter that tracks the number of errors when making requests to the AWS Cost Explorer API
// 	RequestErrorsCount prometheus.Counter

// 	// NextScrapeGauge is a gauge that tracks the next time the exporter will scrape AWS billing data
// 	NextScrape prometheus.Gauge
// }

var (
	HourlyGaugeDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"hourly_rate_usd_per_hour",
		"Hourly cost of NAT Gateway by region. Cost represented in USD/hour",
		[]string{"region", "tier", "name"},
	)
	RequestCountDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"cost_api_requests_total",
		"Total number of requests made to the AWS Cost Explorer API",
		[]string{},
	)
	RequestErrorsCountDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"cost_api_requests_errors_total",
		"Total number of errors when making requests to the AWS Cost Explorer API",
		[]string{},
	)
	NextScrapeDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"next_scrape",
		"The next time the exporter will scrape AWS billing data. Can be used to trigger alerts if now - nextScrape > interval",
		[]string{},
	)
)

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
		createPricingKey := createPricingKey(az, *instance.DBInstanceClass, region, depOption, locationType)
		if _, ok := c.pricingMap[createPricingKey]; !ok {
			v, err := c.Client.GetRDSUnitData(ctx, *instance.DBInstanceClass, region, depOption, *instance.Engine, locationType)
			if err != nil {
				c.logger.Error("error listing rds prices", "error", err)
				return err
			}
			hourlyPrice, err := validateRDSPriceData(ctx, v)
			if err != nil {
				c.logger.Error("error validating RDS price data", "error", err)
				return err
			}
			c.pricingMap[createPricingKey] = hourlyPrice
		}

		ch <- prometheus.MustNewConstMetric(
			HourlyGaugeDesc,
			prometheus.GaugeValue,
			c.pricingMap[createPricingKey],
			region,
			*instance.DBInstanceClass,
			*instance.DBInstanceIdentifier,
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

func createPricingKey(az, tier, region, depOption, locationType string) string {
	return fmt.Sprintf("%s-%s-%s-%s-%s", az, tier, region, depOption, locationType)
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Register(registry provider.Registry) error {
	return nil
}
