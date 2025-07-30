package client

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v5"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

//go:generate mockgen -source=client.go -destination mocks/client.go

type AzureClient interface {
	// Machine Store
	ListClustersInSubscription(context.Context) ([]*armcontainerservice.ManagedCluster, error)
	ListVirtualMachineScaleSetsOwnedVms(context.Context, string, string) ([]*armcompute.VirtualMachineScaleSetVM, error)
	ListVirtualMachineScaleSetsFromResourceGroup(context.Context, string) ([]*armcompute.VirtualMachineScaleSet, error)
	ListMachineTypesByLocation(context.Context, string) ([]*armcompute.VirtualMachineSize, error)

	// Price Store
	ListPrices(context.Context, *retailPriceSdk.RetailPricesClientListOptions) ([]*retailPriceSdk.ResourceSKU, error)
}
