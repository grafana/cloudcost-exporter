package azureClientWrapper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v5"
	"github.com/Azure/go-autorest/autorest/to"

	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

var ErrClientCreationFailure = errors.New("failed to create client")

type AzureClient interface {
	// Machine Store
	ListClustersInSubscription(context.Context) ([]*armcontainerservice.ManagedCluster, error)
	ListVirtualMachineScaleSetsOwnedVms(context.Context, string, string) ([]*armcompute.VirtualMachineScaleSetVM, error)
	ListVirtualMachineScaleSetsFromResourceGroup(context.Context, string) ([]*armcompute.VirtualMachineScaleSet, error)
	ListMachineTypesByLocation(context.Context, string) ([]*armcompute.VirtualMachineSize, error)

	// Price Store
	ListPrices(context.Context, *retailPriceSdk.RetailPricesClientListOptions) ([]*retailPriceSdk.ResourceSKU, error)
}

type AzClientWrapper struct {
	logger *slog.Logger

	azVMSizesClient *armcompute.VirtualMachineSizesClient
	azVMSSClient    *armcompute.VirtualMachineScaleSetsClient
	azVMSSVmClient  *armcompute.VirtualMachineScaleSetVMsClient
	azAksClient     *armcontainerservice.ManagedClustersClient

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

		retailPricesClient: retailPricesClient,
	}, nil
}

var ErrPageAdvanceFailure = errors.New("failed to advance page")

func (a *AzClientWrapper) ListVirtualMachineScaleSetsOwnedVms(ctx context.Context, rgName, vmssName string) ([]*armcompute.VirtualMachineScaleSetVM, error) {
	vmList := []*armcompute.VirtualMachineScaleSetVM{}

	opts := &armcompute.VirtualMachineScaleSetVMsClientListOptions{
		Expand: to.StringPtr("instanceView"),
	}
	pager := a.azVMSSVmClient.NewListPager(rgName, vmssName, opts)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			return nil, ErrPageAdvanceFailure
		}

		vmList = append(vmList, nextResult.Value...)
	}

	return vmList, nil
}

func (a *AzClientWrapper) ListVirtualMachineScaleSetsFromResourceGroup(ctx context.Context, rgName string) ([]*armcompute.VirtualMachineScaleSet, error) {
	vmssList := []*armcompute.VirtualMachineScaleSet{}

	pager := a.azVMSSClient.NewListPager(rgName, nil)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			a.logger.LogAttrs(ctx, slog.LevelError, "unable to advance page in VMSS Client", slog.String("err", err.Error()))
			return nil, ErrPageAdvanceFailure
		}

		vmssList = append(vmssList, nextResult.Value...)
	}

	return vmssList, nil
}

func (a *AzClientWrapper) ListClustersInSubscription(ctx context.Context) ([]*armcontainerservice.ManagedCluster, error) {
	clusterList := []*armcontainerservice.ManagedCluster{}

	pager := a.azAksClient.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			a.logger.LogAttrs(ctx, slog.LevelError, "unable to advance page in AKS Client", slog.String("err", err.Error()))
			return nil, ErrPageAdvanceFailure
		}
		clusterList = append(clusterList, page.Value...)
	}

	return clusterList, nil
}

func (a *AzClientWrapper) ListMachineTypesByLocation(ctx context.Context, region string) ([]*armcompute.VirtualMachineSize, error) {
	machineList := []*armcompute.VirtualMachineSize{}

	pager := a.azVMSizesClient.NewListPager(region, nil)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			a.logger.LogAttrs(ctx, slog.LevelError, "unable to advance page in VM Sizes Client", slog.String("err", err.Error()))
			return nil, ErrPageAdvanceFailure
		}

		machineList = append(machineList, nextResult.Value...)
	}

	return machineList, nil
}

func (a *AzClientWrapper) ListPrices(ctx context.Context, searchOptions *retailPriceSdk.RetailPricesClientListOptions) ([]*retailPriceSdk.ResourceSKU, error) {
	a.logger.LogAttrs(ctx, slog.LevelDebug, "populating prices with opts", slog.String("opts", fmt.Sprintf("%+v", searchOptions)))
	prices := []*retailPriceSdk.ResourceSKU{}

	pager := a.retailPricesClient.NewListPager(searchOptions)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			a.logger.LogAttrs(ctx, slog.LevelError, "error paging through retail prices")
			return nil, err
		}

		for _, v := range page.Items {
			prices = append(prices, &v)
		}
	}

	return prices, nil
}
