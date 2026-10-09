package google

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/collectormetrics"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/google/cloudsql"
	gcpmanagedkafka "github.com/grafana/cloudcost-exporter/pkg/google/managedkafka"
	"github.com/grafana/cloudcost-exporter/pkg/google/networking"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/google/gcs"
	"github.com/grafana/cloudcost-exporter/pkg/google/gke"
	"github.com/grafana/cloudcost-exporter/pkg/google/vertex"
	"github.com/grafana/cloudcost-exporter/pkg/google/vpc"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem = "gcp"

	// collectConcurrencyLimit caps the number of collector goroutines that run
	// simultaneously during a scrape. This prevents unbounded API fan-out when
	// many collectors are registered and keeps memory and rate-limit usage
	// predictable. The value matches Azure's ConcurrentGoroutineLimit.
	collectConcurrencyLimit = 10

	// GCP service names used by -gcp.services.
	serviceGCS          = "GCS"
	serviceGKE          = "GKE"
	serviceCLB          = "CLB"
	serviceVPC          = "VPC"
	serviceSQL          = "SQL"
	serviceManagedKafka = "MANAGEDKAFKA"
	serviceKafkaAlias   = "KAFKA"
	serviceVertex       = "VERTEX"
)

// Services returns the collectors that can be enabled via -gcp.services.
// The Name field is the canonical flag value; the dispatch switch in New
// cases on the same constants.
func Services() []provider.ServiceInfo {
	return []provider.ServiceInfo{
		{Name: serviceGKE, DisplayName: "GKE", Description: "Google Kubernetes Engine clusters"},
		{Name: serviceGCS, DisplayName: "GCS", Description: "Google Cloud Storage buckets"},
		{Name: serviceSQL, DisplayName: "Cloud SQL", Description: "Managed database instances"},
		{Name: serviceManagedKafka, DisplayName: "Managed Kafka", Description: "Managed Service for Apache Kafka clusters", Aliases: []string{serviceKafkaAlias}},
		{Name: serviceCLB, DisplayName: "CLB", Description: "Cloud Load Balancers via forwarding rules"},
		{Name: serviceVPC, DisplayName: "VPC", Description: "Cloud NAT Gateway, VPN Gateway, Private Service Connect"},
		{Name: serviceVertex, DisplayName: "Vertex AI", Description: "Vertex AI model token, character, compute, and reranking pricing"},
	}
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
		"Time of the last scrape.W",
		[]string{"provider", "collector"},
		nil,
	)
)

type GCP struct {
	config           *Config
	collectors       []provider.Collector
	logger           *slog.Logger
	ctx              context.Context
	collectorTimeout time.Duration
	// retryInitial and retryMax bound retryPending's backoff. Set once, before
	// the retry goroutine starts, and never written again.
	retryInitial time.Duration
	retryMax     time.Duration

	// mu guards collectors, registry, and pending, which the background retry
	// changes after startup.
	mu sync.RWMutex
	// registry is set by RegisterCollectors; collectors created later register against it.
	registry provider.Registry
	// pending maps a service whose collector failed to create to the name that
	// collector reports, so Collect can report it under the healthy name.
	pending map[string]string
}

type Config struct {
	ProjectId            string // ProjectID is where the project is running. Used for authentication.
	Region               string
	Projects             string // Projects is a comma-separated list of projects to scrape metadata from
	Services             []string
	ExperimentalServices []string
	ScrapeInterval       time.Duration
	DefaultDiscount      int
	CollectorTimeout     time.Duration
	// GKEZoneConcurrency caps zone-level goroutines per project during a GKE scrape.
	// Zero or negative values fall back to gke.DefaultZoneCollectConcurrency.
	GKEZoneConcurrency int
	// VertexFamilyFilter is a regex matched against the Vertex model family label; only matching
	// families are emitted. Mirrors --aws.bedrock.families. Empty or ".*" emits all families.
	VertexFamilyFilter string
	// CollectorRetryInitial and CollectorRetryMax bound the backoff between
	// attempts to create a collector that failed at startup. Zero or negative
	// values fall back to defaultCollectorRetryInitial and
	// defaultCollectorRetryMax.
	CollectorRetryInitial time.Duration
	CollectorRetryMax     time.Duration
	Logger                *slog.Logger
}

