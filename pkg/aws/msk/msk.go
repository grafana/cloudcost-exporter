package msk

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	msktypes "github.com/aws/aws-sdk-go-v2/service/kafka/types"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/grafana/cloudcost-exporter/pkg/aws/pricingstore"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	subsystem   = "aws_msk"
	serviceName = "msk"

	mskBrokerUsageTypePrefix = "Kafka."
	mskStorageUsageType      = "Kafka.Storage.GP2"
)

var (
	ComputeHourlyGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"compute_hourly_rate_usd_per_hour",
		"Hourly cost of AWS MSK broker compute by cluster. Cost represented in USD/hour",
		[]string{"account_id", "region", "cluster_name", "cluster_arn", "instance_type"},
	)
	StorageHourlyGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"storage_hourly_rate_usd_per_hour",
		"Hourly cost of AWS MSK provisioned storage by cluster. Cost represented in USD/hour",
		[]string{"account_id", "region", "cluster_name", "cluster_arn"},
	)
	brokerPriceFilters = []pricingtypes.Filter{
		{
			Field: aws.String("operation"),
			Type:  pricingtypes.FilterTypeTermMatch,
			Value: aws.String("RunBroker"),
		},
		{
			Field: aws.String("group"),
			Type:  pricingtypes.FilterTypeTermMatch,
			Value: aws.String("Broker"),
		},
		{
			Field: aws.String("description"),
			Type:  pricingtypes.FilterTypeTermMatch,
			Value: aws.String("Broker-hours"),
		},
	}
	storagePriceFilters = []pricingtypes.Filter{
		{
			Field: aws.String("operation"),
			Type:  pricingtypes.FilterTypeTermMatch,
			Value: aws.String("RunVolume"),
		},
		{
			Field: aws.String("group"),
			Type:  pricingtypes.FilterTypeTermMatch,
			Value: aws.String("Storage"),
		},
		{
			Field: aws.String("storageFamily"),
			Type:  pricingtypes.FilterTypeTermMatch,
			Value: aws.String("GP2"),
		},
		{
			Field: aws.String("description"),
			Type:  pricingtypes.FilterTypeTermMatch,
			Value: aws.String("Storage-hours"),
		},
	}
)

type Collector struct {
	regions      []ec2types.Region
	regionMap    map[string]client.Client
	pricingStore pricingstore.PricingStoreRefresher
	logger       *slog.Logger
	accountID    string
}

type Config struct {
	Regions   []ec2types.Region
	RegionMap map[string]client.Client
	Client    client.Client
	Logger    *slog.Logger
	AccountID string
}

type clusterPricingData struct {
	clusterName   string
	clusterARN    string
	instanceType  string
	brokerCount   int32
	volumeSizeGiB int32
}

func New(ctx context.Context, config *Config) (*Collector, error) {
	logger := slog.Default()
	if config.Logger != nil {
		logger = config.Logger.With("logger", serviceName)
	}

	pricingStore, err := pricingstore.NewPricingStore(ctx, logger, config.Regions, newPriceFetcher(config.Client))
	if err != nil {
		return nil, fmt.Errorf("failed to create pricing store: %w", err)
	}

	go func(ctx context.Context) {
		priceTicker := time.NewTicker(pricingstore.PriceRefreshInterval)
		defer priceTicker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-priceTicker.C:
				logger.LogAttrs(ctx, slog.LevelInfo, "refreshing pricing map")
				if err := pricingStore.PopulatePricingMap(ctx); err != nil {
					logger.Error("error populating pricing map", "error", err)
				}
			}
		}
	}(ctx)

	return &Collector{
		regions:      config.Regions,
		regionMap:    config.RegionMap,
		pricingStore: pricingStore,
		logger:       logger,
		accountID:    config.AccountID,
	}, nil
}

func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	snapshot := c.pricingStore.Snapshot()

	var wg sync.WaitGroup
	for _, region := range c.regions {
		if region.RegionName == nil || *region.RegionName == "" {
			c.logger.Warn("skipping region with empty name")
			continue
		}

		regionName := *region.RegionName
		regionClient, ok := c.regionMap[regionName]
		if !ok {
			c.logger.Warn("skipping region without configured client", "region", regionName)
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			clusters, err := regionClient.ListMSKClusters(ctx)
			if err != nil {
				c.logger.Error("error listing MSK clusters", "region", regionName, "error", err)
				return
			}
			for _, cluster := range clusters {
				c.collectCluster(ch, snapshot, regionName, cluster)
			}
		}()
	}
	wg.Wait()
	return nil
}

func (c *Collector) collectCluster(ch chan<- prometheus.Metric, snapshot pricingstore.Snapshot, region string, cluster msktypes.Cluster) {
	clusterData, err := buildClusterPricingData(cluster)
	if err != nil {
		c.logger.Warn("skipping unsupported or incomplete MSK cluster", "region", region, "cluster_arn", aws.ToString(cluster.ClusterArn), "error", err)
		return
	}

	brokerUnitPrice, err := c.getBrokerUnitPrice(snapshot, region, clusterData.instanceType)
	if err != nil {
		c.logger.Warn("skipping MSK cluster with unpriceable broker shape", "region", region, "cluster_arn", clusterData.clusterARN, "instance_type", clusterData.instanceType, "error", err)
		return
	}

	storagePricePerGiBMonth, err := c.getStoragePricePerGiBMonth(snapshot, region)
	if err != nil {
		c.logger.Warn("skipping MSK cluster with unpriceable storage shape", "region", region, "cluster_arn", clusterData.clusterARN, "error", err)
		return
	}

	computeHourlyRate := brokerUnitPrice * float64(clusterData.brokerCount)
	totalAllocatedStorageGiB := float64(clusterData.brokerCount) * float64(clusterData.volumeSizeGiB)
	storageHourlyRate := (storagePricePerGiBMonth / utils.HoursInMonth) * totalAllocatedStorageGiB

	ch <- prometheus.MustNewConstMetric(
		ComputeHourlyGaugeDesc,
		prometheus.GaugeValue,
		computeHourlyRate,
		c.accountID,
		region,
		clusterData.clusterName,
		clusterData.clusterARN,
		clusterData.instanceType,
	)
	ch <- prometheus.MustNewConstMetric(
		StorageHourlyGaugeDesc,
		prometheus.GaugeValue,
		storageHourlyRate,
		c.accountID,
		region,
		clusterData.clusterName,
		clusterData.clusterARN,
	)
}

