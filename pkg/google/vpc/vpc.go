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

	// Cloud NAT Gateway metrics
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

	// VPN Gateway metrics
	VPNGatewayHourlyGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"vpn_gateway_hourly_rate_usd_per_hour",
		"Hourly cost of VPN Gateway by region and project. Cost represented in USD/hour",
		[]string{"region", "project"},
	)

	// Private Service Connect metrics
	PrivateServiceConnectHourlyGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"private_service_connect_hourly_rate_usd_per_hour",
		"Hourly cost of Private Service Connect endpoints by region and project. Cost represented in USD/hour",
		[]string{"region", "project"},
	)

	// External IP Address metrics
	ExternalIPStaticHourlyGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"external_ip_static_hourly_rate_usd_per_hour",
		"Hourly cost of static external IP addresses by region and project. Cost represented in USD/hour",
		[]string{"region", "project"},
	)

	ExternalIPEphemeralHourlyGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"external_ip_ephemeral_hourly_rate_usd_per_hour",
		"Hourly cost of ephemeral external IP addresses by region and project. Cost represented in USD/hour",
		[]string{"region", "project"},
	)

	// Cloud Router metrics
	CloudRouterHourlyGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"cloud_router_hourly_rate_usd_per_hour",
		"Hourly cost of Cloud Router by region and project. Cost represented in USD/hour",
		[]string{"region", "project"},
	)
)

type Config struct {
	Projects       string
	ScrapeInterval time.Duration
	Logger         *slog.Logger
}

type Collector struct {
	gcpClient  client.Client
	projects   []string
	pricingMap *VPCPricingMap
	logger     *slog.Logger
	ctx        context.Context
}

func New(config *Config, gcpClient client.Client) (*Collector, error) {
	ctx := context.Background()
	logger := config.Logger.With("collector", "vpc")

	// Initialize pricing map
	pricingMap := NewVPCPricingMap(logger, gcpClient)

	// Initial pricing data load
	if err := pricingMap.Refresh(ctx); err != nil {
		logger.Error("Failed to load initial VPC pricing data", "error", err)
	}

	// Set up periodic pricing refresh
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

func (c *Collector) Name() string {
	return collectorName
}

func (c *Collector) Register(registry provider.Registry) error {
	c.logger.LogAttrs(c.ctx, slog.LevelInfo, "Registering VPC metrics")
	return nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- CloudNATGatewayHourlyGaugeDesc
	ch <- CloudNATDataProcessingGaugeDesc
	ch <- VPNGatewayHourlyGaugeDesc
	ch <- PrivateServiceConnectHourlyGaugeDesc
	ch <- ExternalIPStaticHourlyGaugeDesc
	ch <- ExternalIPEphemeralHourlyGaugeDesc
	ch <- CloudRouterHourlyGaugeDesc
	return nil
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	c.CollectMetrics(ch)
	return nil
}

func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	c.logger.LogAttrs(c.ctx, slog.LevelInfo, "Collecting VPC metrics")
	start := time.Now()

	// Get all regions from the first project
	if len(c.projects) == 0 {
		c.logger.LogAttrs(c.ctx, slog.LevelWarn, "No projects configured for VPC collection")
		return 0
	}

	regions, err := c.gcpClient.GetRegions(c.projects[0])
	if err != nil {
		c.logger.LogAttrs(c.ctx, slog.LevelError, "Failed to get regions", slog.String("error", err.Error()))
		return 0
	}

	// Collect metrics for each project and region combination
	for _, project := range c.projects {
		for _, region := range regions {
			select {
			case <-c.ctx.Done():
				c.logger.LogAttrs(c.ctx, slog.LevelInfo, "VPC collection cancelled", slog.String("processed_regions", "partial"))
				return 0
			default:
				// Continue with collection
			}

			regionName := region.Name

			// Cloud NAT Gateway rate
			natGatewayRate, err := c.pricingMap.GetCloudNATGatewayHourlyRate(regionName)
			if err != nil {
				c.logger.Error("No Cloud NAT Gateway pricing available", "region", regionName, "project", project, "error", err)
			} else {
				ch <- prometheus.MustNewConstMetric(
					CloudNATGatewayHourlyGaugeDesc,
					prometheus.GaugeValue,
					natGatewayRate,
					regionName,
					project,
				)
			}

			// Cloud NAT data processing rate
			natDataProcessingRate, err := c.pricingMap.GetCloudNATDataProcessingRate(regionName)
			if err != nil {
				c.logger.Error("No Cloud NAT data processing pricing available", "region", regionName, "project", project, "error", err)
			} else {
				ch <- prometheus.MustNewConstMetric(
					CloudNATDataProcessingGaugeDesc,
					prometheus.GaugeValue,
					natDataProcessingRate,
					regionName,
					project,
				)
			}

			// VPN Gateway rate
			vpnGatewayRate, err := c.pricingMap.GetVPNGatewayHourlyRate(regionName)
			if err != nil {
				c.logger.Error("No VPN Gateway pricing available", "region", regionName, "project", project, "error", err)
			} else {
				ch <- prometheus.MustNewConstMetric(
					VPNGatewayHourlyGaugeDesc,
					prometheus.GaugeValue,
					vpnGatewayRate,
					regionName,
					project,
				)
			}

			// Private Service Connect rate
			pscRate, err := c.pricingMap.GetPrivateServiceConnectHourlyRate(regionName)
			if err != nil {
				c.logger.Error("No Private Service Connect pricing available", "region", regionName, "project", project, "error", err)
			} else {
				ch <- prometheus.MustNewConstMetric(
					PrivateServiceConnectHourlyGaugeDesc,
					prometheus.GaugeValue,
					pscRate,
					regionName,
					project,
				)
			}

			// Static External IP rate
			staticIPRate, err := c.pricingMap.GetExternalIPStaticHourlyRate(regionName)
			if err != nil {
				c.logger.Error("No static external IP pricing available", "region", regionName, "project", project, "error", err)
			} else {
				ch <- prometheus.MustNewConstMetric(
					ExternalIPStaticHourlyGaugeDesc,
					prometheus.GaugeValue,
					staticIPRate,
					regionName,
					project,
				)
			}

			// Ephemeral External IP rate
			ephemeralIPRate, err := c.pricingMap.GetExternalIPEphemeralHourlyRate(regionName)
			if err != nil {
				c.logger.Error("No ephemeral external IP pricing available", "region", regionName, "project", project, "error", err)
			} else {
				ch <- prometheus.MustNewConstMetric(
					ExternalIPEphemeralHourlyGaugeDesc,
					prometheus.GaugeValue,
					ephemeralIPRate,
					regionName,
					project,
				)
			}

			// Cloud Router rate
			routerRate, err := c.pricingMap.GetCloudRouterHourlyRate(regionName)
			if err != nil {
				c.logger.Error("No Cloud Router pricing available", "region", regionName, "project", project, "error", err)
			} else {
				ch <- prometheus.MustNewConstMetric(
					CloudRouterHourlyGaugeDesc,
					prometheus.GaugeValue,
					routerRate,
					regionName,
					project,
				)
			}
		}
	}

	c.logger.LogAttrs(c.ctx, slog.LevelInfo, "Finished VPC collection",
		slog.Duration("duration", time.Since(start)))
	return 1
}
