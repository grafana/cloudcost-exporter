package aks

import (
	"testing"

	"github.com/stretchr/testify/assert"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

func TestPriceStoreMapCreation(t *testing.T) {
	// TODO - mock
	t.Skip()
}

func TestDetermineMachineOperatingSystem(t *testing.T) {
	testTable := map[string]struct {
		sku             retailPriceSdk.ResourceSKU
		expectedMachine MachineOperatingSystem
	}{
		"Linux": {
			sku: retailPriceSdk.ResourceSKU{
				ProductName: "Virtual Machines Esv4 Series",
			},
			expectedMachine: Linux,
		},
		"Windows": {
			sku: retailPriceSdk.ResourceSKU{
				ProductName: "Virtual Machines D Series Windows",
			},
			expectedMachine: Windows,
		},
	}

	for name, test := range testTable {
		t.Run(name, func(t *testing.T) {
			machineOs := getMachineOperatingSystemFromSku(test.sku)
			assert.Equal(t, test.expectedMachine, machineOs)
		})
	}
}

func TestDetermineMachinePriority(t *testing.T) {
	testTable := map[string]struct {
		sku              retailPriceSdk.ResourceSKU
		expectedPriority MachinePriority
	}{
		"OnDemand": {
			sku: retailPriceSdk.ResourceSKU{
				SkuName: "Standard_E16pds_v5 Low Priority",
			},
			expectedPriority: OnDemand,
		},
		"Spot": {
			sku: retailPriceSdk.ResourceSKU{
				SkuName: "B4ls v2 Spot",
			},
			expectedPriority: Spot,
		},
	}

	for name, test := range testTable {
		t.Run(name, func(t *testing.T) {
			machinePriority := getMachinePriorityFromSku(test.sku)
			assert.Equal(t, test.expectedPriority, machinePriority)
		})
	}
}
