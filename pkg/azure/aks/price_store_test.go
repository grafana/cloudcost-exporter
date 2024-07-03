package aks

import (
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

func TestPriceStoreMapCreation(t *testing.T) {
	// TODO - mock
	t.Skip()
}

func TestGetPriceInfoFromVmInfo(t *testing.T) {
	fakePriceStore := &PriceStore{
		RegionMap: map[string]PriceByPriority{
			"region1": {
				OnDemand: PriceByOperatingSystem{
					Linux: PriceBySku{
						"sku1": &retailPriceSdk.ResourceSKU{
							RetailPrice: 1.0,
						},
					},
				},
			},
		},
		regionMapLock: &sync.RWMutex{},
		logger:        slog.Default(),
	}

	testTable := map[string]struct {
		vmInfo        *VirtualMachineInfo
		expectedPrice float64
		expectedErr   error
	}{
		"nil vm info": {
			vmInfo:        nil,
			expectedPrice: 0.0,
			expectedErr:   ErrPriceInformationNotFound,
		},

		"missing region": {
			vmInfo: &VirtualMachineInfo{
				Priority:        OnDemand,
				OperatingSystem: Linux,
				MachineTypeSku:  "sku1",
			},
			expectedPrice: 0.0,
			expectedErr:   ErrPriceInformationNotFound,
		},
		"missing sku": {
			vmInfo: &VirtualMachineInfo{
				Priority:        OnDemand,
				Region:          "region1",
				OperatingSystem: Linux,
			},
			expectedPrice: 0.0,
			expectedErr:   ErrPriceInformationNotFound,
		},
		"all information complete": {
			vmInfo: &VirtualMachineInfo{
				Priority:        OnDemand,
				Region:          "region1",
				OperatingSystem: Linux,
				MachineTypeSku:  "sku1",
			},
			expectedPrice: 1.0,
			expectedErr:   nil,
		},
		"all information complete but not in map": {
			vmInfo: &VirtualMachineInfo{
				Priority:        OnDemand,
				Region:          "region2",
				OperatingSystem: Windows,
				MachineTypeSku:  "sku3",
			},
			expectedPrice: 0.0,
			expectedErr:   ErrPriceInformationNotFound,
		},
	}

	for name, test := range testTable {
		t.Run(name, func(t *testing.T) {
			price, err := fakePriceStore.getPriceInfoFromVmInfo(test.vmInfo)

			assert.Equal(t, test.expectedPrice, price)
			if test.expectedErr != nil {
				assert.Equal(t, test.expectedErr, err)
			} else {
				assert.Nil(t, err)
			}

		})
	}
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
