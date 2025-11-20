package vpc

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/prometheus/client_golang/prometheus"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	awsclient "github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

const (
	serviceName = "vpc"
)

const PriceRefreshInterval = 24 * time.Hour

var (
	subsystem = fmt.Sprintf("aws_%s", serviceName)

	VPCEndpointHourlyGaugeDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"endpoint_hourly_rate_usd_per_hour",
		"Hourly cost of VPC endpoints by region and type. Cost represented in USD/hour",
		[]string{"region", "endpoint_type"},
	)
	VPCEndpointServiceHourlyGaugeDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"endpoint_service_hourly_rate_usd_per_hour",
		"Hourly cost of VPC service endpoints by region. Cost represented in USD/hour",
		[]string{"region"},
	)
	TransitGatewayHourlyGaugeDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"transit_gateway_hourly_rate_usd_per_hour",
		"Hourly cost of Transit Gateway by region. Cost represented in USD/hour",
		[]string{"region"},
	)
	ElasticIPInUseGaugeDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"elastic_ip_in_use_hourly_rate_usd_per_hour",
		"Hourly cost of in-use Elastic IPs by region. Cost represented in USD/hour",
		[]string{"region"},
	)
	ElasticIPIdleGaugeDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		"elastic_ip_idle_hourly_rate_usd_per_hour",
		"Hourly cost of idle Elastic IPs by region. Cost represented in USD/hour",
		[]string{"region"},
	)
)

// Collector implements provider.Collector
type Collector struct {
	regions        []ec2Types.Region
	scrapeInterval time.Duration
	pricingMap     *VPCPricingMap
	logger         *slog.Logger
	ctx            context.Context
}

func New(ctx context.Context, config *Config) *Collector {
	logger := config.Logger.With("logger", serviceName)
	pricingMap := NewVPCPricingMap(logger)

	// Initial pricing data load using the dedicated us-east-1 pricing client
	if err := pricingMap.Refresh(ctx, config.Regions, config.Client); err != nil {
		logger.Error("Failed to load initial VPC pricing data", "error", err)
	}

	// Set up periodic pricing refresh
	go func() {
		ticker := time.NewTicker(PriceRefreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				logger.Info("Refreshing VPC pricing data")
				if err := pricingMap.Refresh(ctx, config.Regions, config.Client); err != nil {
					logger.Error("Failed to refresh VPC pricing data", "error", err)
				}
			}
		}
	}()

	return &Collector{
		regions:        config.Regions,
		scrapeInterval: config.ScrapeInterval,
		pricingMap:     pricingMap,
		logger:         logger,
		ctx:            ctx, // Store the context for cancellation checks
	}
}

type Config struct {
	ScrapeInterval time.Duration
	Regions        []ec2Types.Region
	Logger         *slog.Logger
	Client         awsclient.Client // Dedicated client with us-east-1 pricing service
}

func (c *Collector) Name() string { return strings.ToUpper(serviceName) }

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- VPCEndpointHourlyGaugeDesc
	ch <- VPCEndpointServiceHourlyGaugeDesc
	ch <- TransitGatewayHourlyGaugeDesc
	ch <- ElasticIPInUseGaugeDesc
	ch <- ElasticIPIdleGaugeDesc
	return nil
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	c.logger.LogAttrs(ctx, slog.LevelInfo, "calling collect")

	// Collect VPC Endpoint pricing for all regions
	for _, region := range c.regions {
		// Check if context is cancelled (e.g., during shutdown)
		select {
		case <-ctx.Done():
			c.logger.Info("Collect cancelled, stopping region iteration", "processed_regions", "partial")
			return ctx.Err()
		default:
			// Continue with collection
		}

		regionName := *region.RegionName

		// VPC Endpoint metrics - both standard and service-specific
		vpcEndpointRate, err := c.pricingMap.GetVPCEndpointHourlyRate(regionName)
		if err != nil {
			c.logger.Warn("No VPC endpoint pricing available", "region", regionName, "error", err)
		} else {
			ch <- prometheus.MustNewConstMetric(
				VPCEndpointHourlyGaugeDesc,
				prometheus.GaugeValue,
				vpcEndpointRate,
				regionName,
				"standard",
			)
		}

		// VPC Service Endpoint metrics
		vpcServiceEndpointRate, err := c.pricingMap.GetVPCServiceEndpointHourlyRate(regionName)
		if err != nil {
			c.logger.Warn("No VPC service endpoint pricing available", "region", regionName, "error", err)
		} else {
			ch <- prometheus.MustNewConstMetric(
				VPCEndpointServiceHourlyGaugeDesc,
				prometheus.GaugeValue,
				vpcServiceEndpointRate,
				regionName,
			)
		}

		// Transit Gateway metrics
		transitGatewayRate, err := c.pricingMap.GetTransitGatewayHourlyRate(regionName)
		if err != nil {
			c.logger.Warn("No Transit Gateway pricing available", "region", regionName, "error", err)
		} else {
			ch <- prometheus.MustNewConstMetric(
				TransitGatewayHourlyGaugeDesc,
				prometheus.GaugeValue,
				transitGatewayRate,
				regionName,
			)
		}

		// Elastic IP (In Use) metrics
		elasticIPInUseRate, err := c.pricingMap.GetElasticIPInUseRate(regionName)
		if err != nil {
			c.logger.Warn("No Elastic IP in-use pricing available", "region", regionName, "error", err)
		} else {
			ch <- prometheus.MustNewConstMetric(
				ElasticIPInUseGaugeDesc,
				prometheus.GaugeValue,
				elasticIPInUseRate,
				regionName,
			)
		}

		// Elastic IP (Idle) metrics
		elasticIPIdleRate, err := c.pricingMap.GetElasticIPIdleRate(regionName)
		if err != nil {
			c.logger.Warn("No Elastic IP idle pricing available", "region", regionName, "error", err)
		} else {
			ch <- prometheus.MustNewConstMetric(
				ElasticIPIdleGaugeDesc,
				prometheus.GaugeValue,
				elasticIPIdleRate,
				regionName,
			)
		}
	}

	return nil
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	return 0
}

func (c *Collector) Register(registry provider.Registry) error { return nil }
