package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azmetrics"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v9"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/eventhub/armeventhub"
	"github.com/Azure/go-autorest/autorest/to"

	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

var (
	ErrClientCreationFailure = errors.New("failed to create client")
	ErrPageAdvanceFailure    = errors.New("failed to advance page")
)

type AzClientWrapper struct {
	logger *slog.Logger

	subscriptionID string
	credential     *azidentity.DefaultAzureCredential

	azVMSizesClient *armcompute.VirtualMachineSizesClient
	azVMSSClient    *armcompute.VirtualMachineScaleSetsClient
	azVMSSVmClient  *armcompute.VirtualMachineScaleSetVMsClient
	azAksClient     *armcontainerservice.ManagedClustersClient
	azDisksClient   *armcompute.DisksClient
	azEventHubsNS   *armeventhub.NamespacesClient

	retailPricesClient *retailPriceSdk.RetailPricesClient
	metricsClientsMu   sync.Mutex
	metricsClients     map[string]*azmetrics.Client
}

func NewAzureClientWrapper(ctx context.Context, logger *slog.Logger, subscriptionId string, credentials *azidentity.DefaultAzureCredential) (*AzClientWrapper, error) {
	computeClientFactory, err := armcompute.NewClientFactory(subscriptionId, credentials, nil)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "unable to create compute client factory", slog.String("err", err.Error()))
		return nil, ErrClientCreationFailure
	}

	containerClientFactory, err := armcontainerservice.NewClientFactory(subscriptionId, credentials, nil)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "unable to create container client factory", slog.String("err", err.Error()))
		return nil, ErrClientCreationFailure
	}

	eventHubNamespacesClient, err := armeventhub.NewNamespacesClient(subscriptionId, credentials, nil)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "unable to create event hubs namespaces client", slog.String("err", err.Error()))
		return nil, ErrClientCreationFailure
	}

	retailPricesClient, err := retailPriceSdk.NewRetailPricesClient(nil)
	if err != nil {
		return nil, ErrClientCreationFailure
	}

	return &AzClientWrapper{
		logger: logger.With("client", "azure"),

		subscriptionID: subscriptionId,
		credential:     credentials,

		azVMSizesClient: computeClientFactory.NewVirtualMachineSizesClient(),
		azVMSSClient:    computeClientFactory.NewVirtualMachineScaleSetsClient(),
		azVMSSVmClient:  computeClientFactory.NewVirtualMachineScaleSetVMsClient(),
		azAksClient:     containerClientFactory.NewManagedClustersClient(),
		azDisksClient:   computeClientFactory.NewDisksClient(),
		azEventHubsNS:   eventHubNamespacesClient,

		retailPricesClient: retailPricesClient,
		metricsClients:     make(map[string]*azmetrics.Client),
	}, nil
}

func (a *AzClientWrapper) ListVirtualMachineScaleSetsOwnedVms(ctx context.Context, rgName, vmssName string) ([]*armcompute.VirtualMachineScaleSetVM, error) {
	logger := a.logger.With("pager", "listVirtualMachineScaleSetsOwnedVms")

	vmList := make([]*armcompute.VirtualMachineScaleSetVM, 0)

	opts := &armcompute.VirtualMachineScaleSetVMsClientListOptions{
		Expand: to.StringPtr("instanceView"),
	}
	pager := a.azVMSSVmClient.NewListPager(rgName, vmssName, opts)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelError, "unable to advance page", slog.String("err", err.Error()))
			return nil, fmt.Errorf("%w: %w", ErrPageAdvanceFailure, err)
		}

		vmList = append(vmList, nextResult.Value...)
	}

	return vmList, nil
}

func (a *AzClientWrapper) ListVirtualMachineScaleSetsFromResourceGroup(ctx context.Context, rgName string) ([]*armcompute.VirtualMachineScaleSet, error) {
	logger := a.logger.With("pager", "listVirtualMachineScaleSetsFromResourceGroup")

	vmssList := make([]*armcompute.VirtualMachineScaleSet, 0)

	pager := a.azVMSSClient.NewListPager(rgName, nil)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelError, "unable to advance page", slog.String("err", err.Error()))
			return nil, fmt.Errorf("%w: %w", ErrPageAdvanceFailure, err)
		}

		vmssList = append(vmssList, nextResult.Value...)
	}

	return vmssList, nil
}

