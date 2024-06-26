package aks

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/cloudcost-exporter/pkg/provider"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
)

const (
	subsystem             = "azure_aks"
	AZ_API_VERSION string = "2023-01-01-preview" // using latest API Version https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices

	priceRefreshInterval      = 24 * time.Hour  // TODO - update to 24 hours
	machineRefreshInterval    = 5 * time.Minute // TODO - update to 5 minutes
	cacheInvalidationInterval = 24 * time.Hour  // TODO - update to 24 hours
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
	InstanceCPUHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "instance_cpu_usd_per_core_hour"),
		"The cpu cost a compute instance in USD/(core*h)",
		[]string{"instance", "region", "family", "machine_type", "cluster", "price_tier"},
		nil,
	)
	InstanceMemoryHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "instance_memory_usd_per_gib_hour"),
		"The memory cost of a compute instance in USD/(GiB*h)",
		[]string{"instance", "region", "family", "machine_type", "cluster", "price_tier"},
		nil,
	)
)

// Collector is a prometheus collector that collects metrics from AKS clusters.
type Collector struct {
	context context.Context
	logger  *slog.Logger

	PriceStore                   *PriceStore
	priceStoreNextPopulationTime time.Time

	MachineStore                   *MachineStore
	machineStoreNextPopulationTime time.Time

	cacheLock             *sync.RWMutex
	cacheInvalidationTime time.Time
	MachinePricesCache    map[string]float64
}

type Config struct {
	Logger      *slog.Logger
	Credentials *azidentity.DefaultAzureCredential

	SubscriptionId string
}

func New(ctx context.Context, cfg *Config) (*Collector, error) {
	logger := cfg.Logger.With("collector", "aks")
	now := time.Now()

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

		PriceStore:                   priceStore,
		priceStoreNextPopulationTime: now.Add(priceRefreshInterval),

		MachineStore:                   machineStore,
		machineStoreNextPopulationTime: now.Add(machineRefreshInterval),

		cacheLock:             &sync.RWMutex{},
		cacheInvalidationTime: now.Add(cacheInvalidationInterval),
		MachinePricesCache:    make(map[string]float64),
	}, nil
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

func (c *Collector) populateMachinePricesCache() error {
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()
	c.logger.Info("populating machine prices cache")

	c.MachineStore.machineMapLock.RLock()
	defer c.MachineStore.machineMapLock.RUnlock()

	for vmName, vmInfo := range c.MachineStore.MachineMap {
		price, err := c.PriceStore.getPriceInfoFromVmInfo(vmInfo)
		if err != nil {
			return err
		}

		c.MachinePricesCache[vmName] = price
	}

	return nil
}

// TODO - BREAK INTO CPU AND RAM
func (c *Collector) getMachinePrices(vmName string) (float64, error) {
	// Cache Hit
	c.cacheLock.RLock()
	if price, ok := c.MachinePricesCache[vmName]; ok {
		c.cacheLock.RUnlock()
		return price, nil
	}
	c.cacheLock.RUnlock()

	// Cache Miss
	c.cacheLock.Lock()
	defer c.cacheLock.Unlock()

	vmInfo := c.MachineStore.getVmInfoByVmName(vmName)
	price, err := c.PriceStore.getPriceInfoFromVmInfo(vmInfo)
	if err != nil {
		return 0.0, err
	}

	return price, nil
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	c.logger.Info("collecting metrics")
	now := time.Now()

	eg := new(errgroup.Group)
	if now.After(c.machineStoreNextPopulationTime) {
		eg.Go(func() error {
			err := c.MachineStore.PopulateMachineStore()
			if err != nil {
				return err
			}

			c.machineStoreNextPopulationTime = time.Now().Add(machineRefreshInterval)
			return nil
		})
	}

	if now.After(c.priceStoreNextPopulationTime) {
		eg.Go(func() error {
			regionList := c.MachineStore.getRegionList()
			err := c.PriceStore.PopulatePriceStore(regionList)
			if err != nil {
				return err
			}

			c.priceStoreNextPopulationTime = time.Now().Add(priceRefreshInterval)
			return nil
		})
	}

	err := eg.Wait()
	if err != nil {
		return err
	}

	if now.After(c.cacheInvalidationTime) {
		c.MachinePricesCache = make(map[string]float64)
		c.cacheInvalidationTime = now.Add(cacheInvalidationInterval)
		err = c.populateMachinePricesCache()
		if err != nil {
			return nil
		}
	}

	c.MachineStore.machineMapLock.RLock()
	defer c.MachineStore.machineMapLock.RUnlock()
	for vmName, vmInfo := range c.MachineStore.MachineMap {
		price, err := c.getMachinePrices(vmName)
		if err != nil {
			return err
		}

		labelValues := []string{
			vmName,
			vmInfo.Region,
			"TODO - MACHINE FAMILY?",
			vmInfo.MachineTypeSku,
			"TODO - ClusterName",
			vmInfo.Priority.String(),
		}
		ch <- prometheus.MustNewConstMetric(InstanceCPUHourlyCostDesc, prometheus.GaugeValue, price, labelValues...)
		ch <- prometheus.MustNewConstMetric(InstanceMemoryHourlyCostDesc, prometheus.GaugeValue, price, labelValues...)
	}

	c.logger.Info("metrics collected")
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