// collectorFactory creates the collector for a service. Tests inject failures through it.
type collectorFactory func(ctx context.Context, service string) (provider.Collector, error)

var errUnknownService = errors.New("service does not exist")

// collectorNames maps a service to the name its collector reports from Name().
// A collector that could not be created is reported under the same name as a
// healthy one, so both appear as one series over time rather than two.
// TestCollectorNamesMatchTheNameEachCollectorReports keeps this in sync.
var collectorNames = map[string]string{
	serviceGCS:          "GCS",
	serviceGKE:          "gcp_gke",
	serviceCLB:          "ForwardingRule",
	serviceVPC:          "VPC",
	serviceSQL:          "cloudsql",
	serviceManagedKafka: "managedkafka",
	serviceKafkaAlias:   "managedkafka",
	serviceVertex:       "gcp_vertex",
}

// defaultCollectorRetryInitial and defaultCollectorRetryMax bound the backoff
// between attempts to create a collector that failed at startup. Config can
// override them, which is how tests shorten the wait. Mirrors the AKS VM price
// store's adaptive-interval pattern (pkg/azure/aks/aks.go).
const (
	defaultCollectorRetryInitial = 30 * time.Second
	defaultCollectorRetryMax     = 15 * time.Minute
)

// New is responsible for parsing out a configuration file and setting up the associated services that could be required.
// We instantiate services to avoid repeating common services that may be shared across many collectors. In the future we can push
// collector specific services further down.
//
// A collector that fails to create is skipped at startup and retried in the
// background with backoff until it succeeds or ctx is cancelled. Until then,
// Collect reports it with collector_last_scrape_error set to 1.
func New(ctx context.Context, config *Config) (*GCP, error) {
	gcpClient, err := client.NewGCPClient(ctx, client.Config{ProjectId: config.ProjectId, Discount: config.DefaultDiscount})
	if err != nil {
		return nil, err
	}
	return newWithClient(ctx, config, gcpClient), nil
}

// newWithClient builds the provider around an existing client, so tests can
// inject faults into the cloud APIs.
func newWithClient(ctx context.Context, config *Config, gcpClient client.Client) *GCP {
	logger := config.Logger.With("provider", subsystem)

	create := func(ctx context.Context, service string) (provider.Collector, error) {
		return createCollector(ctx, config, logger, gcpClient, service)
	}

	g := &GCP{
		config:           config,
		logger:           logger,
		ctx:              ctx,
		collectorTimeout: config.CollectorTimeout,
		retryInitial:     config.CollectorRetryInitial,
		retryMax:         config.CollectorRetryMax,
	}
	if g.retryInitial <= 0 {
		g.retryInitial = defaultCollectorRetryInitial
	}
	if g.retryMax <= 0 {
		g.retryMax = defaultCollectorRetryMax
	}

	// Register stable services followed by experimental ones. Experimental collectors are outside
	// the backward-compatibility contract, so warn when registering them. A service already enabled
	// as stable is not registered again as experimental; registering it twice would fail collector
	// registration with a duplicate-descriptor error.
	stableNames := make(map[string]bool, len(config.Services))
	for _, s := range config.Services {
		stableNames[strings.ToUpper(strings.TrimSpace(s))] = true
	}
	allServices := slices.Concat(config.Services, config.ExperimentalServices)
	for i, service := range allServices {
		service = strings.TrimSpace(service)
		if service == "" {
			continue
		}
		if i >= len(config.Services) {
			if stableNames[strings.ToUpper(service)] {
				continue
			}
			logger.LogAttrs(ctx, slog.LevelWarn, "registering experimental collector; its metrics are not covered by the backward-compatibility contract and may change",
				slog.String("service", service))
		}
		logger.LogAttrs(ctx, slog.LevelInfo, "Creating service",
			slog.String("service", service))

		collector, err := createSafely(ctx, create, service)
		if errors.Is(err, errUnknownService) {
			logger.LogAttrs(ctx, slog.LevelError, "Error creating service, does not exist",
				slog.String("service", service))
			continue
		}
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelError, "Error creating collector",
				slog.String("service", service),
				slog.String("message", err.Error()))
			g.markPending(service)
			continue
		}
		g.collectors = append(g.collectors, collector)
	}

	if len(g.pendingServices()) > 0 {
		go g.retryPending(ctx, create)
	}
	return g
}

