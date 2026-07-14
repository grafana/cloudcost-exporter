package rds

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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
		[]string{"account_id", "region", "tier", "id", "arn_name"},
	)
)

type Collector struct {
	regions           []types.Region
	regionMap         map[string]client.Client
	scrapeInterval    time.Duration
	regionListTimeout time.Duration
	Client            client.Client
	pricingMap        *pricingMap
	accountID         string
	logger            *slog.Logger
}

type Config struct {
	Regions           []types.Region
	RegionMap         map[string]client.Client
	Client            client.Client
	ScrapeInterval    time.Duration
	RegionListTimeout time.Duration
	AccountID         string
}

const (
	serviceName = "RDS"
)

// New creates an rds collector. A RegionListTimeout of 0 leaves each region's
// DescribeDBInstances call bounded only by the shared collector context
// (-collector-interval); a positive value caps it per region so a slow or
// unreachable region fails fast instead of overrunning the scrape.
func New(_ context.Context, config *Config, logger *slog.Logger) (*Collector, error) {
	return &Collector{
		pricingMap:        newPricingMap(),
		regions:           config.Regions,
		regionMap:         config.RegionMap,
		scrapeInterval:    config.ScrapeInterval,
		regionListTimeout: config.RegionListTimeout,
		Client:            config.Client,
		accountID:         config.AccountID,
		logger:            logger.With("collector", serviceName),
	}, nil
}

func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	logger := c.logger
	var instances = []rdsTypes.DBInstance{}

	numOfRegions := len(c.regions)
	instanceCh := make(chan []rdsTypes.DBInstance, numOfRegions)
	errCh := make(chan error, numOfRegions)

	wg := sync.WaitGroup{}
	for _, region := range c.regions {
		regionName := *region.RegionName
		regionClient, ok := c.regionMap[regionName]
		if !ok {
			logger.Error("no client found for region", "region", regionName)
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.fetchInstancesData(ctx, regionClient, regionName, instanceCh); err != nil {
				errCh <- err
			}
		}()
	}

	go func() {
		wg.Wait()
		close(instanceCh)
		close(errCh)
	}()

	for is := range instanceCh {
		instances = append(instances, is...)
	}

	var fetchErrs []error
	for err := range errCh {
		fetchErrs = append(fetchErrs, err)
	}

	for _, instance := range instances {
		// we need to get the region from the availability zone as there is no field for region
		if instance.AvailabilityZone == nil {
			// sometimes the availability zone is empty, possibly when an RDS instance is introduced or being removed, skipping them for the time being
			logger.Warn("no availability zone found for RDS instance")
			continue
		}
		var az = *instance.AvailabilityZone
		var region = az[:len(az)-1]
		depOption := multiOrSingleAZ(*instance.MultiAZ)
		locationType := isOutpostsInstance(instance) // outposts locations have a different unit price
		createPricingKey := createPricingKey(region, *instance.DBInstanceClass, *instance.Engine, depOption, locationType)

		hourlyPrice, ok := c.pricingMap.Get(createPricingKey)

		if !ok {
			v, err := c.Client.GetRDSUnitData(ctx, *instance.DBInstanceClass, region, depOption, *instance.Engine, locationType)
			if err != nil {
				logger.Error("error listing rds prices", "error", err)
				return err
			}
			if v == "" {
				logger.Warn("no pricing data found for RDS instance, skipping", "instanceType", *instance.DBInstanceClass, "region", region, "engine", *instance.Engine)
				continue
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
			c.accountID,
			region,
			*instance.DBInstanceClass,
			*instance.DbiResourceId,
			*instance.DBInstanceArn,
		)
	}

	return errors.Join(fetchErrs...)
}

func (c *Collector) fetchInstancesData(ctx context.Context, regionClient client.Client, region string, instanceCh chan []rdsTypes.DBInstance) error {
	// A positive regionListTimeout caps this region's call; 0 leaves it bounded
	// only by the parent collector context (backwards-compatible default).
	if c.regionListTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.regionListTimeout)
		defer cancel()
	}

	is, err := regionClient.ListRDSInstances(ctx)
	if err != nil {
		c.logger.Error("error listing RDS instances", "region", region, "error", err)
		return err
	}
	instanceCh <- is
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

func (c *Collector) Regions() []string {
	return utils.RegionsFromMap(c.regionMap)
}

func (c *Collector) Register(registry provider.Registry) error {
	return nil
}
