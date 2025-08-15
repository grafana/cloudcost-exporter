package elb

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	elbv2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/elbv2"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

const (
	subsystem = "aws_elb"
)

var (
	LoadBalancerHourlyCostDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"loadbalancer_total_usd_per_hour",
		"The total cost of the load balancer in USD/h",
		[]string{"name", "arn", "region", "type", "scheme"},
	)
)

type Collector struct {
	Regions          []ec2Types.Region
	ScrapeInterval   time.Duration
	pricingMap       *ELBPricingMap
	client           client.Client
	NextScrape       time.Time
	elbRegionClients map[string]elbv2client.ELBv2
	logger           *slog.Logger
}

type Config struct {
	ScrapeInterval time.Duration
	Regions        []ec2Types.Region
	RegionClients  map[string]elbv2client.ELBv2
	Logger         *slog.Logger
}

type LoadBalancerInfo struct {
	Name   string
	ARN    string
	Type   types.LoadBalancerTypeEnum
	Scheme types.LoadBalancerSchemeEnum
	Region string
	Cost   float64
}

type elbProduct struct {
	Product struct {
		Attributes struct {
			Region        string `json:"regionCode"`
			ProductFamily string `json:"productFamily"`
			LoadBalancer  string `json:"loadBalancer"`
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

func New(config *Config, client client.Client) *Collector {
	return &Collector{
		Regions:          config.Regions,
		ScrapeInterval:   config.ScrapeInterval,
		client:           client,
		elbRegionClients: config.RegionClients,
		logger:           config.Logger,
		pricingMap:       NewELBPricingMap(config.Logger),
	}
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- LoadBalancerHourlyCostDesc
	return nil
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	c.logger.Info("Starting ELB collection")

	if c.shouldScrape() {
		if err := c.pricingMap.refresh(c.client, c.Regions); err != nil {
			c.logger.Error("Failed to refresh pricing", "error", err)
			return err
		}
		c.NextScrape = time.Now().Add(c.ScrapeInterval)
	}

	loadBalancers, err := c.collectLoadBalancers()
	if err != nil {
		c.logger.Error("Failed to collect load balancers", "error", err)
		return err
	}

	for _, lb := range loadBalancers {
		ch <- prometheus.MustNewConstMetric(
			LoadBalancerHourlyCostDesc,
			prometheus.GaugeValue,
			lb.Cost,
			lb.Name,
			lb.ARN,
			lb.Region,
			string(lb.Type),
			string(lb.Scheme),
		)
	}

	c.logger.Info("Completed ELB collection", "load_balancers", len(loadBalancers))
	return nil
}

func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	err := c.Collect(ch)
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

func (c *Collector) collectLoadBalancers() ([]LoadBalancerInfo, error) {
	var allLoadBalancers []LoadBalancerInfo
	var mu sync.Mutex

	eg := errgroup.Group{}

	for regionName, client := range c.elbRegionClients {
		regionName := regionName
		client := client

		eg.Go(func() error {
			loadBalancers, err := c.collectRegionLoadBalancers(regionName, client)
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

func (c *Collector) collectRegionLoadBalancers(region string, client elbv2client.ELBv2) ([]LoadBalancerInfo, error) {
	ctx := context.Background()
	var loadBalancers []LoadBalancerInfo

	result, err := client.DescribeLoadBalancers(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to describe load balancers: %w", err)
	}

	for _, lb := range result.LoadBalancers {
		cost := c.calculateLoadBalancerCost(lb, region)

		loadBalancers = append(loadBalancers, LoadBalancerInfo{
			Name:   *lb.LoadBalancerName,
			ARN:    *lb.LoadBalancerArn,
			Type:   lb.Type,
			Scheme: lb.Scheme,
			Region: region,
			Cost:   cost,
		})
	}

	return loadBalancers, nil
}

func (c *Collector) calculateLoadBalancerCost(lb types.LoadBalancer, region string) float64 {
	pricing, err := c.pricingMap.GetRegionPricing(region)
	if err != nil {
		c.logger.Warn("Failed to get pricing data for region", "error", err)
		return 0
	}

	switch lb.Type {
	case types.LoadBalancerTypeEnumApplication:
		if rate, exists := pricing.ALBHourlyRate["default"]; exists {
			return rate
		}
	case types.LoadBalancerTypeEnumNetwork:
		if rate, exists := pricing.NLBHourlyRate["default"]; exists {
			return rate
		}
	}

	c.logger.Warn("No pricing data available for load balancer type", "type", lb.Type, "region", region)
	return 0
}
