package eks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	ec2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
	pricingClient "github.com/grafana/cloudcost-exporter/pkg/aws/services/pricing"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem  = "aws_eks"
	maxResults = 1000
)

var clusterTags = []string{"cluster", "eks:cluster-name", "aws:eks:cluster-name"}

var (
	ErrRegionNotFound       = errors.New("no region found")
	ErrInstanceTypeNotFound = errors.New("no instance type found")
	ErrClientNotFound       = errors.New("no client found")
	ErrListSpotPrices       = errors.New("error listing spot prices")
	ErrGeneratePricingMap   = errors.New("error generating pricing map")
	ErrListOnDemandPrices   = errors.New("error listing ondemand prices")
)

var (
	InstanceCPUHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "instance_cpu_usd_per_core_hour"),
		"The cpu cost a compute instance in USD/(core*h)",
		[]string{"instance", "region", "family", "machine_type", "cluster", "price_tier"},
		nil,
	)
	InstanceMemoryHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, "instance_memory_usd_per_gib_hour"),
		"The memory cost of a compute instance in USD/(GiB*h)",
		[]string{"instance", "region", "family", "machine_type", "cluster", "price_tier"},
		nil,
	)
)

// Attributes represents ec2 instance attributes that are pulled from AWS api's describing instances.
// It's specifically pulled out of productTerm to enable usage during tests.
type Attributes struct {
	Region            string `json:"regionCode"`
	InstanceType      string `json:"instanceType"`
	VCPU              string `json:"vcpu"`
	Memory            string `json:"memory"`
	InstanceFamily    string `json:"instanceFamily"`
	PhysicalProcessor string `json:"physicalProcessor"`
	Tenancy           string `json:"tenancy"`
	MarketOption      string `json:"marketOption"`
	OperatingSystem   string `json:"operatingSystem"`
	ClockSpeed        string `json:"clockSpeed"`
	UsageType         string `json:"usageType"`
}

// productTerm represents the nested json response returned by the AWS pricing API.
type productTerm struct {
	Product struct {
		Attributes Attributes
	}
	Terms struct {
		OnDemand map[string]struct {
			PriceDimensions map[string]struct {
				PricePerUnit map[string]string `json:"pricePerUnit"`
			}
		}
	}
}

// Collector is a prometheus collector that collects metrics from AWS EKS clusters.
type Collector struct {
	Region          string
	Regions         []ec2Types.Region
	Profile         string
	Profiles        []string
	ScrapeInterval  time.Duration
	pricingMap      *StructuredPricingMap
	pricingService  pricingClient.Pricing
	ec2Client       ec2client.EC2
	NextScrape      time.Time
	ec2RegionClient map[string]ec2client.EC2
	logger          *slog.Logger
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	ctx := context.Background()
	if c.pricingMap == nil || time.Now().After(c.NextScrape) {
		var prices []string
		var spotPrices []ec2Types.SpotPrice
		eg := new(errgroup.Group)
		eg.SetLimit(5)
		m := sync.Mutex{}
		for _, region := range c.Regions {
			eg.Go(func() error {
				priceList, err := ListOnDemandPrices(ctx, *region.RegionName, c.pricingService)
				if err != nil {
					return fmt.Errorf("%w: %w", ErrListOnDemandPrices, err)
				}

				if c.ec2RegionClient[*region.RegionName] == nil {
					return ErrClientNotFound
				}
				client := c.ec2RegionClient[*region.RegionName]
				spotPriceList, err := ListSpotPrices(ctx, client)
				if err != nil {
					return fmt.Errorf("%w: %w", ErrListSpotPrices, err)
				}
				m.Lock()
				spotPrices = append(spotPrices, spotPriceList...)
				prices = append(prices, priceList...)
				m.Unlock()
				return nil
			})
		}
		err := eg.Wait()
		if err != nil {
			return err
		}
		c.pricingMap = NewStructuredPricingMap(c.logger)
		if err := c.pricingMap.GeneratePricingMap(prices, spotPrices); err != nil {
			return fmt.Errorf("%w: %w", ErrGeneratePricingMap, err)
		}
		c.NextScrape = time.Now().Add(c.ScrapeInterval)
	}

	wg := sync.WaitGroup{}
	wg.Add(len(c.Regions))
	instanceCh := make(chan []ec2Types.Reservation, len(c.Regions))
	for _, region := range c.Regions {
		go func(region ec2Types.Region) {
			start := time.Now()
			defer wg.Done()
			client := c.ec2RegionClient[*region.RegionName]
			reservations, err := ListComputeInstances(ctx, client)
			if err != nil {
				c.logger.LogAttrs(ctx, slog.LevelError, "error listing instances",
					slog.String("region", *region.RegionName),
					slog.String("error", err.Error()),
				)
				return
			}
			c.logger.Info("found instances",
				"count", len(reservations),
				"region", *region.RegionName,
				"duration", time.Since(start))
			instanceCh <- reservations
		}(region)
	}
	go func() {
		wg.Wait()
		close(instanceCh)
	}()
	c.emitMetricsFromChannel(instanceCh, ch)
	return nil
}