func buildClusterPricingData(cluster msktypes.Cluster) (clusterPricingData, error) {
	if cluster.ClusterType != "" && cluster.ClusterType != msktypes.ClusterTypeProvisioned {
		return clusterPricingData{}, fmt.Errorf("cluster type %q is not supported", cluster.ClusterType)
	}
	if cluster.Provisioned == nil {
		return clusterPricingData{}, fmt.Errorf("cluster is missing provisioned data")
	}
	clusterName := aws.ToString(cluster.ClusterName)
	if clusterName == "" {
		return clusterPricingData{}, fmt.Errorf("cluster name is missing")
	}
	clusterARN := aws.ToString(cluster.ClusterArn)
	if clusterARN == "" {
		return clusterPricingData{}, fmt.Errorf("cluster ARN is missing")
	}

	provisioned := cluster.Provisioned
	if provisioned.StorageMode == msktypes.StorageModeTiered {
		return clusterPricingData{}, fmt.Errorf("tiered storage is not supported")
	}
	brokerCount := aws.ToInt32(provisioned.NumberOfBrokerNodes)
	if brokerCount <= 0 {
		return clusterPricingData{}, fmt.Errorf("broker count is missing")
	}
	if provisioned.BrokerNodeGroupInfo == nil {
		return clusterPricingData{}, fmt.Errorf("broker node group info is missing")
	}
	instanceType := aws.ToString(provisioned.BrokerNodeGroupInfo.InstanceType)
	if instanceType == "" {
		return clusterPricingData{}, fmt.Errorf("instance type is missing")
	}

	if strings.HasPrefix(instanceType, "express.") {
		return clusterPricingData{}, fmt.Errorf("express brokers are not supported")
	}

	storageInfo := provisioned.BrokerNodeGroupInfo.StorageInfo
	if storageInfo == nil || storageInfo.EbsStorageInfo == nil {
		return clusterPricingData{}, fmt.Errorf("EBS storage info is missing")
	}
	volumeSizeGiB := aws.ToInt32(storageInfo.EbsStorageInfo.VolumeSize)
	if volumeSizeGiB <= 0 {
		return clusterPricingData{}, fmt.Errorf("EBS volume size is missing")
	}
	if storageInfo.EbsStorageInfo.ProvisionedThroughput != nil && aws.ToBool(storageInfo.EbsStorageInfo.ProvisionedThroughput.Enabled) {
		return clusterPricingData{}, fmt.Errorf("EBS provisioned throughput is not supported")
	}

	return clusterPricingData{
		clusterName:   clusterName,
		clusterARN:    clusterARN,
		instanceType:  instanceType,
		brokerCount:   brokerCount,
		volumeSizeGiB: volumeSizeGiB,
	}, nil
}

// newPriceFetcher returns a PriceFetchFunc for MSK pricing lookups.
// The AWS Price List API is served from a small set of endpoint regions.
// We standardize on us-east-1 for pricing lookups; the actual priced
// region is selected by the GetProducts filters.
func newPriceFetcher(pricingClient client.Client) pricingstore.PriceFetchFunc {
	return func(ctx context.Context, region string) ([]string, error) {
		var prices []string

		for _, filters := range [][]pricingtypes.Filter{brokerPriceFilters, storagePriceFilters} {
			priceList, err := pricingClient.ListMSKServicePrices(ctx, region, filters)
			if err != nil {
				return nil, err
			}
			prices = append(prices, priceList...)
		}

		return prices, nil
	}
}

func (c *Collector) getBrokerUnitPrice(snapshot pricingstore.Snapshot, region, instanceType string) (float64, error) {
	usageType := mskBrokerUsageTypePrefix + strings.TrimPrefix(instanceType, "kafka.")
	return c.findRegionPrice(snapshot, region, func(candidate string) bool {
		return strings.HasSuffix(candidate, usageType)
	})
}

func (c *Collector) getStoragePricePerGiBMonth(snapshot pricingstore.Snapshot, region string) (float64, error) {
	return c.findRegionPrice(snapshot, region, func(candidate string) bool {
		return strings.HasSuffix(candidate, mskStorageUsageType)
	})
}

func (c *Collector) findRegionPrice(snapshot pricingstore.Snapshot, region string, matches func(string) bool) (float64, error) {
	pricePerUnit, ok := snapshot.Region(region)
	if !ok {
		return 0, fmt.Errorf("no pricing data found for region %s", region)
	}

	matched := false
	price := 0.0
	for usageType, candidatePrice := range pricePerUnit.Entries() {
		if !matches(usageType) {
			continue
		}
		if matched {
			return 0, fmt.Errorf("found multiple prices matching region %s", region)
		}
		price = candidatePrice
		matched = true
	}

	if !matched {
		return 0, fmt.Errorf("no matching pricing data found for region %s", region)
	}

	return price, nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- ComputeHourlyGaugeDesc
	ch <- StorageHourlyGaugeDesc
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Register(registry provider.Registry) error {
	return nil
}