func (a *AzClientWrapper) ListClustersInSubscription(ctx context.Context) ([]*armcontainerservice.ManagedCluster, error) {
	logger := a.logger.With("pager", "listClustersInSubscription")

	clusterList := make([]*armcontainerservice.ManagedCluster, 0)

	pager := a.azAksClient.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelError, "unable to advance page", slog.String("err", err.Error()))
			return nil, fmt.Errorf("%w: %w", ErrPageAdvanceFailure, err)
		}
		clusterList = append(clusterList, page.Value...)
	}

	return clusterList, nil
}

func (a *AzClientWrapper) ListEventHubNamespaces(ctx context.Context) ([]*armeventhub.EHNamespace, error) {
	logger := a.logger.With("pager", "listEventHubNamespaces")

	namespaces := make([]*armeventhub.EHNamespace, 0)

	pager := a.azEventHubsNS.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelError, "unable to advance page", slog.String("err", err.Error()))
			return nil, fmt.Errorf("%w: %w", ErrPageAdvanceFailure, err)
		}

		namespaces = append(namespaces, page.Value...)
	}

	return namespaces, nil
}

func (a *AzClientWrapper) ListMachineTypesByLocation(ctx context.Context, region string) ([]*armcompute.VirtualMachineSize, error) {
	logger := a.logger.With("pager", "listMachineTypesByLocation")

	machineList := make([]*armcompute.VirtualMachineSize, 0)

	pager := a.azVMSizesClient.NewListPager(region, nil)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelError, "unable to advance page", slog.String("err", err.Error()))
			return nil, fmt.Errorf("%w: %w", ErrPageAdvanceFailure, err)
		}

		machineList = append(machineList, nextResult.Value...)
	}

	return machineList, nil
}

// ListDisksInSubscription retrieves all Azure Managed Disks in the subscription.
// Used for persistent volume cost tracking and Kubernetes metadata extraction.
func (a *AzClientWrapper) ListDisksInSubscription(ctx context.Context) ([]*armcompute.Disk, error) {
	logger := a.logger.With("pager", "listDisksInSubscription")

	diskList := make([]*armcompute.Disk, 0)

	pager := a.azDisksClient.NewListPager(nil)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelError, "unable to advance page", slog.String("err", err.Error()))
			return nil, fmt.Errorf("%w: %w", ErrPageAdvanceFailure, err)
		}

		diskList = append(diskList, nextResult.Value...)
	}

	return diskList, nil
}

func (a *AzClientWrapper) ListPrices(ctx context.Context, searchOptions *retailPriceSdk.RetailPricesClientListOptions) ([]*retailPriceSdk.ResourceSKU, error) {
	logger := a.logger.With("pager", "listPrices")

	logger.LogAttrs(ctx, slog.LevelDebug, "populating prices with opts", slog.String("opts", fmt.Sprintf("%+v", searchOptions)))
	prices := make([]*retailPriceSdk.ResourceSKU, 0)

	pager := a.retailPricesClient.NewListPager(searchOptions)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelError, "unable to advance page", slog.String("err", err.Error()))
			return nil, fmt.Errorf("%w: %w", ErrPageAdvanceFailure, err)
		}

		for _, v := range page.Items {
			prices = append(prices, &v)
		}
	}

	return prices, nil
}

func (a *AzClientWrapper) QueryResourceMetrics(
	ctx context.Context,
	region string,
	metricNamespace string,
	metricNames []string,
	resourceIDs []string,
	options *azmetrics.QueryResourcesOptions,
) (azmetrics.QueryResourcesResponse, error) {
	if len(resourceIDs) == 0 {
		return azmetrics.QueryResourcesResponse{}, nil
	}

	metricsClient, err := a.metricsClient(region)
	if err != nil {
		return azmetrics.QueryResourcesResponse{}, err
	}

	return metricsClient.QueryResources(
		ctx,
		a.subscriptionID,
		metricNamespace,
		metricNames,
		azmetrics.ResourceIDList{ResourceIDs: resourceIDs},
		options,
	)
}

func (a *AzClientWrapper) metricsClient(region string) (*azmetrics.Client, error) {
	normalizedRegion := strings.ToLower(strings.TrimSpace(region))
	if normalizedRegion == "" {
		return nil, fmt.Errorf("region is required for Azure Monitor metrics queries")
	}

	a.metricsClientsMu.Lock()
	defer a.metricsClientsMu.Unlock()

	if client := a.metricsClients[normalizedRegion]; client != nil {
		return client, nil
	}

	endpoint := fmt.Sprintf("https://%s.metrics.monitor.azure.com", normalizedRegion)
	client, err := azmetrics.NewClient(endpoint, a.credential, nil)
	if err != nil {
		return nil, err
	}

	a.metricsClients[normalizedRegion] = client
	return client, nil
}
