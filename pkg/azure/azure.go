package azure

import (
	"context"
	"log/slog"
	"strings"
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
	collectorScrapesTotalCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "scrapes_total"),
			Help: "Total number of scrapes for a collector.",
		},
		[]string{"provider", "collector"},
	)

// TODO - add prometheus metrics here
)

type Azure struct {
	Context context.Context
	Logger  *slog.Logger

	Collectors       []provider.Collector
	CollectorTimeout time.Duration
}

type Config struct {
	Logger *slog.Logger

	CollectorTimeout time.Duration
	Services         []string
}

// New is a TODO
func New(ctx context.Context, config *Config) (*Azure, error) {
	providerGroup := config.Logger.WithGroup(subsystem)
	collectors := []provider.Collector{}

	// Collector Registration
	// TODO - implement AZ Auth, AZ SDK init
	for _, svc := range config.Services {
		switch strings.ToUpper(svc) {
		case "AKS":
			// TODO - Init azure client
			collector := aks.New(ctx, &aks.Config{
				Logger: providerGroup,
			})
			collectors = append(collectors, collector)
		default:
			providerGroup.LogAttrs(ctx, slog.LevelInfo, "unknown service", slog.String("service", svc))
		}
	}

	return &Azure{
		Context: ctx,
		Logger:  providerGroup,

		CollectorTimeout: config.CollectorTimeout,
		Collectors:       collectors,
	}, nil
}

// RegisterCollectors is a TODO
func (a *Azure) RegisterCollectors(registry provider.Registry) error {
	a.Logger.LogAttrs(a.Context, slog.LevelInfo, "registering collectors for azure", slog.Int("NumOfCollectors", len(a.Collectors)))

	registry.MustRegister(collectorScrapesTotalCounter)
	for _, c := range a.Collectors {
		err := c.Register(registry)
		if err != nil {
			return err
		}
	}

	return nil
}

// Describe is a TODO
func (a *Azure) Describe(ch chan<- *prometheus.Desc) {
}

// Collect is a TODO
func (a *Azure) Collect(ch chan<- prometheus.Metric) {
	// TODO - implement collector context
	_, cancel := context.WithTimeout(a.Context, a.CollectorTimeout)
	defer cancel()
}
