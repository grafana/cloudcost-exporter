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

	"github.com/grafana/cloudcost-exporter/pkg/google/compute"
	"github.com/grafana/cloudcost-exporter/pkg/google/gcs"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
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

	billingService, err := billingv1.NewCloudCatalogClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("error creating billing billingService: %w", err)
	}

	regionsClient, err := computeapiv1.NewRegionsRESTClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not create regions client: %v", err)
	}

	storageClient, err := storage.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not create bucket client: %v", err)
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
			}, billingService, regionsClient, storageClient)
			if err != nil {
				log.Printf("Error creating GCS collector: %s", err)
				continue
			}
		case "GKE":
			collector = compute.New(&compute.Config{
				Projects: config.Projects,
			}, computeService, billingService)
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

// RegisterCollectors will iterate over all of the collectors instantiated during New and register their metrics.
func (g *GCP) RegisterCollectors(registry *prometheus.Registry) error {
	for _, c := range g.collectors {
		if err := c.Register(registry); err != nil {
			return err
		}
	}
	return nil
}

// CollectMetrics will collect metrics from all collectors available on the Provider
func (g *GCP) CollectMetrics() error {
	wg := sync.WaitGroup{}
	wg.Add(len(g.collectors))
	for _, c := range g.collectors {
		go func(c provider.Collector) {
			log.Printf("Collecting metrics from %s", c.Name())
			defer wg.Done()
			if err := c.Collect(); err != nil {
				log.Printf("Error collecting metrics from %s: %s", c.Name(), err)
			}
		}(c)
	}
	wg.Wait()
	return nil
}
