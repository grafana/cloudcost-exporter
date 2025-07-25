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
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	awsrds "github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	ec2Collector "github.com/grafana/cloudcost-exporter/pkg/aws/ec2"
	"github.com/grafana/cloudcost-exporter/pkg/aws/rds"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/aws/s3"
	ec2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

type Config struct {
	Services       []string
	Region         string
	Profile        string
	RoleARN        string
	ScrapeInterval time.Duration
	Logger         *slog.Logger
}

type AWS struct {
	Config     *Config
	collectors []provider.Collector
	logger     *slog.Logger
}

var (
	collectorSuccessDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "success"),
		"Count the number of successful scrapes for a collector.",
		[]string{"provider", "collector"},
		nil,
	)
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
	collectorScrapesTotalCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "scrapes_total"),
			Help: "Total number of scrapes for a collector.",
		},
		[]string{"provider", "collector"},
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
)

func New(ctx context.Context, config *Config) (*AWS, error) {
	var collectors []provider.Collector
	logger := config.Logger.With("provider", "aws")
	// There are two scenarios:
	// 1. Running locally, the user must pass in a region and profile to use
	// 2. Running within an EC2 instance and the region and profile can be derived
	// I'm going to use the AWS SDK to handle this for me. If the user has provided a region and profile, it will use that.
	// If not, it will use the EC2 instance metadata service to determine the region and credentials.
	// This is the same logic that the AWS CLI uses, so it should be fine.
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithEC2IMDSRegion()}
	if config.Region != "" {
		options = append(options, awsconfig.WithRegion(config.Region))
	}
	if config.Profile != "" {
		options = append(options, awsconfig.WithSharedConfigProfile(config.Profile))
	}
	if config.RoleARN != "" {
		var err error
		options, err = assumeRole(config.RoleARN, options)
		if err != nil {
			return nil, err
		}
	}
	options = append(options, awsconfig.WithRetryMaxAttempts(maxRetryAttempts))
	ac, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}
	var pricingService *pricing.Client
	var regions *ec2.DescribeRegionsOutput
	var awsCfg *aws.Config
	for _, service := range config.Services {
		// region API is shared between EC2 and RDS
		if service == "RDS" || service == "EC2" {
			pricingService = pricing.NewFromConfig(ac)
			computeService := ec2.NewFromConfig(ac)
			regions, err = computeService.DescribeRegions(ctx, &ec2.DescribeRegionsInput{AllRegions: aws.Bool(false)})
			if err != nil {
				return nil, fmt.Errorf("error getting regions: %w", err)
			}
			for _, r := range regions.Regions {
				awsCfg, err = newAWSConfig(*r.RegionName, config.Profile, config.RoleARN)
				if err != nil {
					return nil, fmt.Errorf("error creating aws config: %w", err)
				}
			}
		}
		switch strings.ToUpper(service) {
		case "S3":
			client := costexplorer.NewFromConfig(ac)
			collector := s3.New(config.ScrapeInterval, client)
			collectors = append(collectors, collector)
		case "EC2":
			regionClientMap := make(map[string]ec2client.EC2)
			for _, r := range regions.Regions {
				ec2Client := ec2.NewFromConfig(*awsCfg)
				regionClientMap[*r.RegionName] = ec2Client
			}
			collector := ec2Collector.New(&ec2Collector.Config{
				Regions:        regions.Regions,
				RegionClients:  regionClientMap,
				Logger:         logger,
				ScrapeInterval: config.ScrapeInterval,
			}, pricingService)
			collectors = append(collectors, collector)
		case "RDS":
			regionMap := make(map[string]awsrds.Client)
			for _, r := range regions.Regions {
				rdsClient := awsrds.NewFromConfig(*awsCfg)
				regionMap[*r.RegionName] = *rdsClient
			}
			_ = rds.New(&rds.Config{
				ScrapeInterval: config.ScrapeInterval,
				Logger:         logger,
				RegionClients:  regionMap,
			}, pricingService)
			// collectors = append(collectors, collector)
		default:
			logger.LogAttrs(ctx, slog.LevelWarn, "unknown server, skipping",
				slog.String("service", service),
			)
			continue
		}
	}
	return &AWS{
		Config:     config,
		collectors: collectors,
		logger:     logger,
	}, nil
}

func (a *AWS) RegisterCollectors(registry provider.Registry) error {
	a.logger.LogAttrs(context.Background(), slog.LevelInfo, "registering collectors",
		slog.Int("count", len(a.collectors)),
	)
	registry.MustRegister(
		collectorScrapesTotalCounter,
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
	ch <- collectorSuccessDesc
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
	wg := &sync.WaitGroup{}
	wg.Add(len(a.collectors))
	for _, c := range a.collectors {
		go func(c provider.Collector) {
			now := time.Now()
			defer wg.Done()
			collectorErrors := 0.0
			if err := c.Collect(ch); err != nil {
				collectorErrors++
				a.logger.LogAttrs(context.Background(), slog.LevelError, "could not collect metrics",
					slog.String("collector", c.Name()),
					slog.String("message", err.Error()),
				)
			}
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeErrorDesc, prometheus.CounterValue, collectorErrors, subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, time.Since(now).Seconds(), subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem, c.Name())
			collectorScrapesTotalCounter.WithLabelValues(subsystem, c.Name()).Inc()

			counter := collectorScrapesTotalCounter.WithLabelValues(subsystem, c.Name())
			totalMetricCount := &dto.Metric{}
			counter.Write(totalMetricCount)
			ch <- prometheus.MustNewConstMetric(collectorSuccessDesc, prometheus.CounterValue, totalMetricCount.GetCounter().GetValue()-collectorErrors, subsystem, c.Name())
		}(c)
	}
	wg.Wait()
}

func newAWSConfig(region, profile, roleARN string) (*aws.Config, error) {
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithEC2IMDSRegion()}
	options = append(options, awsconfig.WithRegion(region))
	if profile != "" {
		options = append(options, awsconfig.WithSharedConfigProfile(profile))
	}
	// Set max retries to 10. Throttling is possible after fetching the pricing data, so setting it to 10 ensures the next scrape will be successful.
	options = append(options, awsconfig.WithRetryMaxAttempts(maxRetryAttempts))

	if roleARN != "" {
		var err error
		options, err = assumeRole(roleARN, options)
		if err != nil {
			return nil, err
		}
	}

	ac, err := awsconfig.LoadDefaultConfig(context.Background(), options...)
	if err != nil {
		return nil, err
	}
	return &ac, nil
}

func assumeRole(roleARN string, options []func(*awsconfig.LoadOptions) error) ([]func(*awsconfig.LoadOptions) error, error) {
	// Add the credentials to assume the role specified in config.RoleARN
	ac, err := awsconfig.LoadDefaultConfig(context.Background(), options...)
	if err != nil {
		return nil, err
	}

	stsService := sts.NewFromConfig(ac)

	options = append(options, awsconfig.WithCredentialsProvider(
		aws.NewCredentialsCache(
			stscreds.NewAssumeRoleProvider(
				stsService,
				roleARN,
			),
		),
	))

	return options, nil
}
