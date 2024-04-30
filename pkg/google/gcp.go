package google

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	billingv1 "cloud.google.com/go/billing/apiv1"
	computeapiv1 "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/storage"
	"github.com/prometheus/client_golang/prometheus"
	computev1 "google.golang.org/api/compute/v1"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/google/compute"
	"github.com/grafana/cloudcost-exporter/pkg/google/gcs"
	"github.com/grafana/cloudcost-exporter/pkg/google/gke"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "gcp"
)

var (
	providerLastScrapeErrorDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "", "last_scrape_error"),
		"Was the last scrape an error. 1 indicates an error.",
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
)

type GCP struct {
	config     *Config
	collectors []provider.Collector
}

type Config struct {
	ProjectId       string // ProjectID is where the project is running. Used for authentication.
	Region          string
	Projects        string // Projects is a comma-separated list of projects to scrape metadata from
	Services        []string
	ScrapeInterval  time.Duration
	DefaultDiscount int
}

// New is responsible for parsing out a configuration file and setting up the associated services that could be required.
// We instantiate services to avoid repeating common services that may be shared across many collectors. In the future we can push
// collector specific services further down.
func New(config *Config) (*GCP, error) {
	ctx := context.Background()

	computeService, err := computev1.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("error creating compute computeService: %w", err)
	}

	cloudCatalogClient, err := billingv1.NewCloudCatalogClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("error creating cloudCatalogClient: %w", err)
	}

	regionsClient, err := computeapiv1.NewRegionsRESTClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not create regions client: %w", err)
	}

	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not create bucket client: %w", err)
	}

	var collectors []provider.Collector
	for _, service := range config.Services {
		log.Printf("Creating collector for %s", service)
		var collector provider.Collector
		switch strings.ToUpper(service) {
		case "GCS":
			collector, err = gcs.New(&gcs.Config{
				ProjectId:       config.ProjectId,
				Projects:        config.Projects,
				ScrapeInterval:  config.ScrapeInterval,
				DefaultDiscount: config.DefaultDiscount,
			}, cloudCatalogClient, regionsClient, storageClient)
			if err != nil {
				log.Printf("Error creating GCS collector: %s", err)
				continue
			}
		case "COMPUTE":
			collector = compute.New(&compute.Config{
				Projects:       config.Projects,
				ScrapeInterval: config.ScrapeInterval,
			}, computeService, cloudCatalogClient)
		case "GKE":
			collector = gke.New(&gke.Config{
				Projects:       config.Projects,
				ScrapeInterval: config.ScrapeInterval,
			}, computeService, cloudCatalogClient)
		default:
			log.Printf("Unknown service %s", service)
			// Continue to next service, no need to halt here
			continue
		}
		collectors = append(collectors, collector)
	}
	return &GCP{
		config:     config,
		collectors: collectors,
	}, nil
}

// RegisterCollectors will iterate over all the collectors instantiated during New and register their metrics.
func (g *GCP) RegisterCollectors(registry provider.Registry) error {
	registry.MustRegister(providerScrapesTotalCounter)
	registry.MustRegister(collectorScrapesTotalCounter)
	for _, c := range g.collectors {
		if err := c.Register(registry); err != nil {
			return err
		}
	}
	return nil
}

// Describe implements the prometheus.Collector interface and will iterate over all the collectors instantiated during New and describe their metrics.
func (g *GCP) Describe(ch chan<- *prometheus.Desc) {
	ch <- collectorLastScrapeErrorDesc
	ch <- collectorDurationDesc
	ch <- providerLastScrapeErrorDesc
	ch <- providerLastScrapeDurationDesc
	ch <- collectorLastScrapeTime
	ch <- providerLastScrapeTime
	for _, c := range g.collectors {
		if err := c.Describe(ch); err != nil {
			log.Printf("Error describing collector %s: %s", c.Name(), err)
		}
	}
}

// Collect implements the prometheus.Collector interface and will iterate over all the collectors instantiated during New and collect their metrics.
func (g *GCP) Collect(ch chan<- prometheus.Metric) {
	wg := sync.WaitGroup{}
	wg.Add(len(g.collectors))
	start := time.Now()
	for _, c := range g.collectors {
		go func(c provider.Collector) {
			now := time.Now()
			defer wg.Done()
			collectorSuccess := 0.0
			if err := c.Collect(ch); err != nil {
				log.Printf("Error collecting metrics from collector %s: %s", c.Name(), err)
				collectorSuccess = 1.0
			}
			log.Printf("Collector(%s) collect respose=%.2f", c.Name(), collectorSuccess)
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeErrorDesc, prometheus.GaugeValue, collectorSuccess, subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, time.Since(now).Seconds(), subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem, c.Name())
			collectorScrapesTotalCounter.WithLabelValues(subsystem, c.Name()).Inc()
		}(c)
	}
	wg.Wait()
	// When can the error actually happen? Potentially if all the collectors fail?
	ch <- prometheus.MustNewConstMetric(providerLastScrapeErrorDesc, prometheus.GaugeValue, 0.0, subsystem)
	ch <- prometheus.MustNewConstMetric(providerLastScrapeDurationDesc, prometheus.GaugeValue, time.Since(start).Seconds(), subsystem)
	ch <- prometheus.MustNewConstMetric(providerLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem)
	providerScrapesTotalCounter.WithLabelValues(subsystem).Inc()
}