func createCollector(ctx context.Context, config *Config, logger *slog.Logger, gcpClient client.Client, service string) (provider.Collector, error) {
	switch strings.ToUpper(service) {
	case serviceGCS:
		c, err := gcs.New(ctx, &gcs.Config{
			ProjectId:      config.ProjectId,
			Projects:       config.Projects,
			ScrapeInterval: config.ScrapeInterval,
		}, logger, gcpClient)
		if err != nil {
			return nil, err
		}
		return c, nil
	case serviceGKE:
		c, err := gke.New(ctx, &gke.Config{
			Projects:        config.Projects,
			ScrapeInterval:  config.ScrapeInterval,
			ZoneConcurrency: config.GKEZoneConcurrency,
		}, logger, gcpClient)
		if err != nil {
			return nil, err
		}
		return c, nil
	case serviceCLB:
		// CLB = Cloud Load Balancer, but we use forwarding rules to calculate price
		c, err := networking.New(ctx, &networking.Config{
			ScrapeInterval: config.ScrapeInterval,
			Projects:       config.Projects,
		}, logger, gcpClient)
		logger.LogAttrs(ctx, slog.LevelInfo, "Creating collector",
			slog.String("service", service),
			slog.String("projects", config.Projects))
		if err != nil {
			return nil, err
		}
		return c, nil
	case serviceVPC:
		c, err := vpc.New(ctx, &vpc.Config{
			Projects:       config.Projects,
			ScrapeInterval: config.ScrapeInterval,
		}, logger, gcpClient)
		if err != nil {
			return nil, err
		}
		return c, nil
	case serviceSQL:
		c, err := cloudsql.New(ctx, &cloudsql.Config{
			Projects:       config.Projects,
			ScrapeInterval: config.ScrapeInterval,
		}, logger, gcpClient)
		if err != nil {
			return nil, err
		}
		return c, nil
	case serviceKafkaAlias, serviceManagedKafka:
		c, err := gcpmanagedkafka.New(ctx, &gcpmanagedkafka.Config{
			Projects:       config.Projects,
			ScrapeInterval: config.ScrapeInterval,
		}, logger, gcpClient)
		if err != nil {
			return nil, err
		}
		return c, nil
	case serviceVertex:
		c, err := vertex.New(ctx, &vertex.Config{
			ProjectId:    config.ProjectId,
			FamilyFilter: config.VertexFamilyFilter,
		}, logger, gcpClient)
		if err != nil {
			return nil, err
		}
		return c, nil
	default:
		return nil, errUnknownService
	}
}

// markPending records a service whose collector failed to create.
func (g *GCP) markPending(service string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.pending == nil {
		g.pending = map[string]string{}
	}
	name, ok := collectorNames[strings.ToUpper(service)]
	if !ok {
		name = strings.ToLower(service)
	}
	g.pending[service] = name
}

// createSafely turns a panic inside a collector constructor into an error, so
// one bad collector cannot take down the provider. At startup an unrecovered
// panic kills the process before it serves; during the background retry it
// would kill an already-serving pod and every healthy collector with it. A
// panicking constructor is treated like any other failure: skipped, reported,
// and retried.
func createSafely(ctx context.Context, create collectorFactory, service string) (c provider.Collector, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic creating collector for %s: %v", service, p)
		}
	}()
	return create(ctx, service)
}

