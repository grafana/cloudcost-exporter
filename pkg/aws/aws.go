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
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/prometheus/client_golang/prometheus"

	ec2Collector "github.com/grafana/cloudcost-exporter/pkg/aws/ec2"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/aws/s3"
	ec2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

type Config struct {
	Services       []string
	Region         string
	Profile        string
	ScrapeInterval time.Duration
	Logger         *slog.Logger
}

type AWS struct {
	Config     *Config
	collectors []provider.Collector
	logger     *slog.Logger
}

var (
	providerLastScrapeErrorDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "", "last_scrape_error"),
		"Was the last scrape an error. 1 indicates an error.",
		[]string{"provider"},
		nil,
	)
	collectorSuccessDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "collector_success"),
		"Was the last scrape of the AWS metrics successful.",
		[]string{"collector"},
		nil,
	)
	collectorLastScrapeErrorDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "last_scrape_error"),
		"Was the last scrape an error. 1 indicates an error.",
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
	providerLastScrapeTime = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "", "last_scrape_time"),
		"Time of the last scrape.",
		[]string{"provider"},
		nil,
	)
	providerLastScrapeDurationDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "", "last_scrape_duration_seconds"),
		"Duration of the last scrape in seconds.",
		[]string{"provider"},
		nil,
	)
	providerScrapesTotalCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, "", "scrapes_total"),
			Help: "Total number of scrapes.",
		},
		[]string{"provider"},
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
	options = append(options, awsconfig.WithRetryMaxAttempts(maxRetryAttempts))
	ac, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}
	for _, service := range config.Services {
		switch strings.ToUpper(service) {
		case "S3":
			client := costexplorer.NewFromConfig(ac)
			collector := s3.New(config.ScrapeInterval, client)
			collectors = append(collectors, collector)
		case "EC2":
			pricingService := pricing.NewFromConfig(ac)
			computeService := ec2.NewFromConfig(ac)
			regions, err := computeService.DescribeRegions(ctx, &ec2.DescribeRegionsInput{AllRegions: aws.Bool(false)})
			if err != nil {
				return nil, fmt.Errorf("error getting regions: %w", err)
			}
			regionClientMap := make(map[string]ec2client.EC2)
			for _, r := range regions.Regions {
				client, err := newEc2Client(*r.RegionName, config.Profile)
				if err != nil {
					return nil, fmt.Errorf("error creating ec2 client: %w", err)
				}
				regionClientMap[*r.RegionName] = client
			}
			collector := ec2Collector.New(&ec2Collector.Config{
				Regions:        regions.Regions,
				RegionClients:  regionClientMap,
				Logger:         logger,
				ScrapeInterval: config.ScrapeInterval,
			}, pricingService)

			// gc code
			collector.DumpPricingMapsToCSV()
			// end of gc code
			collectors = append(collectors, collector)
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
	ch <- providerLastScrapeErrorDesc
	ch <- providerLastScrapeDurationDesc
	ch <- collectorLastScrapeTime
	ch <- providerLastScrapeTime
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
	start := time.Now()
	wg := &sync.WaitGroup{}
	wg.Add(len(a.collectors))
	for _, c := range a.collectors {
		go func(c provider.Collector) {
			now := time.Now()
			defer wg.Done()
			collectorErrors := 0.0
			if err := c.Collect(ch); err != nil {
				collectorErrors = 1.0
				a.logger.LogAttrs(context.Background(), slog.LevelError, "could not collect metrics",
					slog.String("collector", c.Name()),
					slog.String("message", err.Error()),
				)
			}
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeErrorDesc, prometheus.GaugeValue, collectorErrors, subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, time.Since(now).Seconds(), subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorSuccessDesc, prometheus.GaugeValue, collectorErrors, c.Name())
			collectorScrapesTotalCounter.WithLabelValues(subsystem, c.Name()).Inc()
		}(c)
	}
	wg.Wait()
	ch <- prometheus.MustNewConstMetric(providerLastScrapeErrorDesc, prometheus.GaugeValue, 0.0, subsystem)
	ch <- prometheus.MustNewConstMetric(providerLastScrapeDurationDesc, prometheus.GaugeValue, time.Since(start).Seconds(), subsystem)
	ch <- prometheus.MustNewConstMetric(providerLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem)
	providerScrapesTotalCounter.WithLabelValues(subsystem).Inc()
}

func (a *AWS) CheckReadiness() bool {
	for _, c := range a.collectors {
		if !c.CheckReadiness() {
			return false
		}
	}
	return true
}

func newEc2Client(region, profile string) (*ec2.Client, error) {
	options := []func(*awsconfig.LoadOptions) error{awsconfig.WithEC2IMDSRegion()}
	options = append(options, awsconfig.WithRegion(region))
	if profile != "" {
		options = append(options, awsconfig.WithSharedConfigProfile(profile))
	}
	// Set max retries to 10. Throttling is possible after fetching the pricing data, so setting it to 10 ensures the next scrape will be successful.
	options = append(options, awsconfig.WithRetryMaxAttempts(maxRetryAttempts))
	ac, err := awsconfig.LoadDefaultConfig(context.Background(), options...)
	if err != nil {
		return nil, err
	}

	return ec2.NewFromConfig(ac), nil
}
