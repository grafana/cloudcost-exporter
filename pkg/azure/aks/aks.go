package aks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/azure/client"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"

	"github.com/prometheus/client_golang/prometheus"

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

var machinePriorityNames [2]string = [2]string{"ondemand", "spot"}

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
	InstanceCPUHourlyCostDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		utils.InstanceCPUCostSuffix,
		"The cpu cost a a compute instance in USD/(core*h)",
		[]string{"instance", "region", "machine_type", "family", "cluster_name", "price_tier", "operating_system"},
	)
	InstanceMemoryHourlyCostDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		utils.InstanceMemoryCostSuffix,
		"The memory cost of a compute instance in USD/(GiB*h)",
		[]string{"instance", "region", "machine_type", "family", "cluster_name", "price_tier", "operating_system"},
	)
	InstanceTotalHourlyCostDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		utils.InstanceTotalCostSuffix,
		"The total cost of a compute instance in USD/h",
		[]string{"instance", "region", "machine_type", "family", "cluster_name", "price_tier", "operating_system"},
	)
	PersistentVolumeHourlyCostDesc = utils.GenerateDesc(
		cloudcost_exporter.MetricPrefix,
		subsystem,
		utils.PersistentVolumeCostSuffix,
		"The cost of an Azure Managed Disk in USD/(GiB*h)",
		[]string{"persistentvolume", "region", "availability_zone", "disk", "type", "size_gib", "state"},
	)
)

// Collector is a prometheus collector that collects metrics from AKS clusters.
type Collector struct {
	context context.Context
	logger  *slog.Logger

	PriceStore   *PriceStore
	MachineStore *MachineStore
	DiskStore    *DiskStore
}

type Config struct {
	Logger *slog.Logger
}

func New(ctx context.Context, cfg *Config, azClientWrapper client.AzureClient) (*Collector, error) {
	logger := cfg.Logger.With("collector", "aks")
	priceStore := NewPricingStore(ctx, logger, azClientWrapper)
	machineStore, err := NewMachineStore(ctx, logger, azClientWrapper)
	if err != nil {
		return nil, err
	}
	diskStore := NewDiskStore(ctx, logger, azClientWrapper)

	priceTicker := time.NewTicker(priceRefreshInterval)
	machineTicker := time.NewTicker(machineRefreshInterval)
	diskTicker := time.NewTicker(diskRefreshInterval)

	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-priceTicker.C:
				priceStore.PopulatePriceStore(ctx)
			}
		}
	}(ctx)
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-machineTicker.C:
				machineStore.PopulateMachineStore(ctx)
			}
		}
	}(ctx)
	go func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-diskTicker.C:
				diskStore.PopulateDiskStore(ctx)
				diskStore.PopulateDiskPricing(ctx)
			}
		}
	}(ctx)

	return &Collector{
		context: ctx,
		logger:  logger,

		PriceStore:   priceStore,
		MachineStore: machineStore,
		DiskStore:    diskStore,
	}, nil
}

// CollectMetrics is a no-op function that satisfies the provider.Collector interface.
// Deprecated: CollectMetrics is deprecated and will be removed in a future release.
func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

func (c *Collector) getMachinePrices(vmId string) (*MachineSku, error) {
	vmInfo, err := c.MachineStore.getVmInfoByVmId(vmId)
	if err != nil {
		return nil, err
	}

	prices, err := c.PriceStore.getPriceInfoFromVmInfo(vmInfo)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", err, ErrVmPriceRetrievalFailure)
	}

	return prices, nil
}

// Collect satisfies the provider.Collector interface.
func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	c.logger.Info("collecting metrics")
	now := time.Now()

	machineList := c.MachineStore.GetListOfVmsForSubscription()

	machineMetricsCount := 0
	for _, vmInfo := range machineList {
		vmId := vmInfo.Id
		price, err := c.getMachinePrices(vmId)
		if err != nil {
			c.logger.LogAttrs(c.context, slog.LevelWarn, "failed to get machine pricing, skipping VM metric", 
				slog.String("vmId", vmId),
				slog.String("region", vmInfo.Region),
				slog.String("error", err.Error()))
			continue
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
		machineMetricsCount++
	}

	kubernetesDisks := c.DiskStore.GetKubernetesDisks()
	for _, disk := range kubernetesDisks {
		diskPricing, err := c.DiskStore.GetDiskPricing(disk)
		if err != nil {
			c.logger.LogAttrs(c.context, slog.LevelWarn, "failed to get disk pricing", 
				slog.String("disk", disk.Name),
				slog.String("error", err.Error()))
			continue
		}

		pricePerGBHour := diskPricing.RetailPrice / float64(disk.Size)

		diskLabelValues := []string{
			disk.PersistentVolumeName,
			disk.Location,
			disk.Zone,
			disk.Name,
			disk.GetSKUForPricing(),
			fmt.Sprintf("%d", disk.Size),
			disk.State,
		}

		ch <- prometheus.MustNewConstMetric(PersistentVolumeHourlyCostDesc, prometheus.GaugeValue, pricePerGBHour, diskLabelValues...)
	}

	c.logger.LogAttrs(c.context, slog.LevelInfo, "metrics collected", 
		slog.Duration("duration", time.Since(now)),
		slog.Int("machines_total", len(machineList)),
		slog.Int("machines_with_pricing", machineMetricsCount),
		slog.Int("persistent_volumes", len(kubernetesDisks)))
	return nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- InstanceCPUHourlyCostDesc
	ch <- InstanceMemoryHourlyCostDesc
	ch <- InstanceTotalHourlyCostDesc
	ch <- PersistentVolumeHourlyCostDesc
	return nil
}

func (c *Collector) Name() string {
	return subsystem
}

func (c *Collector) Register(_ provider.Registry) error {
	c.logger.LogAttrs(c.context, slog.LevelInfo, "registering collector")
	return nil
}
