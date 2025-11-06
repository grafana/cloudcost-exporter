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
	ch <- PrivateServiceConnectEndpointHourlyGaugeDesc
	ch <- PrivateServiceConnectDataProcessingGaugeDesc
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

	for _, project := range c.projects {
		for _, region := range regions {
			select {
			case <-c.ctx.Done():
				c.logger.LogAttrs(c.ctx, slog.LevelInfo, "VPC collection cancelled", slog.String("processed_regions", "partial"))
				return 0
			default:
			}

			regionName := region.Name

			natGatewayRate, err := c.pricingMap.GetCloudNATGatewayHourlyRate(regionName)
			if err != nil {
				c.logger.Debug("No Cloud NAT Gateway pricing available", "region", regionName, "project", project, "error", err)
			} else {
				ch <- prometheus.MustNewConstMetric(
					CloudNATGatewayHourlyGaugeDesc,
					prometheus.GaugeValue,
					natGatewayRate,
					regionName,
					project,
				)
			}

			natDataProcessingRate, err := c.pricingMap.GetCloudNATDataProcessingRate(regionName)
			if err != nil {
				c.logger.Debug("No Cloud NAT data processing pricing available", "region", regionName, "project", project, "error", err)
			} else {
				ch <- prometheus.MustNewConstMetric(
					CloudNATDataProcessingGaugeDesc,
					prometheus.GaugeValue,
					natDataProcessingRate,
					regionName,
					project,
				)
			}

			vpnGatewayRate, err := c.pricingMap.GetVPNGatewayHourlyRate(regionName)
			if err != nil {
				c.logger.Debug("No VPN Gateway pricing available", "region", regionName, "project", project, "error", err)
			} else {
				ch <- prometheus.MustNewConstMetric(
					VPNGatewayHourlyGaugeDesc,
					prometheus.GaugeValue,
					vpnGatewayRate,
					regionName,
					project,
				)
			}

			// Private Service Connect - Endpoint rates by type
			pscEndpointRates, err := c.pricingMap.GetPrivateServiceConnectEndpointRates(regionName)
			if err != nil {
				c.logger.Debug("No Private Service Connect endpoint pricing available", "region", regionName, "project", project, "error", err)
			} else {
				for endpointType, rate := range pscEndpointRates {
					ch <- prometheus.MustNewConstMetric(
						PrivateServiceConnectEndpointHourlyGaugeDesc,
						prometheus.GaugeValue,
						rate,
						regionName,
						project,
						endpointType,
					)
				}
			}

			// Private Service Connect - Data processing
			pscDataProcessingRate, err := c.pricingMap.GetPrivateServiceConnectDataProcessingRate(regionName)
			if err != nil {
				c.logger.Debug("No Private Service Connect data processing pricing available", "region", regionName, "project", project, "error", err)
			} else {
				ch <- prometheus.MustNewConstMetric(
					PrivateServiceConnectDataProcessingGaugeDesc,
					prometheus.GaugeValue,
					pscDataProcessingRate,
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
