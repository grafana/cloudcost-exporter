package client

import (
	"context"
	"time"
	
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	awsPricing "github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/prometheus/client_golang/prometheus"
)

const maxRetryAttempts = 10

type Option func(client []func(options *awsconfig.LoadOptions) error)

func WithRegion(region string) Option {
	return func(options []func(options *awsconfig.LoadOptions) error) {
		options = append(options, awsconfig.WithRegion(region))
	}
}

func WithProfile(profile string) Option {
	return func(options []func(options *awsconfig.LoadOptions) error) {
		options = append(options, awsconfig.WithSharedConfigProfile(profile))
	}
}

func WithRoleARN(roleARN string) Option {
	return func(options []func(options *awsconfig.LoadOptions) error) {
		option, err := assumeRole(roleARN, options)
		if err != nil {
			return
		}
		
		options = append(options, option)
	}
}

type Config struct {
	Region  string
	Profile string
	RoleARN string
}

type AWSClient struct {
	priceService   *pricing
	computeService *compute
	billing        *billing
	metrics        *Metrics
}

func NewAWSClient(ctx context.Context, opts ...Option) (*AWSClient, error) {
	optionsFunc := make([]func(options *awsconfig.LoadOptions) error, 0)
	optionsFunc = append(optionsFunc, awsconfig.WithEC2IMDSRegion())
	optionsFunc = append(optionsFunc, awsconfig.WithRetryMaxAttempts(maxRetryAttempts))
	
	for _, opt := range opts {
		opt(optionsFunc)
	}
	
	ac, err := awsconfig.LoadDefaultConfig(ctx, optionsFunc...)
	if err != nil {
		return nil, err
	}
	
	ec2Service := ec2.NewFromConfig(ac)
	m := NewMetrics()
	
	return &AWSClient{
		priceService:   newPricing(awsPricing.NewFromConfig(ac), ec2Service),
		computeService: newCompute(ec2Service),
		billing:        newBilling(costexplorer.NewFromConfig(ac), m),
		metrics:        m,
	}, nil
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

func assumeRole(roleARN string, options []func(*awsconfig.LoadOptions) error) (awsconfig.LoadOptionsFunc, error) {
	// Add the credentials to assume the role specified in config.RoleARN
	ac, err := awsconfig.LoadDefaultConfig(context.Background(), options...)
	if err != nil {
		return nil, err
	}
	
	stsService := sts.NewFromConfig(ac)
	
	return awsconfig.WithCredentialsProvider(
		aws.NewCredentialsCache(
			stscreds.NewAssumeRoleProvider(
				stsService,
				roleARN,
			),
		),
	), nil
}
