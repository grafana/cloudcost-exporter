package aks

import (
	"errors"
	"log/slog"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/Azure/go-autorest/autorest/to"
	mock_client "github.com/grafana/cloudcost-exporter/pkg/azure/client/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

var (
	priceStoreTestLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))
)

func TestPopulatePriceStore(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	defaultListOpts := &retailPriceSdk.RetailPricesClientListOptions{
		APIVersion:  to.StringPtr(AZ_API_VERSION),
		Filter:      to.StringPtr(AzurePriceSearchFilter),
		MeterRegion: to.StringPtr(AzureMeterRegion),
	}

	mockAzureClient := mock_client.NewMockAzureClient(ctrl)

	testTable := map[string]struct {
		expectedErr           error
		listOpts              *retailPriceSdk.RetailPricesClientListOptions
		apiReturns            []*retailPriceSdk.ResourceSKU
		expectedPriceMap      map[string]PriceByPriority
		timesToCallListPrices int
	}{
		"base case": {
			expectedErr: nil,
			listOpts:    defaultListOpts,
			expectedPriceMap: map[string]PriceByPriority{
				"westus": {
					OnDemand: PriceByOperatingSystem{
						Linux: PriceBySku{
							"Standard_D4s_v3": &MachineSku{
								RetailPrice: 0.1,
							},
						},
					},
					Spot: PriceByOperatingSystem{},
				},
				"centraleurope": {
					OnDemand: PriceByOperatingSystem{
						Linux: PriceBySku{
							"Standard_D8s_v3": &MachineSku{
								RetailPrice: 0.1,
							},
						},
					},
					Spot: PriceByOperatingSystem{
						Linux: PriceBySku{
							"Standard_D16s_v3": &MachineSku{
								RetailPrice: 0.01,
							},
						},
					},
				},
			},

			apiReturns: []*retailPriceSdk.ResourceSKU{
				{ArmSkuName: "Standard_D4s_v3", SkuName: "D4s v3", ArmRegionName: "westus", ProductName: "Virtual Machines D Series", RetailPrice: 0.1},
				{ArmSkuName: "Standard_D8s_v3", SkuName: "D8s v3", ArmRegionName: "centraleurope", ProductName: "Virtual Machines D Series", RetailPrice: 0.1},
				{ArmSkuName: "Standard_D16s_v3", SkuName: "D16s v3 Spot", ArmRegionName: "centraleurope", ProductName: "Virtual Machines D Series", RetailPrice: 0.01},
				{ArmSkuName: "Standard_D4s_v3", SkuName: "D4s v3 Low Priority", ArmRegionName: "centraleurope", ProductName: "Virtual Machines D Series", RetailPrice: 0.01}, // low priority machines are disregarded
			},

			timesToCallListPrices: 1,
		},
		"err case": {
			expectedErr:           errors.New("error paging through retail prices"),
			listOpts:              defaultListOpts,
			expectedPriceMap:      map[string]PriceByPriority{},
			apiReturns:            nil,
			timesToCallListPrices: 5,
		},
	}

	for name, tc := range testTable {
		t.Run(name, func(t *testing.T) {
			call := mockAzureClient.EXPECT().ListPrices(gomock.Any(), gomock.Any())
			call.Times(tc.timesToCallListPrices).Return(tc.apiReturns, tc.expectedErr)

			p := &PriceStore{
				logger:             priceStoreTestLogger,
				context:            t.Context(),
				azureClientWrapper: mockAzureClient,

				regionMapLock: &sync.RWMutex{},
				RegionMap:     make(map[string]PriceByPriority),
			}

			p.PopulatePriceStore(t.Context())

			mapEq := reflect.DeepEqual(tc.expectedPriceMap, p.RegionMap)
			assert.True(t, mapEq)
		})
	}
}

func TestGetPriceInfoFromVmInfo(t *testing.T) {
	fakePriceStore := &PriceStore{
		RegionMap: map[string]PriceByPriority{
			"region1": {
				OnDemand: PriceByOperatingSystem{
					Linux: PriceBySku{
						"sku1": &MachineSku{
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
		vmInfo           *VirtualMachineInfo
		expectedPrice    float64
		expectedCpuPrice float64
		expectedRamPrice float64
		expectedErr      error
	}{
		"nil vm info": {
			vmInfo:           nil,
			expectedPrice:    0.0,
			expectedCpuPrice: 0.0,
			expectedRamPrice: 0.0,
			expectedErr:      ErrPriceInformationNotFound,
		},

		"missing region": {
			vmInfo: &VirtualMachineInfo{
				Priority:        OnDemand,
				OperatingSystem: Linux,
				MachineTypeSku:  "sku1",
			},
			expectedPrice:    0.0,
			expectedCpuPrice: 0.0,
			expectedRamPrice: 0.0,
			expectedErr:      ErrPriceInformationNotFound,
		},
		"missing sku": {
			vmInfo: &VirtualMachineInfo{
				Priority:        OnDemand,
				Region:          "region1",
				OperatingSystem: Linux,
			},
			expectedPrice:    0.0,
			expectedCpuPrice: 0.0,
			expectedRamPrice: 0.0,
			expectedErr:      ErrPriceInformationNotFound,
		},
		"all information complete": {
			vmInfo: &VirtualMachineInfo{
				Priority:        OnDemand,
				Region:          "region1",
				OperatingSystem: Linux,
				MachineTypeSku:  "sku1",
				MachineFamily:   "General purpose",

				NumOfCores:  4.0,
				MemoryInMiB: 16000.0,
			},
			expectedPrice:    1.0,
			expectedCpuPrice: 0.1625,
			expectedRamPrice: 0.0224,
			expectedErr:      nil,
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
			if test.expectedErr != nil {
				assert.Equal(t, test.expectedErr, err)
			} else {
				assert.Nil(t, err)
				assert.NotNil(t, price.MachinePricesBreakdown)
				assert.Equal(t, test.expectedPrice, price.RetailPrice)
				assert.Equal(t, test.expectedCpuPrice, price.MachinePricesBreakdown.PricePerCore)
				assert.Equal(t, test.expectedRamPrice, price.MachinePricesBreakdown.PricePerGiB)
			}
		})
	}
}

func TestDetermineMachineOperatingSystem(t *testing.T) {
	testTable := map[string]struct {
		sku             *retailPriceSdk.ResourceSKU
		expectedMachine MachineOperatingSystem
	}{
		"Linux": {
			sku: &retailPriceSdk.ResourceSKU{
				ProductName: "Virtual Machines Esv4 Series",
			},
			expectedMachine: Linux,
		},
		"Windows": {
			sku: &retailPriceSdk.ResourceSKU{
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
		sku              *retailPriceSdk.ResourceSKU
		expectedPriority MachinePriority
	}{
		"OnDemand": {
			sku: &retailPriceSdk.ResourceSKU{
				SkuName: "Standard_E16pds_v5 Low Priority",
			},
			expectedPriority: OnDemand,
		},
		"Spot": {
			sku: &retailPriceSdk.ResourceSKU{
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
