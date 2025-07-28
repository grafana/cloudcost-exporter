package google

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/prometheus/client_golang/prometheus"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/google/gcs"
	"github.com/grafana/cloudcost-exporter/pkg/google/gke"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const subsystem = "gcp_gcs"

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
		"Time of the last scrape.W",
		[]string{"provider", "collector"},
		nil,
	)
)

type GCP struct {
	config     *Config
	collectors []provider.Collector
	logger     *slog.Logger
}

type Config struct {
	ProjectId       string // ProjectID is where the project is running. Used for authentication.
	Region          string
	Projects        string // Projects is a comma-separated list of projects to scrape metadata from
	Services        []string
	ScrapeInterval  time.Duration
	DefaultDiscount int
	Logger          *slog.Logger
}

// New is responsible for parsing out a configuration file and setting up the associated services that could be required.
// We instantiate services to avoid repeating common services that may be shared across many collectors. In the future we can push
// collector specific services further down.
func New(config *Config) (*GCP, error) {
	ctx := context.Background()
	logger := config.Logger.With("provider", "gcp")

	gpcClient, err := client.NewGPCClient(ctx, client.Config{ProjectId: config.ProjectId, Discount: config.DefaultDiscount})
	if err != nil {
		return nil, err
	}

	var collectors []provider.Collector
	for _, service := range config.Services {
		logger.LogAttrs(ctx, slog.LevelInfo, "Creating service",
			slog.String("service", service))

		var collector provider.Collector
		switch strings.ToUpper(service) {
		case "GCS":
			collector, err = gcs.New(&gcs.Config{
				ProjectId:       config.ProjectId,
				Projects:        config.Projects,
				ScrapeInterval:  config.ScrapeInterval,
			}, gpcClient)
			if err != nil {
				logger.LogAttrs(ctx, slog.LevelError, "Error creating collector",
					slog.String("service", service),
					slog.String("message", err.Error()))
				continue
			}
		case "GKE":
			collector, err = gke.New(&gke.Config{
				Projects:       config.Projects,
				Logger:         config.Logger,
				ScrapeInterval: config.ScrapeInterval,
			}, gpcClient)
			if err != nil {
				logger.LogAttrs(ctx, slog.LevelError, "Error creating collector",
					slog.String("service", service),
					slog.String("message", err.Error()))
				continue
			}
		default:
			logger.LogAttrs(ctx, slog.LevelError, "Error creating service, does not exist",
				slog.String("service", service))
			continue
		}
		collectors = append(collectors, collector)
	}
	return &GCP{
		config:     config,
		collectors: collectors,
		logger:     logger,
	}, nil
}

// RegisterCollectors will iterate over all the collectors instantiated during New and register their metrics.
func (g *GCP) RegisterCollectors(registry provider.Registry) error {
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
	ch <- collectorLastScrapeTime
	for _, c := range g.collectors {
		if err := c.Describe(ch); err != nil {
			g.logger.LogAttrs(context.Background(), slog.LevelError, "Error calling describe",
				slog.String("message", err.Error()),
			)
		}
	}
}

// Collect implements the prometheus.Collector interface and will iterate over all the collectors instantiated during New and collect their metrics.
func (g *GCP) Collect(ch chan<- prometheus.Metric) {
	wg := sync.WaitGroup{}
	wg.Add(len(g.collectors))
	for _, c := range g.collectors {
		go func(c provider.Collector) {
			now := time.Now()
			defer wg.Done()
			collectorErrors := 0.0
			if err := c.Collect(ch); err != nil {
				g.logger.LogAttrs(context.Background(), slog.LevelError, "Error collecting metrics",
					slog.String("collector", c.Name()),
					slog.String("message", err.Error()),
				)
				collectorErrors++
			}
			g.logger.LogAttrs(context.Background(), slog.LevelInfo, "Collect successful",
				slog.String("collector", c.Name()),
				slog.Duration("duration", time.Since(now)),
			)
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeErrorDesc, prometheus.CounterValue, collectorErrors, subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, time.Since(now).Seconds(), subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem, c.Name())
		}(c)
	}
	wg.Wait()
}
