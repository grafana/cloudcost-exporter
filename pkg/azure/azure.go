package azure

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/grafana/cloudcost-exporter/pkg/azure/aks"
	"github.com/grafana/cloudcost-exporter/pkg/azure/azureClientWrapper"
	"github.com/grafana/cloudcost-exporter/pkg/provider"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
)

const (
	subsystem = "azure"
)

var (
	InvalidSubscriptionId = errors.New("subscription id was invalid")
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
		"Counter of the number of errors that occurred during the last scrape.",
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
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "success"),
		"Count the number of successful scrapes for a collector.",
		[]string{"provider", "collector"},
		nil,
	)
)

type Azure struct {
	context context.Context
	logger  *slog.Logger

	subscriptionId string
	azCredentials  *azidentity.DefaultAzureCredential

	collectorTimeout time.Duration
	collectors       []provider.Collector
}

type Config struct {
	Logger *slog.Logger

	SubscriptionId string

	CollectorTimeout time.Duration
	Services         []string
}

func New(ctx context.Context, config *Config) (*Azure, error) {
	logger := config.Logger.With("provider", subsystem)
	collectors := []provider.Collector{}

	if config.SubscriptionId == "" {
		logger.LogAttrs(ctx, slog.LevelError, "subscription id was invalid")
		return nil, InvalidSubscriptionId
	}

	creds, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "failed to create azure credentials", slog.String("err", err.Error()))
		return nil, err
	}

	azClientWrapper, err := azureClientWrapper.NewAzureClientWrapper(logger, config.SubscriptionId, creds)
	if err != nil {
		return nil, err
	}

	// Collector Registration
	for _, svc := range config.Services {
		switch strings.ToUpper(svc) {
		case "AKS":
			collector, err := aks.New(ctx, &aks.Config{
				Credentials:    creds,
				SubscriptionId: config.SubscriptionId,
				Logger:         logger,
			}, azClientWrapper)
			if err != nil {
				return nil, err
			}
			collectors = append(collectors, collector)
		default:
			logger.LogAttrs(ctx, slog.LevelInfo, "unknown service", slog.String("service", svc))
		}
	}

	return &Azure{
		context: ctx,
		logger:  logger,

		subscriptionId: config.SubscriptionId,
		azCredentials:  creds,

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
	ch <- collectorLastScrapeTime
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

	wg := &sync.WaitGroup{}
	wg.Add(len(a.collectors))

	for _, c := range a.collectors {
		go func(c provider.Collector) {
			collectorStart := time.Now()
			defer wg.Done()
			collectorErrors := 0.0
			if err := c.Collect(ch); err != nil {
				collectorErrors++
				a.logger.LogAttrs(a.context, slog.LevelInfo, "error collecting metrics from collector", slog.String("collector", c.Name()), slog.String("error", err.Error()))
			}
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeErrorDesc, prometheus.CounterValue, collectorErrors, subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, time.Since(collectorStart).Seconds(), subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem, c.Name())
			collectorScrapesTotalCounter.WithLabelValues(subsystem, c.Name()).Inc()

			counter := collectorScrapesTotalCounter.WithLabelValues(subsystem, c.Name())
			totalMetricCount := &dto.Metric{}
			counter.Write(totalMetricCount)

			ch <- prometheus.MustNewConstMetric(collectorSuccessDesc, prometheus.CounterValue, math.Max(0, totalMetricCount.GetCounter().GetValue()-collectorErrors), subsystem, c.Name())
		}(c)
	}
	wg.Wait()
}
