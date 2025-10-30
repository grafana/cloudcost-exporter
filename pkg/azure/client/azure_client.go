package client

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v8"
	"github.com/Azure/go-autorest/autorest/to"

	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

var (
	ErrClientCreationFailure = errors.New("failed to create client")
	ErrPageAdvanceFailure    = errors.New("failed to advance page")
)

type AzClientWrapper struct {
	logger *slog.Logger

	azVMSizesClient *armcompute.VirtualMachineSizesClient
	azVMSSClient    *armcompute.VirtualMachineScaleSetsClient
	azVMSSVmClient  *armcompute.VirtualMachineScaleSetVMsClient
	azAksClient     *armcontainerservice.ManagedClustersClient
	azDisksClient   *armcompute.DisksClient

	retailPricesClient *retailPriceSdk.RetailPricesClient
}

func NewAzureClientWrapper(logger *slog.Logger, subscriptionId string, credentials *azidentity.DefaultAzureCredential) (*AzClientWrapper, error) {
	ctx := context.TODO()

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

	retailPricesClient, err := retailPriceSdk.NewRetailPricesClient(nil)
	if err != nil {
		return nil, ErrClientCreationFailure
	}

	return &AzClientWrapper{
		logger: logger.With("client", "azure"),

		azVMSizesClient: computeClientFactory.NewVirtualMachineSizesClient(),
		azVMSSClient:    computeClientFactory.NewVirtualMachineScaleSetsClient(),
		azVMSSVmClient:  computeClientFactory.NewVirtualMachineScaleSetVMsClient(),
		azAksClient:     containerClientFactory.NewManagedClustersClient(),
		azDisksClient:   computeClientFactory.NewDisksClient(),

		retailPricesClient: retailPricesClient,
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
