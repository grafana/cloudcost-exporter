package azure

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/cloudcost-exporter/pkg/azure/aks"
	"github.com/grafana/cloudcost-exporter/pkg/provider"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
)

const (
	subsystem = "azure"
)

var (
	collectorDurationDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "last_scrape_duration_seconds"),
		"Duration of the last scrape in seconds.",
		[]string{"provider", "collector"},
		nil,
	)
	collectorLastScrapeErrorDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "last_scrape_error"),
		"Was the last scrape an error. 1 indicates an error.",
		[]string{"provider", "collector"},
		nil,
	)
	collectorLastScrapeTime = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "last_scrape_time"),
		"Time of the last scrape.",
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
	collectorSuccessDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "collector_success"),
		"Was the last scrape of the Azure metrics successful.",
		[]string{"collector"},
		nil,
	)
	providerLastScrapeDurationDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "", "last_scrape_duration_seconds"),
		"Duration of the last scrape in seconds.",
		[]string{"provider"},
		nil,
	)
	providerLastScrapeErrorDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "", "last_scrape_error"),
		"Was the last scrape an error. 1 indicates an error.",
		[]string{"provider"},
		nil,
	)
	providerLastScrapeTime = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "", "last_scrape_time"),
		"Time of the last scrape.",
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

type Azure struct {
	context          context.Context
	logger           *slog.Logger
	collectorTimeout time.Duration

	collectors []provider.Collector
}

type Config struct {
	Logger *slog.Logger

	CollectorTimeout time.Duration
	Services         []string
}

// New is a TODO
func New(ctx context.Context, config *Config) (*Azure, error) {
	logger := config.Logger.With("provider", subsystem)
	collectors := []provider.Collector{}

	// Collector Registration
	// TODO - implement AZ Auth, AZ SDK init
	for _, svc := range config.Services {
		switch strings.ToUpper(svc) {
		case "AKS":
			// TODO - Init azure client
			collector := aks.New(ctx, &aks.Config{
				Logger: logger,
			})
			collectors = append(collectors, collector)
		default:
			logger.LogAttrs(ctx, slog.LevelInfo, "unknown service", slog.String("service", svc))
		}
	}

	return &Azure{
		context: ctx,
		logger:  logger,

		collectorTimeout: config.CollectorTimeout,
		collectors:       collectors,
	}, nil
}

func (a *Azure) RegisterCollectors(registry provider.Registry) error {
	a.logger.LogAttrs(a.context, slog.LevelInfo, "registering collectors", slog.Int("NumOfCollectors", len(a.collectors)))

	registry.MustRegister(collectorScrapesTotalCounter)
	for _, c := range a.collectors {
		err := c.Register(registry)
		if err != nil {
			return err
		}
	}

	return nil
}

func (a *Azure) Describe(ch chan<- *prometheus.Desc) {
	ch <- collectorLastScrapeErrorDesc
	ch <- collectorDurationDesc
	ch <- providerLastScrapeErrorDesc
	ch <- providerLastScrapeDurationDesc
	ch <- collectorLastScrapeTime
	ch <- providerLastScrapeTime
	ch <- collectorSuccessDesc
	for _, c := range a.collectors {
		if err := c.Describe(ch); err != nil {
			a.logger.LogAttrs(a.context, slog.LevelInfo, "error describing collector", slog.String("collector", c.Name()), slog.String("error", err.Error()))
		}
	}
}

func (a *Azure) Collect(ch chan<- prometheus.Metric) {
	// TODO - implement collector context
	_, cancel := context.WithTimeout(a.context, a.collectorTimeout)
	defer cancel()

	providerStart := time.Now()
	wg := &sync.WaitGroup{}
	wg.Add(len(a.collectors))

	for _, c := range a.collectors {
		go func(c provider.Collector) {
			collectorStart := time.Now()
			defer wg.Done()
			collectorErrors := 0.0
			if err := c.Collect(ch); err != nil {
				collectorErrors = 1.0
				a.logger.LogAttrs(a.context, slog.LevelInfo, "error collecting metrics from collector", slog.String("collector", c.Name()), slog.String("error", err.Error()))
			}
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeErrorDesc, prometheus.GaugeValue, collectorErrors, subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, time.Since(collectorStart).Seconds(), subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorSuccessDesc, prometheus.GaugeValue, collectorErrors, c.Name())
			collectorScrapesTotalCounter.WithLabelValues(subsystem, c.Name()).Inc()
		}(c)

	}
	wg.Wait()

	ch <- prometheus.MustNewConstMetric(providerLastScrapeErrorDesc, prometheus.GaugeValue, 0.0, subsystem)
	ch <- prometheus.MustNewConstMetric(providerLastScrapeDurationDesc, prometheus.GaugeValue, time.Since(providerStart).Seconds(), subsystem)
	ch <- prometheus.MustNewConstMetric(providerLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem)
	providerScrapesTotalCounter.WithLabelValues(subsystem).Inc()
}
