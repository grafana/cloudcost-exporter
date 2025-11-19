package vpc

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
)

const (
	collectorName = "VPC"
)

const PriceRefreshInterval = 24 * time.Hour

var (
	subsystem = fmt.Sprintf("gcp_%s", strings.ToLower(collectorName))

	CloudNATGatewayHourlyGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"nat_gateway_hourly_rate_usd_per_hour",
		"Hourly cost of Cloud NAT Gateway by region and project. Cost represented in USD/hour",
		[]string{"region", "project"},
	)
	CloudNATDataProcessingGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"nat_gateway_data_processing_usd_per_gb",
		"Data processing cost of Cloud NAT Gateway by region and project. Cost represented in USD/GB",
		[]string{"region", "project"},
	)

	VPNGatewayHourlyGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"vpn_gateway_hourly_rate_usd_per_hour",
		"Hourly cost of VPN Gateway by region and project. Cost represented in USD/hour",
		[]string{"region", "project"},
	)

	PrivateServiceConnectEndpointHourlyGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"private_service_connect_endpoint_hourly_rate_usd_per_hour",
		"Hourly cost of Private Service Connect endpoints by region, project, and type. Cost represented in USD/hour",
		[]string{"region", "project", "endpoint_type"},
	)
	PrivateServiceConnectDataProcessingGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"private_service_connect_data_processing_usd_per_gb",
		"Data processing cost of Private Service Connect by region and project. Cost represented in USD/GB",
		[]string{"region", "project"},
	)
)

// Config holds configuration for the VPC collector
type Config struct {
	Projects       string
	ScrapeInterval time.Duration
	Logger         *slog.Logger
}

// Collector implements provider.Collector for GCP VPC metrics
type Collector struct {
	gcpClient  client.Client
	projects   []string
	pricingMap *VPCPricingMap
	logger     *slog.Logger
	ctx        context.Context
}

// New creates a new VPC collector and starts periodic pricing refresh
func New(ctx context.Context, config *Config, gcpClient client.Client) (*Collector, error) {
	logger := config.Logger.With("collector", "vpc")

	pricingMap := NewVPCPricingMap(logger, gcpClient)

	if err := pricingMap.Refresh(ctx); err != nil {
		logger.Error("Failed to load initial VPC pricing data", "error", err)
	}

	go func() {
		ticker := time.NewTicker(PriceRefreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logger.Info("VPC pricing refresh cancelled")
				return
			case <-ticker.C:
				logger.Info("Refreshing VPC pricing data")
				if err := pricingMap.Refresh(ctx); err != nil {
					logger.Error("Failed to refresh VPC pricing data", "error", err)
				}
			}
		}
	}()

	return &Collector{
		gcpClient:  gcpClient,
		projects:   strings.Split(config.Projects, ","),
		pricingMap: pricingMap,
		logger:     logger,
		ctx:        ctx,
	}, nil
}

// Name returns the name of the collector
func (c *Collector) Name() string {
	return collectorName
}

// Register registers the collector with the provider registry
func (c *Collector) Register(registry provider.Registry) error {
	c.logger.LogAttrs(c.ctx, slog.LevelInfo, "Registering VPC metrics")
	return nil
}

// Describe sends metric descriptors to the channel
func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- CloudNATGatewayHourlyGaugeDesc
	ch <- CloudNATDataProcessingGaugeDesc
	ch <- VPNGatewayHourlyGaugeDesc
	ch <- PrivateServiceConnectEndpointHourlyGaugeDesc
	ch <- PrivateServiceConnectDataProcessingGaugeDesc
	return nil
}

// Collect implements the Prometheus Collector interface
func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	return c.collectMetrics(ctx, ch)
}

// CollectMetrics collects and exports VPC pricing metrics
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	if err := c.collectMetrics(context.Background(), ch); err != nil {
		return 0
	}
	return 1
}

// collectMetrics collects and exports VPC pricing metrics
func (c *Collector) collectMetrics(ctx context.Context, ch chan<- prometheus.Metric) error {
	c.logger.LogAttrs(ctx, slog.LevelInfo, "Collecting VPC metrics")
	start := time.Now()

	if len(c.projects) == 0 {
		c.logger.LogAttrs(ctx, slog.LevelWarn, "No projects configured for VPC collection")
		return nil
	}

	regions, err := c.gcpClient.GetRegions(c.projects[0])
	if err != nil {
		c.logger.LogAttrs(ctx, slog.LevelError, "Failed to get regions", slog.String("error", err.Error()))
		return err
	}

	for _, project := range c.projects {
		for _, region := range regions {
			select {
			case <-ctx.Done():
				c.logger.LogAttrs(ctx, slog.LevelInfo, "VPC collection cancelled", slog.String("processed_regions", "partial"))
				return ctx.Err()
			default:
			}

			regionName := region.Name

			c.collectSimpleMetric(ch, regionName, project, "Cloud NAT Gateway",
				c.pricingMap.GetCloudNATGatewayHourlyRate, CloudNATGatewayHourlyGaugeDesc)

			c.collectSimpleMetric(ch, regionName, project, "Cloud NAT data processing",
				c.pricingMap.GetCloudNATDataProcessingRate, CloudNATDataProcessingGaugeDesc)

			c.collectSimpleMetric(ch, regionName, project, "VPN Gateway",
				c.pricingMap.GetVPNGatewayHourlyRate, VPNGatewayHourlyGaugeDesc)

			c.collectPSCEndpointMetrics(ch, regionName, project)

			c.collectSimpleMetric(ch, regionName, project, "Private Service Connect data processing",
				c.pricingMap.GetPrivateServiceConnectDataProcessingRate, PrivateServiceConnectDataProcessingGaugeDesc)
		}
	}

	c.logger.LogAttrs(ctx, slog.LevelInfo, "Finished VPC collection",
		slog.Duration("duration", time.Since(start)))
	return nil
}

// collectSimpleMetric collects a single metric with standard labels (region, project)
func (c *Collector) collectSimpleMetric(
	ch chan<- prometheus.Metric,
	region, project, metricName string,
	getRate func(string) (float64, error),
	desc *prometheus.Desc,
) {
	rate, err := getRate(region)
	if err != nil {
		c.logger.Debug(fmt.Sprintf("No %s pricing available", metricName), "region", region, "project", project, "error", err)
		return
	}
	ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, rate, region, project)
}

// collectPSCEndpointMetrics collects Private Service Connect endpoint metrics with endpoint_type label
func (c *Collector) collectPSCEndpointMetrics(ch chan<- prometheus.Metric, region, project string) {
	rates, err := c.pricingMap.GetPrivateServiceConnectEndpointRates(region)
	if err != nil {
		c.logger.Debug("No Private Service Connect endpoint pricing available", "region", region, "project", project, "error", err)
		return
	}

	for endpointType, rate := range rates {
		ch <- prometheus.MustNewConstMetric(
			PrivateServiceConnectEndpointHourlyGaugeDesc,
			prometheus.GaugeValue,
			rate,
			region,
			project,
			endpointType,
		)
	}
}
