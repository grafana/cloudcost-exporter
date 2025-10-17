package client

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbTypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	c "github.com/grafana/cloudcost-exporter/pkg/aws/services/costexplorer"
	e "github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
	elbv2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/elbv2"
	p "github.com/grafana/cloudcost-exporter/pkg/aws/services/pricing"
	"github.com/prometheus/client_golang/prometheus"
)

type Config struct {
	PricingService p.Pricing
	EC2Service     e.EC2
	BillingService c.CostExplorer
	RDSService     *rds.Client
	ELBService     elbv2client.ELBv2
}

type AWSClient struct {
	priceService   *pricing
	computeService *compute
	billing        *billing
	rdsService     *rdsService
	elbService     *elb
	metrics        *Metrics
}

func NewAWSClient(cfg Config) *AWSClient {
	m := NewMetrics()
	return &AWSClient{
		priceService:   newPricing(cfg.PricingService, cfg.EC2Service),
		computeService: newCompute(cfg.EC2Service),
		billing:        newBilling(cfg.BillingService, m),
		elbService:     newELB(cfg.ELBService),
		rdsService:     newRDS(cfg.RDSService),
		metrics:        m,
	}
}

func (c *AWSClient) Metrics() []prometheus.Collector {
	return []prometheus.Collector{c.metrics.RequestCount, c.metrics.RequestErrorsCount}
}

func (c *AWSClient) GetBillingData(ctx context.Context, startDate time.Time, endDate time.Time) (*BillingData, error) {
	return c.billing.getBillingData(ctx, startDate, endDate)
}

func (c *AWSClient) DescribeRegions(ctx context.Context, allRegions bool) ([]types.Region, error) {
	return c.computeService.describeRegions(ctx, allRegions)
}

func (c *AWSClient) ListComputeInstances(ctx context.Context) ([]types.Reservation, error) {
	return c.computeService.listComputeInstances(ctx)
}

func (c *AWSClient) ListEBSVolumes(ctx context.Context) ([]types.Volume, error) {
	return c.computeService.listEBSVolumes(ctx)
}

func (c *AWSClient) ListSpotPrices(ctx context.Context) ([]types.SpotPrice, error) {
	return c.priceService.listSpotPrices(ctx)
}

func (c *AWSClient) ListOnDemandPrices(ctx context.Context, region string) ([]string, error) {
	return c.priceService.listOnDemandPrices(ctx, region)
}

func (c *AWSClient) ListStoragePrices(ctx context.Context, region string) ([]string, error) {
	return c.priceService.listStoragePrices(ctx, region)
}

func (c *AWSClient) ListEC2ServicePrices(ctx context.Context, region string, filters []pricingTypes.Filter) ([]string, error) {
	return c.priceService.listEC2ServicePrices(ctx, region, filters)
}

func (c *AWSClient) ListELBPrices(ctx context.Context, region string) ([]string, error) {
	return c.priceService.listELBPrices(ctx, region)
}

func (c *AWSClient) DescribeLoadBalancers(ctx context.Context) ([]elbTypes.LoadBalancer, error) {
	return c.elbService.describeLoadBalancers(ctx)
}

func (c *AWSClient) ListRDSInstances(ctx context.Context) ([]rdsTypes.DBInstance, error) {
	return c.rdsService.listRDSInstances(ctx)
}

func (c *AWSClient) GetRDSUnitData(ctx context.Context, instType, region, deploymentOption, databaseEngine, locationType string) (string, error) {
	return c.priceService.getRDSUnitData(ctx, instType, region, deploymentOption, databaseEngine, locationType)
}

func (c *AWSClient) ListVPCServicePrices(ctx context.Context, region string, filters []pricingTypes.Filter) ([]string, error) {
	return c.priceService.listVPCServicePrices(ctx, region, filters)
}
