package aws

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/prometheus/client_golang/prometheus"

	ec2Collector "github.com/grafana/cloudcost-exporter/pkg/aws/ec2"
	"github.com/grafana/cloudcost-exporter/pkg/aws/rds"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/aws/s3"
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
	serviceS3  = "S3"
	serviceEC2 = "EC2"
	serviceRDS = "RDS"
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
	awsClient, err := client.NewAWSClient(ctx,
		client.WithRegion(config.Region),
		client.WithProfile(config.Profile),
		client.WithRoleARN(config.RoleARN))

	if err != nil {
		return nil, err
	}
	var regions []types.Region
	for _, service := range config.Services {
		service = strings.ToUpper(service)
		// region API is shared between EC2 and RDS
		if service == serviceRDS || service == serviceEC2 {
			regions, err = awsClient.DescribeRegions(ctx, false)
			if err != nil {
				return nil, fmt.Errorf("error getting regions: %w", err)
			}
		}

		switch service {
		case serviceS3:
			collector := s3.New(config.ScrapeInterval, awsClient)
			collectors = append(collectors, collector)
		case serviceEC2:
			collector := ec2Collector.New(&ec2Collector.Config{
				Regions:        regions,
				Logger:         logger,
				ScrapeInterval: config.ScrapeInterval,
			}, awsClient)
			collectors = append(collectors, collector)
		case serviceRDS:
			_ = rds.New(&rds.Config{
				ScrapeInterval: config.ScrapeInterval,
				Logger:         logger,
				Regions:        regions,
			}, awsClient)
			// TODO: append new aws rds collectors next
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
		}(c)
	}
	wg.Wait()
}
