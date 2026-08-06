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
		[]string{"account_id", "region", "tier", "id", "arn_name"},
	)
)

// Collector is a prometheus collector that collects metrics from AWS RDS clusters.
type Collector struct {
	regions        []types.Region
	regionMap      map[string]client.Client
	pricingMap     *pricingMap
	store          *instanceStore
	accountID      string
	populateErrors *prometheus.CounterVec
	logger         *slog.Logger
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

// New creates an rds collector.
//
// Instance inventory and pricing are refreshed in the background on a ticker
// rather than on the scrape path (see background-store-pattern.md). New()
// kicks off the first populate immediately via the store constructor and
// returns without blocking, so a slow AWS API never delays startup. Collect()
// makes zero AWS calls and serves metrics from the warm store and pricing map.
// A positive RegionListTimeout caps each region's background work; 0 falls back
// to an internal safety ceiling so a slow region never wedges the refresh.
func New(ctx context.Context, config *Config, logger *slog.Logger) (*Collector, error) {
	logger = logger.With("collector", serviceName)
	pm := newPricingMap()
	populateErrors := newPopulateErrorsCounter()
	store := newInstanceStore(ctx, logger, config, pm, populateErrors)

	startRefreshTicker(ctx, config.ScrapeInterval, func() { store.Populate(ctx) })

	return &Collector{
		regions:        config.Regions,
		regionMap:      config.RegionMap,
		pricingMap:     pm,
		store:          store,
		accountID:      config.AccountID,
		populateErrors: populateErrors,
		logger:         logger,
	}, nil
}

// Collect satisfies the provider.Collector interface. It makes no AWS calls:
// instances come from the background store and prices from the pre-warmed
// pricing map. A cold store (no populate finished yet) or a pricing miss emits
// nothing for the affected instances and logs, rather than failing the scrape.
func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	select {
	case <-c.store.Done():
	default:
		c.logger.LogAttrs(ctx, slog.LevelInfo, "instance store not yet populated, skipping metrics")
		return nil
	}

	for _, region := range c.regions {
		for _, instance := range c.store.Get(*region.RegionName) {
			// AWS can return partial instances (missing AZ, class, engine, or
			// multi-AZ) mid create or delete; skip rather than deref a nil.
			key, azRegion, ok := pricingKeyFor(instance)
			if !ok {
				c.logger.Warn("incomplete RDS instance, skipping")
				continue
			}
			if instance.DbiResourceId == nil || instance.DBInstanceArn == nil {
				c.logger.Warn("RDS instance missing identifiers, skipping", "region", azRegion)
				continue
			}

			hourlyPrice, ok := c.pricingMap.Get(key)
			if !ok {
				c.logger.Warn("no pricing data found for RDS instance, skipping", "instanceType", *instance.DBInstanceClass, "region", azRegion, "engine", *instance.Engine)
				continue
			}

			ch <- prometheus.MustNewConstMetric(
				HourlyGaugeDesc,
				prometheus.GaugeValue,
				hourlyPrice,
				c.accountID,
				azRegion,
				*instance.DBInstanceClass,
				*instance.DbiResourceId,
				*instance.DBInstanceArn,
			)
		}
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

// pricingKeyFor derives the pricing-map key and the region (from the instance's
// availability zone) for an instance. It returns ok=false when a field the key
// depends on is missing, which AWS can return while an instance is mid create
// or delete. Callers must skip such instances rather than dereference the nils.
func pricingKeyFor(instance rdsTypes.DBInstance) (key, region string, ok bool) {
	if instance.AvailabilityZone == nil || instance.DBInstanceClass == nil || instance.Engine == nil || instance.MultiAZ == nil {
		return "", "", false
	}
	az := *instance.AvailabilityZone
	if az == "" {
		return "", "", false
	}
	region = az[:len(az)-1]
	depOption := multiOrSingleAZ(*instance.MultiAZ)
	locationType := isOutpostsInstance(instance) // outposts locations have a different unit price
	return createPricingKey(region, *instance.DBInstanceClass, *instance.Engine, depOption, locationType), region, true
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
	registry.MustRegister(c.populateErrors)
	return nil
}
