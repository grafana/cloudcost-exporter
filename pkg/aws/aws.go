package aws

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/prometheus/client_golang/prometheus"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/aws/s3"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

type Config struct {
	Services       []string
	Region         string
	Profile        string
	ScrapeInterval time.Duration
}

type AWS struct {
	Config     *Config
	collectors []provider.Collector
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
		"Time of the last scrape.W",
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

var services = []string{"S3"}

const (
	subsystem = "aws"
)

func New(config *Config) (*AWS, error) {
	var collectors []provider.Collector
	for _, service := range services {
		switch service {
		case "S3":
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
			ac, err := awsconfig.LoadDefaultConfig(context.Background(), options...)
			if err != nil {
				return nil, err
			}

			client := costexplorer.NewFromConfig(ac)
			collector, err := s3.New(config.ScrapeInterval, client)
			if err != nil {
				return nil, fmt.Errorf("error creating s3 collector: %w", err)
			}
			collectors = append(collectors, collector)
		default:
			log.Printf("Unknown service %s", service)
			continue
		}
	}
	return &AWS{
		Config:     config,
		collectors: collectors,
	}, nil
}

func (a *AWS) RegisterCollectors(registry provider.Registry) error {
	log.Printf("Registering %d collectors for AWS", len(a.collectors))
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
			log.Printf("Error describing collector %s: %s", c.Name(), err)
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
			collectorSuccess := 0.0
			if err := c.Collect(ch); err != nil {
				collectorSuccess = 1.0
				log.Printf("Error collecting metrics from collector %s: %s", c.Name(), err)
			}
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeErrorDesc, prometheus.GaugeValue, collectorSuccess, subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, time.Since(now).Seconds(), subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorSuccessDesc, prometheus.GaugeValue, collectorSuccess, c.Name())
			collectorScrapesTotalCounter.WithLabelValues(subsystem, c.Name()).Inc()
		}(c)
	}
	wg.Wait()
	ch <- prometheus.MustNewConstMetric(providerLastScrapeErrorDesc, prometheus.GaugeValue, 0.0, subsystem)
	ch <- prometheus.MustNewConstMetric(providerLastScrapeDurationDesc, prometheus.GaugeValue, time.Since(start).Seconds(), subsystem)
	ch <- prometheus.MustNewConstMetric(providerLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem)
	providerScrapesTotalCounter.WithLabelValues(subsystem).Inc()
}
