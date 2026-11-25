package azure

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/cloudcost-exporter/pkg/azure/aks"
	"github.com/grafana/cloudcost-exporter/pkg/azure/blob"
	"github.com/grafana/cloudcost-exporter/pkg/azure/client"
	"github.com/grafana/cloudcost-exporter/pkg/azure/eventhubs"
	"github.com/grafana/cloudcost-exporter/pkg/collectormetrics"
	"github.com/grafana/cloudcost-exporter/pkg/provider"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
)

const (
	subsystem = "azure"

	// Azure service names used by -azure.services. Matching is case-insensitive
	// (see strings.EqualFold below), so these casings are canonical only for
	// display and registry purposes.
	serviceAKS            = "AKS"
	serviceBlob           = "blob"
	serviceEventHubs      = "EVENTHUBS"
	serviceEventHubsAlias = "EVENTHUB"
)

// Services returns the collectors that can be enabled via -azure.services.
// The Name field is the canonical flag value; the dispatch switch in New
// matches case-insensitively against the same constants.
func Services() []provider.ServiceInfo {
	return []provider.ServiceInfo{
		{Name: serviceAKS, DisplayName: "AKS", Description: "Azure Kubernetes Service VM instances and managed disks"},
		{Name: serviceBlob, DisplayName: "Blob", Description: "Azure Blob Storage (cost metrics registered; no series until Cost Management)"},
		{Name: serviceEventHubs, DisplayName: "Event Hubs", Description: "Kafka-compatible Azure Event Hubs namespaces", Aliases: []string{serviceEventHubsAlias}},
	}
}

var errInvalidSubscriptionID = errors.New("subscription id was invalid")

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
)

type Azure struct {
	context          context.Context
	logger           *slog.Logger
	collectorTimeout time.Duration
	collectors       []provider.Collector
	// pending maps a service whose collector failed to create to the name that
	// collector reports, so Collect can report it under the healthy name.
	pending map[string]string
}

type Config struct {
	Logger *slog.Logger
	Region string

	SubscriptionID string
	ScrapeInterval time.Duration

	CollectorTimeout     time.Duration
	Services             []string
	ExperimentalServices []string
}

func New(ctx context.Context, config *Config) (*Azure, error) {
	logger := config.Logger.With("provider", subsystem)

	if config.SubscriptionID == "" {
		logger.LogAttrs(ctx, slog.LevelError, "subscription id was invalid")
		return nil, errInvalidSubscriptionID
	}

	creds, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "failed to create azure credentials", slog.String("err", err.Error()))
		return nil, err
	}

	azClientWrapper, err := client.NewAzureClientWrapper(ctx, logger, config.SubscriptionID, creds)
	if err != nil {
		return nil, err
	}

	create := func(ctx context.Context, service string) (provider.Collector, error) {
		return createCollector(ctx, config, logger, azClientWrapper, service)
	}
	return newWithFactory(ctx, config, logger, create), nil
}

// newWithFactory builds the provider from a collector factory, so tests can
// inject creation failures. A collector that fails to create is skipped and
// reported by Collect with collector_last_scrape_error set to 1.
func newWithFactory(ctx context.Context, config *Config, logger *slog.Logger, create collectorFactory) *Azure {
	collectors := []provider.Collector{}
	pending := map[string]string{}

	// Collector Registration (--azure.services matching is case-insensitive). Register stable
	// services followed by experimental ones, which are outside the backward-compatibility
	// contract, so warn when registering them. A service already enabled as stable is not
	// registered again as experimental; registering it twice would fail collector registration
	// with a duplicate-descriptor error.
	stableNames := make(map[string]bool, len(config.Services))
	for _, s := range config.Services {
		stableNames[strings.ToUpper(strings.TrimSpace(s))] = true
	}
	allServices := slices.Concat(config.Services, config.ExperimentalServices)
	for i, svc := range allServices {
		svc = strings.TrimSpace(svc)
		if svc == "" {
			continue
		}
		if i >= len(config.Services) {
			if stableNames[strings.ToUpper(svc)] {
				continue
			}
			logger.LogAttrs(ctx, slog.LevelWarn, "registering experimental collector; its metrics are not covered by the backward-compatibility contract and may change",
				slog.String("service", svc))
		}
		collector, err := createSafely(ctx, create, svc)
		if errors.Is(err, errUnknownService) {
			logger.LogAttrs(ctx, slog.LevelInfo, "unknown service", slog.String("service", svc))
			continue
		}
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelError, "Error creating collector",
				slog.String("service", svc),
				slog.String("message", err.Error()))
			pending[svc] = pendingName(svc)
			continue
		}
		collectors = append(collectors, collector)
	}

	return &Azure{
		context:          ctx,
		logger:           logger,
		collectorTimeout: config.CollectorTimeout,
		collectors:       collectors,
		pending:          pending,
	}
}