// retryPending keeps trying to create the collectors that failed at startup,
// backing off between rounds, until every one exists or ctx is cancelled.
// Anything still pending is reported by Collect as an error, so a collector
// that never comes back stays visible rather than merely absent.
func (g *GCP) retryPending(ctx context.Context, create collectorFactory) {
	queue := g.pendingServices()
	delay := g.retryInitial
	for len(queue) > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		var remaining []string
		for _, service := range queue {
			collector, err := createSafely(ctx, create, service)
			if err == nil {
				err = g.addCollector(collector)
			}
			if err != nil {
				g.logger.LogAttrs(ctx, slog.LevelWarn, "Retrying collector creation failed",
					slog.String("service", service),
					slog.String("message", err.Error()))
				remaining = append(remaining, service)
				continue
			}
			g.clearPending(service)
			g.logger.LogAttrs(ctx, slog.LevelInfo, "Collector created after retry",
				slog.String("service", service))
		}

		queue = remaining
		delay = min(delay*2, g.retryMax)
	}
}

func (g *GCP) pendingServices() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	services := make([]string, 0, len(g.pending))
	for service := range g.pending {
		services = append(services, service)
	}
	slices.Sort(services)
	return services
}

func (g *GCP) clearPending(service string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.pending, service)
}

// addCollector adds a collector created after startup. Once RegisterCollectors
// has run it also registers the collector's metrics. A panic during
// registration is returned as an error so a background goroutine cannot crash
// the exporter.
func (g *GCP) addCollector(c provider.Collector) (err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.registry != nil {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic registering collector %s: %v", c.Name(), p)
			}
		}()
		if err := c.Register(g.registry); err != nil {
			return err
		}
	}
	g.collectors = append(g.collectors, c)
	return nil
}

// snapshot returns the collectors, and the names of those that failed to
// create, both safe to use without holding the lock.
func (g *GCP) snapshot() ([]provider.Collector, []string) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	names := make([]string, 0, len(g.pending))
	for _, name := range g.pending {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Clone(g.collectors), names
}

// RegisterCollectors will iterate over all the collectors instantiated during New and register their metrics.
// Collectors created later, by the background retry, register against the same
// registry as they are added.
func (g *GCP) RegisterCollectors(registry provider.Registry) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.registry = registry
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
	collectors, _ := g.snapshot()
	for _, c := range collectors {
		if err := c.Describe(ch); err != nil {
			g.logger.LogAttrs(g.ctx, slog.LevelError, "Error calling describe",
				slog.String("message", err.Error()),
			)
		}
	}
}

// Collect implements the prometheus.Collector interface and will iterate over all the collectors instantiated during New and collect their metrics.
// A collector that has not been created yet is reported with
// collector_last_scrape_error set to 1.
func (g *GCP) Collect(ch chan<- prometheus.Metric) {
	// Create a context with timeout for this collection cycle
	collectCtx, cancel := context.WithTimeout(g.ctx, g.collectorTimeout)
	defer cancel()

	collectors, notCreated := g.snapshot()
	// A collector that failed to create emits the same three metrics a healthy
	// one does, so the family keeps a single label set and queries that join
	// them do not silently drop it. Duration is zero because no scrape ran, and
	// the timestamp marks this cycle, matching the healthy path, which stamps
	// time.Now() whether the scrape succeeded or not.
	for _, name := range notCreated {
		ch <- prometheus.MustNewConstMetric(collectorLastScrapeErrorDesc, prometheus.CounterValue, 1, subsystem, name)
		ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, 0, subsystem, name)
		ch <- prometheus.MustNewConstMetric(collectorLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem, name)
	}

	eg, collectCtx := errgroup.WithContext(collectCtx)
	eg.SetLimit(collectConcurrencyLimit)
	for _, c := range collectors {
		eg.Go(func() error {
			duration, hasError := collectormetrics.Collect(collectCtx, c, ch, g.logger, subsystem)

			if !hasError {
				g.logger.LogAttrs(collectCtx, slog.LevelInfo, "Collect successful",
					slog.String("collector", c.Name()),
					slog.Duration("duration", time.Duration(duration*float64(time.Second))),
				)
			}

			//TODO: remove collectorErrors once we have the new metrics
			collectorErrors := 0.0
			if hasError {
				collectorErrors = 1.0
			}
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeErrorDesc, prometheus.CounterValue, collectorErrors, subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, duration, subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem, c.Name())
			return nil
		})
	}
	// Goroutines always return nil; Wait() will not return an error.
	_ = eg.Wait()
}
