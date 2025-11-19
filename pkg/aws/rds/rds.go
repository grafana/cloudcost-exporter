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

var (
	HourlyGaugeDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"hourly_rate_usd_per_hour",
		"Hourly cost of AWS RDS instances by region, tier and id. Cost represented in USD/hour",
		[]string{"region", "tier", "id", "arn_name"},
	)
)

// Collector is a prometheus collector that collects metrics from AWS RDS clusters.
type Collector struct {
	regions        []types.Region
	regionMap      map[string]client.Client
	scrapeInterval time.Duration
	Client         client.Client
	pricingMap     *pricingMap
}

type Config struct {
	Regions        []types.Region
	RegionMap      map[string]client.Client
	Client         client.Client
	ScrapeInterval time.Duration
}

const (
	serviceName = "rds"
)

// New creates an rds collector
func New(config *Config) *Collector {
	return &Collector{
		pricingMap:     newPricingMap(),
		regions:        config.Regions,
		regionMap:      config.RegionMap,
		scrapeInterval: config.ScrapeInterval,
		Client:         config.Client,
	}
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	logger := slog.With("logger", serviceName)
	var instances = []rdsTypes.DBInstance{}
	for _, region := range c.regions {
		regionName := *region.RegionName
		regionClient, ok := c.regionMap[regionName]
		if !ok {
			logger.Error("no client found for region", "region", regionName)
			continue
		}

		is, err := regionClient.ListRDSInstances(ctx)
		if err != nil {
			logger.Error("error listing RDS instances", "region", regionName, "error", err)
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
		createPricingKey := createPricingKey(region, *instance.DBInstanceClass, *instance.Engine, depOption, locationType)

		hourlyPrice, ok := c.pricingMap.Get(createPricingKey)

		if !ok {
			// Compute price without holding the lock
			v, err := c.Client.GetRDSUnitData(ctx, *instance.DBInstanceClass, region, depOption, *instance.Engine, locationType)
			if err != nil {
				logger.Error("error listing rds prices", "error", err)
				return err
			}
			validatedPrice, err := validateRDSPriceData(ctx, v)
			if err != nil {
				logger.Error("error validating RDS price data", "error", err)
				return err
			}
			c.pricingMap.Set(createPricingKey, validatedPrice)
			hourlyPrice = validatedPrice
		}

		ch <- prometheus.MustNewConstMetric(
			HourlyGaugeDesc,
			prometheus.GaugeValue,
			hourlyPrice,
			region,
			*instance.DBInstanceClass,
			*instance.DbiResourceId,
			*instance.DBInstanceArn,
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

func createPricingKey(region, tier, engine, depOption, locationType string) string {
	return fmt.Sprintf("%s-%s-%s-%s-%s", region, tier, engine, depOption, locationType)
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
