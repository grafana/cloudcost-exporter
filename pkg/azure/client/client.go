package client

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v8"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

//go:generate mockgen -source=client.go -destination mocks/client.go

type AzureClient interface {
	// Machine Store
	ListClustersInSubscription(context.Context) ([]*armcontainerservice.ManagedCluster, error)
	ListVirtualMachineScaleSetsOwnedVms(context.Context, string, string) ([]*armcompute.VirtualMachineScaleSetVM, error)
	ListVirtualMachineScaleSetsFromResourceGroup(context.Context, string) ([]*armcompute.VirtualMachineScaleSet, error)
	ListMachineTypesByLocation(context.Context, string) ([]*armcompute.VirtualMachineSize, error)

	// Disk Store - Azure Managed Disk operations for persistent volume cost tracking
	ListDisksInSubscription(context.Context) ([]*armcompute.Disk, error)

	// Price Store
	ListPrices(context.Context, *retailPriceSdk.RetailPricesClientListOptions) ([]*retailPriceSdk.ResourceSKU, error)
}
