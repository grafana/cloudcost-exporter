package aws

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	awsPricing "github.com/aws/aws-sdk-go-v2/service/pricing"
	rds "github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	rdsCollector "github.com/grafana/cloudcost-exporter/pkg/aws/rds"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	ec2Collector "github.com/grafana/cloudcost-exporter/pkg/aws/ec2"
	"github.com/grafana/cloudcost-exporter/pkg/aws/elb"
	awsgwnat "github.com/grafana/cloudcost-exporter/pkg/aws/natgateway"
	awsvpc "github.com/grafana/cloudcost-exporter/pkg/aws/vpc"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/aws/s3"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

type Config struct {
	Services         []string
	Region           string
	Profile          string
	RoleARN          string
	ScrapeInterval   time.Duration
	CollectorTimeout time.Duration
	Logger           *slog.Logger
}

type AWS struct {
	Config           *Config
	collectors       []provider.Collector
	logger           *slog.Logger
	ctx              context.Context
	collectorTimeout time.Duration
}

var (
	collectorLastScrapeErrorDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "last_scrape_error"),
		"Counter of the number of errors that occurred during the last scrape.",
		[]string{"provider", "collector"},
		nil,
	)
	collectorDurationDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "last_scrape_duration_seconds"),
		"Duration of the last scrape in seconds.",
		[]string{"provider", "collector"},
		nil,
	)
	collectorLastScrapeTime = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "last_scrape_time"),
		"Time of the last scrape.",
		[]string{"provider", "collector"},
		nil,
	)
)

const (
	subsystem        = "aws"
	maxRetryAttempts = 10

	// AWS service names used across the AWS provider.
	serviceS3    = "S3"
	serviceEC2   = "EC2"
	serviceRDS   = "RDS"
	serviceNATGW = "NATGATEWAY"
	serviceELB   = "ELB"
	serviceVPC   = "VPC"
)

func New(ctx context.Context, config *Config) (*AWS, error) {
	// There are two scenarios:
	// 1. Running locally, the user must pass in a region and profile to use
	// 2. Running within an EC2 instance and the region and profile can be derived
	// I'm going to use the AWS SDK to handle this for me. If the user has provided a region and profile, it will use that.
	// If not, it will use the EC2 instance metadata service to determine the region and credentials.
	// This is the same logic that the AWS CLI uses, so it should be fine.
	ac, err := createAWSConfig(ctx, config.Region, config.Profile, config.RoleARN)
	if err != nil {
		return nil, err
	}
	awsClient := client.NewAWSClient(client.Config{
		PricingService: awsPricing.NewFromConfig(ac),
		EC2Service:     ec2.NewFromConfig(ac),
		BillingService: costexplorer.NewFromConfig(ac),
		RDSService:     rds.NewFromConfig(ac),
		ELBService:     elbv2.NewFromConfig(ac),
	})

	// Get regions from the client
	regions, err := awsClient.DescribeRegions(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("error getting regions: %w", err)
	}

	// Create per-region clients
	awsClientPerRegion, err := newRegionClientMap(ctx, ac, regions, config.Profile, config.RoleARN)
	if err != nil {
		return nil, err
	}

	return newWithDependencies(ctx, config, awsClient, awsClientPerRegion, regions, ac)
}

// newWithDependencies creates an AWS provider with all dependencies injected as parameters
func newWithDependencies(ctx context.Context, config *Config, awsClient client.Client, regionClients map[string]client.Client, regions []types.Region, awsConfig aws.Config) (*AWS, error) {
	var collectors []provider.Collector
	logger := config.Logger.With("provider", subsystem)

	for _, service := range config.Services {
		service = strings.ToUpper(service)

		switch service {
		case serviceS3:
			collector := s3.New(config.ScrapeInterval, awsClient)
			collectors = append(collectors, collector)
		case serviceEC2:
			collector, err := ec2Collector.New(ctx, &ec2Collector.Config{
				Regions:        regions,
				Logger:         logger,
				ScrapeInterval: config.ScrapeInterval,
				RegionMap:      regionClients,
			})
			if err != nil {
				logger.LogAttrs(ctx, slog.LevelError, "Error creating collector",
					slog.String("service", service),
					slog.String("message", err.Error()))
				continue
			}
			collectors = append(collectors, collector)
		case serviceRDS:
			// pricing API for RDS client needs to use always the same region
			// as for RDS , the pricing data is only available in the us-east-1
			pricingConfig, err := createAWSConfig(ctx, "us-east-1", config.Profile, config.RoleARN)
			if err != nil {
				return nil, err
			}
			awsRDSClient := client.NewAWSClient(client.Config{
				PricingService: awsPricing.NewFromConfig(pricingConfig),
				RDSService:     rds.NewFromConfig(awsConfig),
			})
			collector := rdsCollector.New(&rdsCollector.Config{
				ScrapeInterval: config.ScrapeInterval,
				Regions:        regions,
				RegionMap:      regionClients,
				Client:         awsRDSClient,
			})
			collectors = append(collectors, collector)
		case serviceNATGW:
			natGwCollector := awsgwnat.New(ctx, &awsgwnat.Config{
				ScrapeInterval: config.ScrapeInterval,
				Logger:         logger,
				Regions:        regions,
				RegionMap:      regionClients,
			})
			collectors = append(collectors, natGwCollector)
		case serviceELB:
			collector := elb.New(&elb.Config{
				Regions:        regions,
				RegionClients:  regionClients,
				ScrapeInterval: config.ScrapeInterval,
				Logger:         logger,
			})
			collectors = append(collectors, collector)
		case serviceVPC:
			// pricing API for VPC client needs to use always the same region
			// as for VPC, the pricing data is only available in the us-east-1
			pricingConfig, err := createAWSConfig(ctx, "us-east-1", config.Profile, config.RoleARN)
			if err != nil {
				return nil, err
			}
			awsVPCClient := client.NewAWSClient(client.Config{
				PricingService: awsPricing.NewFromConfig(pricingConfig),
				EC2Service:     ec2.NewFromConfig(pricingConfig),
			})
			collector := awsvpc.New(ctx, &awsvpc.Config{
				ScrapeInterval: config.ScrapeInterval,
				Logger:         logger,
				Regions:        regions,
				Client:         awsVPCClient,
			})
			collectors = append(collectors, collector)
		default:
			logger.LogAttrs(ctx, slog.LevelWarn, "unknown server, skipping",
				slog.String("service", service),
			)
			continue
		}
	}
	return &AWS{
		Config:           config,
		collectors:       collectors,
		logger:           logger,
		ctx:              ctx,
		collectorTimeout: config.CollectorTimeout,
	}, nil
}

