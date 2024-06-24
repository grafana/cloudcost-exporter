package ec2

import (
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/prometheus/client_golang/prometheus"

	ec2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
	pricingClient "github.com/grafana/cloudcost-exporter/pkg/aws/services/pricing"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "aws_ec2"
)

// Collector is a prometheus collector that collects metrics from AWS EKS clusters.
type Collector struct {
	Region          string
	Regions         []ec2Types.Region
	Profile         string
	Profiles        []string
	ScrapeInterval  time.Duration
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
	return nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func New(ps pricingClient.Pricing, ec2s ec2client.EC2, regions []ec2Types.Region, regionClientMap map[string]ec2client.EC2) *Collector {
	return &Collector{
		pricingService:  ps,
		ec2Client:       ec2s,
		Regions:         regions,
		ec2RegionClient: regionClientMap,
	}
}

func (c *Collector) Register(_ provider.Registry) error {
	return nil
}
