package elb

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbTypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

const (
	subsystem = "aws_elb"
)

var (
	LoadBalancerUsageHourlyCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"loadbalancer_usage_total_usd_per_hour",
		"The total cost of hourly usage of the load balancer in USD/h",
		[]string{"name", "region", "type"},
	)
	LoadBalancerCapacityUnitsUsageHourlyCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"loadbalancer_capacity_units_total_usd_per_hour",
		"The total cost of Load Balancer Capacity units (LCU) used in USD/hour",
		[]string{"name", "region", "type"},
	)
)

type Collector struct {
	Regions            []ec2Types.Region
	ScrapeInterval     time.Duration
	pricingMap         *ELBPricingMap
	awsRegionClientMap map[string]client.Client
	NextScrape         time.Time
	logger             *slog.Logger
}

type Config struct {
	ScrapeInterval time.Duration
	Regions        []ec2Types.Region
	RegionClients  map[string]client.Client
	Logger         *slog.Logger
}

type LoadBalancerInfo struct {
	Name                  string
	Type                  elbTypes.LoadBalancerTypeEnum
	Region                string
	LCUUsageCost          float64
	LoadBalancerUsageCost float64
}

type elbProduct struct {
	Product struct {
		Attributes struct {
			RegionCode string `json:"regionCode"`
			UsageType  string `json:"usageType"`
			Operation  string `json:"operation"`
		}
	}
	Terms struct {
		OnDemand map[string]struct {
			PriceDimensions map[string]struct {
				PricePerUnit map[string]string `json:"pricePerUnit"`
			}
		}
	}
}

func New(config *Config) *Collector {
	return &Collector{
		Regions:            config.Regions,
		ScrapeInterval:     config.ScrapeInterval,
		awsRegionClientMap: config.RegionClients,
		logger:             config.Logger,
		pricingMap:         NewELBPricingMap(config.Logger),
	}
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- LoadBalancerUsageHourlyCostDesc
	ch <- LoadBalancerCapacityUnitsUsageHourlyCostDesc
	return nil
}

func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	c.logger.Info("Starting ELB collection")

	if c.shouldScrape() {
		if err := c.pricingMap.refresh(c.awsRegionClientMap, c.Regions); err != nil {
			c.logger.Error("Failed to refresh pricing", "error", err)
			return err
		}
		c.NextScrape = time.Now().Add(c.ScrapeInterval)
	}

	loadBalancers, err := c.collectLoadBalancers(ctx)
	if err != nil {
		c.logger.Error("Failed to collect load balancers", "error", err)
		return err
	}

	for _, lb := range loadBalancers {
		if lb.LoadBalancerUsageCost > 0 {
			ch <- prometheus.MustNewConstMetric(
				LoadBalancerUsageHourlyCostDesc,
				prometheus.GaugeValue,
				lb.LoadBalancerUsageCost,
				lb.Name,
				lb.Region,
				string(lb.Type),
			)
		} else {
			c.logger.Warn("No LoadBalancerUsage cost data available for load balancer", "name", lb.Name, "region", lb.Region, "type", lb.Type)
		}

		if lb.LCUUsageCost > 0 {
			ch <- prometheus.MustNewConstMetric(
				LoadBalancerCapacityUnitsUsageHourlyCostDesc,
				prometheus.GaugeValue,
				lb.LCUUsageCost,
				lb.Name,
				lb.Region,
				string(lb.Type),
			)
		} else {
			c.logger.Warn("No LCUUsage cost data available for load balancer", "name", lb.Name, "region", lb.Region, "type", lb.Type)
		}
	}

	c.logger.Info("Completed ELB collection", "load_balancers", len(loadBalancers))
	return nil
}

func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	err := c.Collect(context.Background(), ch)
	if err != nil {
		c.logger.Error("Failed to collect metrics", "error", err)
		return 0
	}
	return 1
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) shouldScrape() bool {
	return time.Now().After(c.NextScrape)
}

func (c *Collector) collectLoadBalancers(ctx context.Context) ([]LoadBalancerInfo, error) {
	var allLoadBalancers []LoadBalancerInfo
	var mu sync.Mutex

	eg := errgroup.Group{}
	for regionName := range c.awsRegionClientMap {
		eg.Go(func() error {
			loadBalancers, err := c.collectRegionLoadBalancers(ctx, regionName)
			if err != nil {
				return fmt.Errorf("failed to collect load balancers for region %s: %w", regionName, err)
			}

			mu.Lock()
			allLoadBalancers = append(allLoadBalancers, loadBalancers...)
			mu.Unlock()

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	return allLoadBalancers, nil
}

func (c *Collector) collectRegionLoadBalancers(ctx context.Context, region string) ([]LoadBalancerInfo, error) {
	var loadBalancers []LoadBalancerInfo

	lbList, err := c.awsRegionClientMap[region].DescribeLoadBalancers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to describe load balancers: %w", err)
	}

	for _, lb := range lbList {
		lcuUsageCost, loadBalancerUsageCost := c.calculateLoadBalancerCost(lb, region)
		loadBalancers = append(loadBalancers, LoadBalancerInfo{
			Name:                  *lb.LoadBalancerName,
			Type:                  lb.Type,
			Region:                region,
			LCUUsageCost:          lcuUsageCost,
			LoadBalancerUsageCost: loadBalancerUsageCost,
		})
	}

	return loadBalancers, nil
}

func (c *Collector) calculateLoadBalancerCost(lb elbTypes.LoadBalancer, region string) (float64, float64) {
	pricing, err := c.pricingMap.GetRegionPricing(region)
	if err != nil {
		c.logger.Warn("Failed to get pricing data for region", "error", err)
		return 0, 0
	}

	lcuUsageCost, loadBalancerUsageCost := 0.0, 0.0
	switch lb.Type {
	case elbTypes.LoadBalancerTypeEnumApplication:
		if rate, exists := pricing.ALBHourlyRate[LCUUsage]; exists {
			lcuUsageCost = rate
		} else {
			c.logger.Warn("No LCUUsage cost data available for ALB", "region", region)
			lcuUsageCost = ALCUUsageHourlyRateDefault
		}

		if rate, exists := pricing.ALBHourlyRate[LoadBalancerUsage]; exists {
			loadBalancerUsageCost = rate
		} else {
			c.logger.Warn("No LoadBalancerUsage cost data available for ALB", "region", region)
			loadBalancerUsageCost = LoadBalancerUsageHourlyRateDefault
		}

	case elbTypes.LoadBalancerTypeEnumNetwork:
		if rate, exists := pricing.NLBHourlyRate[LCUUsage]; exists {
			lcuUsageCost = rate
		} else {
			c.logger.Warn("No LCUUsage cost data available for NLB", "region", region)
			lcuUsageCost = NLCUUsageHourlyRateDefault
		}

		if rate, exists := pricing.NLBHourlyRate[LoadBalancerUsage]; exists {
			loadBalancerUsageCost = rate
		} else {
			c.logger.Warn("No LoadBalancerUsage cost data available for NLB", "region", region)
			loadBalancerUsageCost = LoadBalancerUsageHourlyRateDefault
		}
	default:
		c.logger.Warn("Unknown load balancer type", "type", lb.Type)
	}

	return lcuUsageCost, loadBalancerUsageCost
}