func (a *AWS) RegisterCollectors(registry provider.Registry) error {
	a.logger.LogAttrs(context.Background(), slog.LevelInfo, "registering collectors",
		slog.Int("count", len(a.collectors)),
	)
	for _, c := range a.collectors {
		if err := c.Register(registry); err != nil {
			return err
		}
	}
	return nil
}

func (a *AWS) Describe(ch chan<- *prometheus.Desc) {
	ch <- collectorLastScrapeErrorDesc
	ch <- collectorDurationDesc
	ch <- collectorLastScrapeTime
	for _, c := range a.collectors {
		if err := c.Describe(ch); err != nil {
			a.logger.LogAttrs(context.Background(), slog.LevelError, "failed to describe collector",
				slog.String("message", err.Error()),
				slog.String("collector", c.Name()),
			)
		}
	}
}

func (a *AWS) Collect(ch chan<- prometheus.Metric) {
	// Create a context with timeout for this collection cycle
	collectCtx, cancel := context.WithTimeout(a.ctx, a.collectorTimeout)
	defer cancel()

	wg := &sync.WaitGroup{}
	wg.Add(len(a.collectors))
	for _, c := range a.collectors {
		go func(c provider.Collector) {
			now := time.Now()
			defer wg.Done()
			collectorErrors := 0.0
			if err := c.Collect(collectCtx, ch); err != nil {
				collectorErrors++
				a.logger.LogAttrs(context.Background(), slog.LevelError, "could not collect metrics",
					slog.String("collector", c.Name()),
					slog.String("message", err.Error()),
				)
			}
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeErrorDesc, prometheus.CounterValue, collectorErrors, subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, time.Since(now).Seconds(), subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem, c.Name())
		}(c)
	}
	wg.Wait()
}

func newRegionClientMap(ctx context.Context, globalConfig aws.Config, regions []types.Region, profile string, roleARN string) (map[string]client.Client, error) {
	awsClientPerRegion := make(map[string]client.Client)
	for _, region := range regions {
		ac, err := createAWSConfig(ctx, *region.RegionName, profile, roleARN)
		if err != nil {
			return nil, err
		}
		awsClientPerRegion[*region.RegionName] = client.NewAWSClient(
			client.Config{
				PricingService: awsPricing.NewFromConfig(globalConfig),
				EC2Service:     ec2.NewFromConfig(ac),
				BillingService: costexplorer.NewFromConfig(globalConfig),
				RDSService:     rds.NewFromConfig(ac),
				ELBService:     elbv2.NewFromConfig(ac),
			})
	}

	return awsClientPerRegion, nil
}

func createAWSConfig(ctx context.Context, region, profile, roleARN string) (aws.Config, error) {
	optionsFunc := make([]func(options *awsconfig.LoadOptions) error, 0)
	optionsFunc = append(optionsFunc, awsconfig.WithEC2IMDSRegion())
	optionsFunc = append(optionsFunc, awsconfig.WithRetryMaxAttempts(maxRetryAttempts))

	if region != "" {
		optionsFunc = append(optionsFunc, awsconfig.WithRegion(region))
	}

	if profile != "" {
		optionsFunc = append(optionsFunc, awsconfig.WithSharedConfigProfile(profile))
	}

	if roleARN != "" {
		role, err := assumeRole(roleARN, optionsFunc)
		if err != nil {
			return aws.Config{}, err
		}
		optionsFunc = append(optionsFunc, role)
	}

	return awsconfig.LoadDefaultConfig(ctx, optionsFunc...)
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
