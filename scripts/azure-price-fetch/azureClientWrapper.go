package main

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

type AzureClientWrapper struct {
	subscriptionId string
	priceClient    *sdk.RetailPricesClient
	rgClient       *armresources.ResourceGroupsClient
	vmssClient     *armcompute.VirtualMachineScaleSetsClient
	vmssVmClient   *armcompute.VirtualMachineScaleSetVMsClient
}

func NewAzureClientWrapper(ctx context.Context, subId string, cred *azidentity.DefaultAzureCredential) (*AzureClientWrapper, error) {
	retailPricesClient, err := sdk.NewRetailPricesClient(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create retail prices client: %w", err)
	}

	rgClient, err := armresources.NewResourceGroupsClient(subId, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build resource group client: %w", err)
	}

	computeClientFactory, err := armcompute.NewClientFactory(subId, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build compute client: %w", err)
	}

	return &AzureClientWrapper{
		subscriptionId: subId,

		priceClient:  retailPricesClient,
		rgClient:     rgClient,
		vmssClient:   computeClientFactory.NewVirtualMachineScaleSetsClient(),
		vmssVmClient: computeClientFactory.NewVirtualMachineScaleSetVMsClient(),
	}, nil
}
