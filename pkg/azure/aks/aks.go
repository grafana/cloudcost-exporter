package aks

import (
	"context"
	"errors"
	"log/slog"
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
	ErrClientCreationFailure         = errors.New("failed to create client")
	ErrPageAdvanceFailure            = errors.New("failed to advance page")
	ErrPriceStorePopulationFailure   = errors.New("failed to populate price store")
	ErrMachineStorePopulationFailure = errors.New("failed to populate machine store")
	ErrVmPriceRetrievalFailure       = errors.New("failed to retrieve price info for VM")
)

// Prometheus Metrics
var (
	InstanceCPUHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "instance_cpu_usd_per_core_hour"),
		"The cpu cost a compute instance in USD/(core*h)",
		[]string{"instance", "region", "machine_type", "family", "cluster_name", "price_tier", "operating_system"},
		nil,
	)
	InstanceMemoryHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "instance_memory_usd_per_gib_hour"),
		"The memory cost of a compute instance in USD/(GiB*h)",
		[]string{"instance", "region", "machine_type", "family", "cluster_name", "price_tier", "operating_system"},
		nil,
	)
	InstanceTotalHourlyCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "instance_total_usd_per_hour"),
		"The total cost of an compute instance in USD/h",
		[]string{"instance", "region", "machine_type", "family", "cluster_name", "price_tier", "operating_system"},
		nil,
	)
)

// Collector is a prometheus collector that collects metrics from AKS clusters.
type Collector struct {
	context context.Context
	logger  *slog.Logger

	PriceStore *PriceStore

	MachineStore *MachineStore
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
	}, nil
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

func (c *Collector) CheckReadiness() bool {
	return c.PriceStore.CheckReadiness() && c.MachineStore.CheckReadiness()
}

func (c *Collector) getMachinePrices(vmId string) (*MachineSku, error) {
	vmInfo, err := c.MachineStore.getVmInfoByVmId(vmId)
	if err != nil {
		return nil, err
	}

	prices, err := c.PriceStore.getPriceInfoFromVmInfo(vmInfo)
	if err != nil {
		return nil, ErrVmPriceRetrievalFailure
	}

	return prices, nil
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	c.logger.Info("collecting metrics")
	now := time.Now()

	eg, egCtx := errgroup.WithContext(c.context)
	eg.Go(func() error {
		err := c.MachineStore.PopulateMachineStore(egCtx)
		if err != nil {
			return ErrMachineStorePopulationFailure
		}
		return nil
	})

	eg.Go(func() error {
		err := c.PriceStore.PopulatePriceStore(egCtx)
		if err != nil {
			return ErrPriceStorePopulationFailure
		}
		return nil
	})

	err := eg.Wait()
	if err != nil {
		return err
	}

	c.MachineStore.machineMapLock.RLock()
	defer c.MachineStore.machineMapLock.RUnlock()
	for vmId, vmInfo := range c.MachineStore.MachineMap {
		price, err := c.getMachinePrices(vmId)
		if err != nil {
			return err
		}

		labelValues := []string{
			vmInfo.Name,
			vmInfo.Region,
			vmInfo.MachineTypeSku,
			vmInfo.MachineFamily,
			vmInfo.OwningCluster,
			vmInfo.Priority.String(),
			vmInfo.OperatingSystem.String(),
		}

		ch <- prometheus.MustNewConstMetric(InstanceCPUHourlyCostDesc, prometheus.GaugeValue, price.MachinePricesBreakdown.PricePerCore, labelValues...)
		ch <- prometheus.MustNewConstMetric(InstanceMemoryHourlyCostDesc, prometheus.GaugeValue, price.MachinePricesBreakdown.PricePerGiB, labelValues...)
		ch <- prometheus.MustNewConstMetric(InstanceTotalHourlyCostDesc, prometheus.GaugeValue, price.RetailPrice, labelValues...)
	}

	c.logger.LogAttrs(c.context, slog.LevelInfo, "metrics collected", slog.Duration("duration", time.Since(now)))
	return nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- InstanceCPUHourlyCostDesc
	ch <- InstanceMemoryHourlyCostDesc
	ch <- InstanceTotalHourlyCostDesc
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Register(_ provider.Registry) error {
	c.logger.LogAttrs(c.context, slog.LevelInfo, "registering collector")
	return nil
}
