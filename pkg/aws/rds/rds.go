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
	instanceStore  *instanceStore
	pricingStore   *pricingStore
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
// Instance inventory and pricing are refreshed independently in the
// background, each on its own ticker, rather than on the scrape path. New()
// kicks off both stores' first populate immediately and returns without
// blocking, so a slow AWS API never delays startup. Collect() makes zero AWS
// calls and serves metrics from the two warm stores, joined by pricing key.
// A positive RegionListTimeout caps each region's background work for both
// stores; 0 falls back to an internal safety ceiling so a slow region never
// wedges either refresh.
func New(ctx context.Context, config *Config, logger *slog.Logger) (*Collector, error) {
	logger = logger.With("collector", serviceName)
	populateErrors := newPopulateErrorsCounter()

	instanceStore := newInstanceStore(ctx, logger, config, populateErrors)
	pricingStore := newPricingStore(ctx, logger, config, populateErrors)

	startRefreshTicker(ctx, config.ScrapeInterval, func() { instanceStore.Populate(ctx) })
	startRefreshTicker(ctx, defaultPricingRefreshInterval, func() { pricingStore.Populate(ctx) })

	return &Collector{
		regions:        config.Regions,
		regionMap:      config.RegionMap,
		instanceStore:  instanceStore,
		pricingStore:   pricingStore,
		accountID:      config.AccountID,
		populateErrors: populateErrors,
		logger:         logger,
	}, nil
}

// isDone reports whether ch has been closed.
func isDone(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// Collect satisfies the provider.Collector interface. It makes no AWS calls:
// instances come from the background instance store and prices from the
// background pricing store. Either store not yet populated, or any
// combination of the two, skips the scrape (no metrics, no error) rather than
// failing it. A pricing miss for an individual instance is likewise skipped
// with a log, not an error.
func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	instancesReady := isDone(c.instanceStore.Done())
	pricesReady := isDone(c.pricingStore.Done())
	switch {
	case !instancesReady && !pricesReady:
		c.logger.LogAttrs(ctx, slog.LevelInfo, "instance and pricing stores not yet populated, skipping metrics")
		return nil
	case !instancesReady:
		c.logger.LogAttrs(ctx, slog.LevelInfo, "instance store not yet populated, skipping metrics")
		return nil
	case !pricesReady:
		c.logger.LogAttrs(ctx, slog.LevelInfo, "pricing store not yet populated, skipping metrics")
		return nil
	}

	for _, region := range c.regions {
		for _, instance := range c.instanceStore.Get(*region.RegionName) {
			// pricingKeyFor returns ok=false for a partial instance (AWS can
			// return one missing its AZ, class, engine, or multi-AZ flag mid
			// create or delete) or an engine we cannot map to a price; skip
			// either rather than deref a nil.
			key, azRegion, ok := pricingKeyFor(instance)
			if !ok {
				c.logger.Warn("cannot derive pricing key for RDS instance, skipping")
				continue
			}
			if instance.DbiResourceId == nil || instance.DBInstanceArn == nil {
				c.logger.Warn("RDS instance missing identifiers, skipping", "region", azRegion)
				continue
			}

			hourlyPrice, ok := c.pricingStore.Get(key)
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

// engineAttributes maps an RDS instance Engine code to the AmazonRDS pricing
// databaseEngine and databaseEdition attribute values that key its price. The
// instance Engine field ("postgres", "oracle-ee") differs from the Pricing
// API's display names ("PostgreSQL", "Oracle" plus edition "Enterprise"), so we
// translate explicitly. Engines absent from this map cannot be priced and are
// skipped by pricingKeyFor.
//
// VERIFY: the open-source rows (MySQL/MariaDB/PostgreSQL/Aurora) are confirmed,
// but the Oracle and SQL Server databaseEdition strings are best-effort and
// should be checked against a real GetProducts response before relying on their
// prices.
//
// TODO: harden Aurora and multi-edition engine matching against a real
// GetProducts response (e.g. Aurora Serverless v2 may need a distinct key
// from provisioned Aurora); tracked as a follow-up issue.
var engineAttributes = map[string]struct {
	databaseEngine  string
	databaseEdition string
}{
	"mysql":             {databaseEngine: "MySQL"},
	"mariadb":           {databaseEngine: "MariaDB"},
	"postgres":          {databaseEngine: "PostgreSQL"},
	"aurora-mysql":      {databaseEngine: "Aurora MySQL"},
	"aurora-postgresql": {databaseEngine: "Aurora PostgreSQL"},
	"oracle-ee":         {databaseEngine: "Oracle", databaseEdition: "Enterprise"},
	"oracle-ee-cdb":     {databaseEngine: "Oracle", databaseEdition: "Enterprise"},
	"oracle-se2":        {databaseEngine: "Oracle", databaseEdition: "Standard Two"},
	"oracle-se2-cdb":    {databaseEngine: "Oracle", databaseEdition: "Standard Two"},
	"sqlserver-ee":      {databaseEngine: "SQL Server", databaseEdition: "Enterprise"},
	"sqlserver-se":      {databaseEngine: "SQL Server", databaseEdition: "Standard"},
	"sqlserver-web":     {databaseEngine: "SQL Server", databaseEdition: "Web"},
	"sqlserver-ex":      {databaseEngine: "SQL Server", databaseEdition: "Express"},
}

// openSourceLicense is the pricing licenseModel attribute shared by every
// open-source RDS engine, so those engines never depend on the instance's
// LicenseModel field.
const openSourceLicense = "No license required"

// licenseModelAttributes maps an RDS instance LicenseModel to the pricing
// licenseModel attribute value. Only the licensed engines (Oracle, SQL Server)
// consult this map; see pricingKeyFor.
//
// VERIFY: the "License included" / "Bring your own license" strings are
// best-effort and should be checked against a real GetProducts response.
var licenseModelAttributes = map[string]string{
	"license-included":       "License included",
	"bring-your-own-license": "Bring your own license",
}

func createPricingKey(region, instanceType, databaseEngine, databaseEdition, depOption, licenseModel, locationType string) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s", region, instanceType, databaseEngine, databaseEdition, depOption, licenseModel, locationType)
}

// pricingKeyFor derives the pricing-map key and the region (from the instance's
// availability zone) for an instance. It returns ok=false when a field the key
// depends on is missing (AWS can return partial instances mid create or delete)
// or the engine is not in engineAttributes. Callers must skip such instances
// rather than dereference the nils.
func pricingKeyFor(instance rdsTypes.DBInstance) (key, region string, ok bool) {
	if instance.AvailabilityZone == nil || instance.DBInstanceClass == nil || instance.Engine == nil || instance.MultiAZ == nil {
		return "", "", false
	}
	az := *instance.AvailabilityZone
	if az == "" {
		return "", "", false
	}
	region = az[:len(az)-1]

	engine, ok := engineAttributes[*instance.Engine]
	if !ok {
		return "", region, false
	}

	// Open-source engines all price under a single license; only the licensed
	// engines (Oracle, SQL Server) key on the instance's LicenseModel.
	license := openSourceLicense
	if engine.databaseEdition != "" {
		lm := ""
		if instance.LicenseModel != nil {
			lm = *instance.LicenseModel
		}
		license, ok = licenseModelAttributes[lm]
		if !ok {
			return "", region, false
		}
	}

	depOption := multiOrSingleAZ(*instance.MultiAZ)
	locationType := isOutpostsInstance(instance) // outposts locations have a different unit price
	return createPricingKey(region, *instance.DBInstanceClass, engine.databaseEngine, engine.databaseEdition, depOption, license, locationType), region, true
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
