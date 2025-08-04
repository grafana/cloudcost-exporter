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
	"github.com/aws/aws-sdk-go-v2/service/pricing"
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
	priceService   *pricing.Client
	computeService *ec2.Client
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
	
	m := NewMetrics()
	
	return &AWSClient{
		priceService:   pricing.NewFromConfig(ac),
		computeService: ec2.NewFromConfig(ac),
		billing:        newBilling(costexplorer.NewFromConfig(ac), m),
		metrics:        m,
	}, nil
}

func (c *AWSClient) Metrics() []prometheus.Collector {
	return []prometheus.Collector{c.metrics.RequestCount, c.metrics.RequestErrorsCount}
}

func (c *AWSClient) DescribeRegions(ctx context.Context, allRegions bool) ([]types.Region, error) {
	regions, err := c.computeService.DescribeRegions(ctx, &ec2.DescribeRegionsInput{AllRegions: aws.Bool(allRegions)})
	if err != nil {
		return nil, err
	}
	
	return regions.Regions, nil
}

func (c *AWSClient) GetBillingData(ctx context.Context, startDate time.Time, endDate time.Time) (*BillingData, error) {
	return c.billing.getBillingData(ctx, startDate, endDate)
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
