package eks

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/aws/compute"
	ec2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
	pricingClient "github.com/grafana/cloudcost-exporter/pkg/aws/services/pricing"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "aws_eks"
)

var (
	ErrClientNotFound = errors.New("no client found")

	ErrGeneratePricingMap = errors.New("error generating pricing map")
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

// Collector is a prometheus collector that collects metrics from AWS EKS clusters.
type Collector struct {
	Region          string
	Regions         []ec2Types.Region
	Profile         string
	Profiles        []string
	ScrapeInterval  time.Duration
	pricingMap      *compute.StructuredPricingMap
	pricingService  pricingClient.Pricing
	ec2Client       ec2client.EC2
	NextScrape      time.Time
	ec2RegionClient map[string]ec2client.EC2
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	if c.pricingMap == nil || time.Now().After(c.NextScrape) {
		var prices []string
		var spotPrices []ec2Types.SpotPrice
		eg := new(errgroup.Group)
		eg.SetLimit(5)
		m := sync.Mutex{}
		for _, region := range c.Regions {
			eg.Go(func() error {
				priceList, err := compute.ListOnDemandPrices(context.Background(), *region.RegionName, c.pricingService)
				if err != nil {
					return fmt.Errorf("%w: %w", compute.ErrListOnDemandPrices, err)
				}

				if c.ec2RegionClient[*region.RegionName] == nil {
					return ErrClientNotFound
				}
				client := c.ec2RegionClient[*region.RegionName]
				spotPriceList, err := compute.ListSpotPrices(context.Background(), client)
				if err != nil {
					return fmt.Errorf("%w: %w", compute.ErrListSpotPrices, err)
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
		c.pricingMap = compute.NewStructuredPricingMap()
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
			defer wg.Done()
			client := c.ec2RegionClient[*region.RegionName]
			reservations, err := compute.ListComputeInstances(context.Background(), client)
			if err != nil {
				log.Printf("error listing instances: %s", err)
				return
			}
			log.Printf("found %d instances in region %s", len(reservations), *region.RegionName)
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
				clusterName := compute.ClusterNameFromInstance(instance)
				if clusterName == "" {
					log.Printf("no cluster name found for instance %s", *instance.InstanceId)
					continue
				}
				if instance.PrivateDnsName == nil || *instance.PrivateDnsName == "" {
					log.Printf("no private dns name found for instance %s", *instance.InstanceId)
					continue
				}
				if instance.Placement == nil || instance.Placement.AvailabilityZone == nil {
					log.Printf("no availability zone found for instance %s", *instance.InstanceId)
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
					log.Printf("error getting price for instance type %s: %s", instance.InstanceType, err)
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

func New(region string, profile string, scrapeInterval time.Duration, ps pricingClient.Pricing, ec2s ec2client.EC2, regions []ec2Types.Region, regionClientMap map[string]ec2client.EC2) *Collector {
	return &Collector{
		Region:          region,
		Profile:         profile,
		ScrapeInterval:  scrapeInterval,
		pricingService:  ps,
		ec2Client:       ec2s,
		Regions:         regions,
		ec2RegionClient: regionClientMap,
	}
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}