// collectorFactory creates the collector for a service. Tests inject failures through it.
type collectorFactory func(ctx context.Context, service string) (provider.Collector, error)

var errUnknownService = errors.New("service does not exist")

// collectorNames maps a service to the name its collector reports from Name().
// A collector that could not be created is reported under the same name as a
// healthy one, so both appear as one series over time rather than two.
var collectorNames = map[string]string{
	strings.ToUpper(serviceAKS):            "azure_aks",
	strings.ToUpper(serviceBlob):           "azure_blob",
	strings.ToUpper(serviceEventHubs):      "eventhubs",
	strings.ToUpper(serviceEventHubsAlias): "eventhubs",
}

func pendingName(service string) string {
	if name, ok := collectorNames[strings.ToUpper(service)]; ok {
		return name
	}
	return strings.ToLower(service)
}

func createCollector(ctx context.Context, config *Config, logger *slog.Logger, azClientWrapper client.AzureClient, service string) (provider.Collector, error) {
	switch {
	case strings.EqualFold(service, serviceAKS):
		return aks.New(ctx, &aks.Config{
			SubscriptionID: config.SubscriptionID,
		}, logger, azClientWrapper)
	case strings.EqualFold(service, serviceBlob):
		return blob.New(ctx, &blob.Config{
			SubscriptionID: config.SubscriptionID,
			ScrapeInterval: config.ScrapeInterval,
		}, logger)
	case strings.EqualFold(service, serviceEventHubs), strings.EqualFold(service, serviceEventHubsAlias):
		return eventhubs.New(ctx, &eventhubs.Config{
			Logger: logger,
		}, azClientWrapper)
	default:
		return nil, errUnknownService
	}
}

// createSafely turns a panic inside a collector constructor into an error, so
// one bad collector cannot take down the provider at startup.
func createSafely(ctx context.Context, create collectorFactory, service string) (c provider.Collector, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic creating collector for %s: %v", service, p)
		}
	}()
	return create(ctx, service)
}

func (a *Azure) RegisterCollectors(registry provider.Registry) error {
	a.logger.LogAttrs(a.context, slog.LevelInfo, "registering collectors", slog.Int("NumOfCollectors", len(a.collectors)))
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
	for _, c := range a.collectors {
		if err := c.Describe(ch); err != nil {
			a.logger.LogAttrs(a.context, slog.LevelInfo, "error describing collector", slog.String("collector", c.Name()), slog.String("error", err.Error()))
		}
	}
}

func (a *Azure) Collect(ch chan<- prometheus.Metric) {
	// Create a context with timeout for this collection cycle
	collectCtx, cancel := context.WithTimeout(a.context, a.collectorTimeout)
	defer cancel()

	// A collector that failed to create emits the same three metrics a healthy
	// one does, so the family keeps a single label set. Duration is zero because
	// no scrape ran.
	names := make([]string, 0, len(a.pending))
	for _, name := range a.pending {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		ch <- prometheus.MustNewConstMetric(collectorLastScrapeErrorDesc, prometheus.CounterValue, 1, subsystem, name)
		ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, 0, subsystem, name)
		ch <- prometheus.MustNewConstMetric(collectorLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem, name)
	}

	wg := &sync.WaitGroup{}
	wg.Add(len(a.collectors))

	for _, c := range a.collectors {
		go func(c provider.Collector) {
			defer wg.Done()

			duration, hasError := collectormetrics.Collect(collectCtx, c, ch, a.logger, subsystem)

			// TODO: remove collectorErrors once we have the new metrics
			collectorErrors := 0.0
			if hasError {
				collectorErrors = 1.0
			}
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeErrorDesc, prometheus.CounterValue, collectorErrors, subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, duration, subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem, c.Name())
		}(c)
	}
	wg.Wait()
}
