package aks

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/prometheus/client_golang/prometheus"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	subsystem             = "azure_aks"
	AZ_API_VERSION string = "2023-01-01-preview" // using latest API Version https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices
)

type MachineOperatingSystem int

const (
	Linux MachineOperatingSystem = iota
	Windows
)

var machineOperatingSystemNames [2]string = [2]string{"Linux", "Windows"}

func (mo MachineOperatingSystem) String() string {
	return machineOperatingSystemNames[mo]
}

type MachinePriority int

const (
	OnDemand MachinePriority = iota
	Spot
)

var machinePriorityNames [2]string = [2]string{"OnDemand", "Spot"}

func (mp MachinePriority) String() string {
	return machinePriorityNames[mp]
}

// Errors
var (
	ErrClientCreationFailure = errors.New("failed to create client")
	ErrPageAdvanceFailure    = errors.New("failed to advance page")
)

// Prometheus Metrics
var (
// TODO - define Prometheus Metrics
)

// Collector is a prometheus collector that collects metrics from AKS clusters.
type Collector struct {
	context context.Context
	logger  *slog.Logger

	PriceStore   *PriceStore
	MachineStore *MachineStore

	cacheLock          *sync.RWMutex
	MachinePricesCache map[string]*retailPriceSdk.ResourceSKU
}

type Config struct {
	Logger      *slog.Logger
	Credentials *azidentity.DefaultAzureCredential

	SubscriptionId string
}

func New(ctx context.Context, cfg *Config) (*Collector, error) {
	logger := cfg.Logger.With("collector", "aks")

	priceStore, err := NewPricingStore(ctx, logger, cfg.SubscriptionId)
	if err != nil {
		return nil, err
	}

	machineStore, err := NewMachineStore(ctx, logger, priceStore.subscriptionId, cfg.Credentials)
	if err != nil {
		return nil, err
	}

	return &Collector{
		context: ctx,
		logger:  logger,

		PriceStore:   priceStore,
		MachineStore: machineStore,

		cacheLock:          &sync.RWMutex{},
		MachinePricesCache: make(map[string]*retailPriceSdk.ResourceSKU),
	}, nil
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	// TODO - implement
	c.logger.LogAttrs(c.context, slog.LevelInfo, "TODO - implement AKS collector Collect method")
	return nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	// TODO - implement
	c.logger.LogAttrs(c.context, slog.LevelInfo, "TODO - implement AKS collector Describe method")
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Register(_ provider.Registry) error {
	c.logger.LogAttrs(c.context, slog.LevelInfo, "registering collector")
	return nil
}
