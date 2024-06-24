package ec2

import (
	"context"
	"log/slog"
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
	logger          *slog.Logger
	context         context.Context
}

type Config struct {
	Regions []ec2Types.Region
	Logger  *slog.Logger
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(_ chan<- prometheus.Metric) error {
	c.logger.LogAttrs(c.context, slog.LevelInfo, "TODO - Implement ec2.Describe")
	return nil
}

func (c *Collector) Describe(_ chan<- *prometheus.Desc) error {
	c.logger.LogAttrs(c.context, slog.LevelInfo, "TODO - Implement ec2.Describe")
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func New(config *Config, ps pricingClient.Pricing, ec2s ec2client.EC2, regionClientMap map[string]ec2client.EC2) *Collector {
	logger := config.Logger.With("collector", "ec2")
	return &Collector{
		pricingService:  ps,
		ec2Client:       ec2s,
		Regions:         config.Regions,
		ec2RegionClient: regionClientMap,
		logger:          logger,
		context:         context.TODO(),
	}
}

func (c *Collector) Register(_ provider.Registry) error {
	c.logger.LogAttrs(c.context, slog.LevelInfo, "Registering AWS EC2 collector")
	return nil
}
