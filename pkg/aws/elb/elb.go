package elb

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	elbv2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/elbv2"
	pricingClient "github.com/grafana/cloudcost-exporter/pkg/aws/services/pricing"
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
	pricingService   pricingClient.Pricing
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

func New(config *Config, ps pricingClient.Pricing) *Collector {
	return &Collector{
		Regions:          config.Regions,
		ScrapeInterval:   config.ScrapeInterval,
		pricingService:   ps,
		elbRegionClients: config.RegionClients,
		logger:           config.Logger,
		pricingMap:       NewELBPricingMap(),
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
		if err := c.refreshPricing(); err != nil {
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

func (c *Collector) refreshPricing() error {
	c.logger.Info("Refreshing ELB pricing data")

	eg := errgroup.Group{}
	var mu sync.Mutex

	for _, region := range c.Regions {
		region := region
		eg.Go(func() error {
			pricing, err := c.fetchRegionPricing(*region.RegionName)
			if err != nil {
				return fmt.Errorf("failed to fetch pricing for region %s: %w", *region.RegionName, err)
			}

			mu.Lock()
			c.pricingMap.SetRegionPricing(*region.RegionName, pricing)
			mu.Unlock()

			return nil
		})
	}

	return eg.Wait()
}

func (c *Collector) fetchRegionPricing(region string) (*RegionPricing, error) {
	regionPricing := &RegionPricing{
		ALBHourlyRate: make(map[string]float64),
		NLBHourlyRate: make(map[string]float64),
		CLBHourlyRate: make(map[string]float64),
	}

	// Fetch ELB pricing from AWS Pricing API
	input := &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		Filters: []pricingTypes.Filter{
			{
				Field: aws.String("regionCode"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String(region),
			},
			{
				Field: aws.String("productFamily"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String("Load Balancer"),
			},
		},
	}

	prices, err := c.getPricesFromProductList(context.Background(), input)
	if err != nil {
		return nil, fmt.Errorf("failed to get ELB pricing: %w", err)
	}

	for _, product := range prices {
		var productInfo elbProduct
		if err := json.Unmarshal([]byte(product), &productInfo); err != nil {
			c.logger.Warn("Failed to unmarshal pricing product", "error", err)
			continue
		}

		// Extract pricing information
		for _, term := range productInfo.Terms.OnDemand {
			for _, priceDimension := range term.PriceDimensions {
				price, err := strconv.ParseFloat(priceDimension.PricePerUnit["USD"], 64)
				if err != nil {
					continue
				}

				// Determine the load balancer type based on product family or attributes
				switch productInfo.Product.Attributes.ProductFamily {
				case "Load Balancer-Application":
					regionPricing.ALBHourlyRate["default"] = price
				case "Load Balancer-Network":
					regionPricing.NLBHourlyRate["default"] = price
				case "Load Balancer":
					// Classic Load Balancer
					regionPricing.CLBHourlyRate["default"] = price
				}
			}
		}
	}

	// Set default rates if not found (fallback values)
	if len(regionPricing.ALBHourlyRate) == 0 {
		regionPricing.ALBHourlyRate["default"] = 0.0225 // Default ALB rate
	}
	if len(regionPricing.NLBHourlyRate) == 0 {
		regionPricing.NLBHourlyRate["default"] = 0.0225 // Default NLB rate
	}
	if len(regionPricing.CLBHourlyRate) == 0 {
		regionPricing.CLBHourlyRate["default"] = 0.025 // Default CLB rate
	}

	return regionPricing, nil
}

func (c *Collector) getPricesFromProductList(ctx context.Context, input *pricing.GetProductsInput) ([]string, error) {
	var productOutputs []string

	for {
		products, err := c.pricingService.GetProducts(ctx, input)
		if err != nil {
			return productOutputs, err
		}

		if products == nil {
			break
		}

		productOutputs = append(productOutputs, products.PriceList...)
		if products.NextToken == nil || *products.NextToken == "" {
			break
		}
		input.NextToken = products.NextToken
	}
	return productOutputs, nil
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
	pricing := c.pricingMap.GetRegionPricing(region)
	if pricing == nil {
		c.logger.Warn("No pricing data available for region", "region", region)
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