func (c *Collector) emitMetricsFromChannel(reservationsCh chan []ec2Types.Reservation, ch chan<- prometheus.Metric) {
	for reservations := range reservationsCh {
		for _, reservation := range reservations {
			for _, instance := range reservation.Instances {
				clusterName := clusterNameFromInstance(instance)
				if clusterName == "" {
					c.logger.Debug("no cluster name found for instance", "instance", *instance.InstanceId)
					continue
				}
				if instance.PrivateDnsName == nil || *instance.PrivateDnsName == "" {
					c.logger.Debug("no private dns name found for instance", "instance", *instance.InstanceId)
					continue
				}
				if instance.Placement == nil || instance.Placement.AvailabilityZone == nil {
					c.logger.Debug("no availability zone found for instance", "instance", *instance.InstanceId)
					continue
				}

				region := *instance.Placement.AvailabilityZone

				pricetier := "spot"
				if instance.InstanceLifecycle != ec2Types.InstanceLifecycleTypeSpot {
					pricetier = "ondemand"
					// Ondemand instances are keyed based upon their region, so we need to remove the availability zone
					region = region[:len(region)-1]
				}
				price, err := c.pricingMap.GetPriceForInstanceType(region, string(instance.InstanceType))
				if err != nil {
					c.logger.Error("error getting price for instance type", "instance_type", instance.InstanceType, "error", err)
					continue
				}
				labelValues := []string{
					*instance.PrivateDnsName,
					region,
					c.pricingMap.InstanceDetails[string(instance.InstanceType)].InstanceFamily,
					string(instance.InstanceType),
					clusterName,
					pricetier,
				}
				ch <- prometheus.MustNewConstMetric(InstanceCPUHourlyCostDesc, prometheus.GaugeValue, price.Cpu, labelValues...)
				ch <- prometheus.MustNewConstMetric(InstanceMemoryHourlyCostDesc, prometheus.GaugeValue, price.Ram, labelValues...)
			}
		}
	}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- InstanceCPUHourlyCostDesc
	ch <- InstanceMemoryHourlyCostDesc
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func New(region string, profile string, scrapeInterval time.Duration, ps pricingClient.Pricing, ec2s ec2client.EC2, regions []ec2Types.Region, regionClientMap map[string]ec2client.EC2, logger *slog.Logger) *Collector {
	logger = logger.With("collector", "eks")
	return &Collector{
		Region:          region,
		Profile:         profile,
		ScrapeInterval:  scrapeInterval,
		pricingService:  ps,
		ec2Client:       ec2s,
		Regions:         regions,
		ec2RegionClient: regionClientMap,
		logger:          logger,
	}
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}

func ListOnDemandPrices(ctx context.Context, region string, client pricingClient.Pricing) ([]string, error) {
	var productOutputs []string
	input := &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		Filters: []types.Filter{
			{
				Field: aws.String("regionCode"),
				// TODO: Use the defined enum for this once I figure out how I can import it
				Type:  "TERM_MATCH",
				Value: aws.String(region),
			},
			{
				// Limit output to only base installs
				Field: aws.String("preInstalledSw"),
				Type:  "TERM_MATCH",
				Value: aws.String("NA"),
			},
			{
				// Limit to shared tenancy machines
				Field: aws.String("tenancy"),
				Type:  "TERM_MATCH",
				Value: aws.String("shared"),
			},
			{
				// Limit to compute instances(ie, not bare metal)
				Field: aws.String("productFamily"),
				Type:  "TERM_MATCH",
				Value: aws.String("Compute Instance"),
			},
			{
				// RunInstances is the operation that we're interested in.
				Field: aws.String("operation"),
				Type:  "TERM_MATCH",
				Value: aws.String("RunInstances"),
			},
			{
				// This effectively filters only for ondemand pricing
				Field: aws.String("capacitystatus"),
				Type:  "TERM_MATCH",
				Value: aws.String("UnusedCapacityReservation"),
			},
			{
				// Only care about Linux. If there's a request for windows, remove this flag and expand the pricing map to include a key for operating system
				Field: aws.String("operatingSystem"),
				Type:  "TERM_MATCH",
				Value: aws.String("Linux"),
			},
		},
	}

	for {
		products, err := client.GetProducts(ctx, input)
		if err != nil {
			return productOutputs, err
		}

		if products == nil {
			break
		}

		productOutputs = append(productOutputs, products.PriceList...)
		if products.NextToken == nil {
			break
		}
		input.NextToken = products.NextToken
	}
	return productOutputs, nil
}

func ListComputeInstances(ctx context.Context, client ec2client.EC2) ([]ec2Types.Reservation, error) {
	dii := &ec2.DescribeInstancesInput{
		// 1000 max results was decided arbitrarily. This can likely be tuned.
		MaxResults: aws.Int32(maxResults),
	}
	var instances []ec2Types.Reservation
	for {
		resp, err := client.DescribeInstances(ctx, dii)
		if err != nil {
			return nil, err
		}
		instances = append(instances, resp.Reservations...)
		if resp.NextToken == nil || *resp.NextToken == "" {
			break
		}
		dii.NextToken = resp.NextToken
	}

	return instances, nil
}

func ListSpotPrices(ctx context.Context, client ec2client.EC2) ([]ec2Types.SpotPrice, error) {
	var spotPrices []ec2Types.SpotPrice
	startTime := time.Now().Add(-time.Hour)
	endTime := time.Now()
	sphi := &ec2.DescribeSpotPriceHistoryInput{
		ProductDescriptions: []string{
			"Linux/UNIX (Amazon VPC)",
		},

		StartTime: &startTime,
		EndTime:   &endTime,
	}
	for {
		resp, err := client.DescribeSpotPriceHistory(ctx, sphi)
		if err != nil {
			// If there's an error, return the set of processed spotPrices and the error.
			return spotPrices, err
		}
		spotPrices = append(spotPrices, resp.SpotPriceHistory...)
		if resp.NextToken == nil || *resp.NextToken == "" {
			break
		}
		sphi.NextToken = resp.NextToken
	}
	return spotPrices, nil
}

func clusterNameFromInstance(instance ec2Types.Instance) string {
	for _, tag := range instance.Tags {
		for _, key := range clusterTags {
			if *tag.Key == key {
				return *tag.Value
			}
		}
	}
	return ""
}
