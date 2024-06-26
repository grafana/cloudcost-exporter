package aks

import (
	"context"
	"errors"
	"log/slog"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/cloudcost-exporter/pkg/provider"

	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

const (
	subsystem = "azure_aks"
)

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

	resourceGroupClient          *armresources.ResourceGroupsClient
	virtualMachineClient         *armcompute.VirtualMachineScaleSetVMsClient
	virtualMachineScaleSetClient *armcompute.VirtualMachineScaleSetsClient

	PriceStore *PriceStore
}

type Config struct {
	Logger      *slog.Logger
	Credentials *azidentity.DefaultAzureCredential

	SubscriptionId string
}

func New(ctx context.Context, cfg *Config) (*Collector, error) {
	logger := cfg.Logger.With("collector", "aks")

	retailPricesClient, err := retailPriceSdk.NewRetailPricesClient(nil)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "failed to create retail prices client", slog.String("err", err.Error()))
		return nil, ErrClientCreationFailure
	}

	rgClient, err := armresources.NewResourceGroupsClient(cfg.SubscriptionId, cfg.Credentials, nil)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "failed to create resource group client", slog.String("err", err.Error()))
		return nil, ErrClientCreationFailure
	}

	computeClientFactory, err := armcompute.NewClientFactory(cfg.SubscriptionId, cfg.Credentials, nil)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "failed to create compute client factory", slog.String("err", err.Error()))
		return nil, ErrClientCreationFailure
	}

	return &Collector{
		context: ctx,
		logger:  logger,

		resourceGroupClient:          rgClient,
		virtualMachineClient:         computeClientFactory.NewVirtualMachineScaleSetVMsClient(),
		virtualMachineScaleSetClient: computeClientFactory.NewVirtualMachineScaleSetsClient(),

		PriceStore: NewPricingStore(cfg.SubscriptionId, retailPricesClient, logger, ctx),
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
