package aks

import (
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

var (
	aksTestLogger *slog.Logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
)

func TestNew(t *testing.T) {
	t.Skip()
	// Note - testing the new functionality doesn't really do anything useful.
	// We test the Populate* functions in their respective tests, and we'd really just
	// be wrapping those tests.
}

func TestGetMachinePrices(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	testTable := map[string]struct {
		machineStore *MachineStore
		priceStore   *PriceStore

		vmId           string
		expectedErr    error
		expectedPrices *MachineSku
	}{
		"nil machine store": {
			machineStore: &MachineStore{machineMapLock: &sync.RWMutex{}, logger: aksTestLogger},
			priceStore: &PriceStore{
				logger:        aksTestLogger,
				regionMapLock: &sync.RWMutex{},
				RegionMap: map[string]PriceByPriority{
					"centralus": {
						OnDemand: {
							Linux: {
								"Standard_D4s_v3": {
									RetailPrice: 0.1,
								},
							},
						},
					},
				},
			},

			vmId:           "vmId",
			expectedErr:    ErrMachineNotFound,
			expectedPrices: nil,
		},

		"machine store missing ID": {
			machineStore: &MachineStore{
				logger:         aksTestLogger,
				machineMapLock: &sync.RWMutex{},
				MachineMap: map[string]*VirtualMachineInfo{
					"vmWrongId": {},
				},
			},
			priceStore: &PriceStore{
				logger:        aksTestLogger,
				regionMapLock: &sync.RWMutex{},
				RegionMap: map[string]PriceByPriority{
					"centralus": {
						OnDemand: {
							Linux: {
								"Standard_D4s_v3": {
									RetailPrice: 0.1,
								},
							},
						},
					},
				},
			},

			vmId:           "vmId",
			expectedErr:    ErrMachineNotFound,
			expectedPrices: nil,
		},

		"nil vm passed to price store": {
			machineStore: &MachineStore{
				logger:         aksTestLogger,
				machineMapLock: &sync.RWMutex{},
				MachineMap: map[string]*VirtualMachineInfo{
					"vmId": nil,
				},
			},
			priceStore: &PriceStore{
				logger:        aksTestLogger,
				regionMapLock: &sync.RWMutex{},
				RegionMap:     map[string]PriceByPriority{},
			},

			vmId:           "vmId",
			expectedErr:    ErrPriceInformationNotFound,
			expectedPrices: nil,
		},

		"price store wrong region": {
			machineStore: &MachineStore{
				logger:         aksTestLogger,
				machineMapLock: &sync.RWMutex{},
				MachineMap: map[string]*VirtualMachineInfo{
					"vmId": {
						Name:            "vmId",
						Id:              "vmId",
						Region:          "centralus",
						MachineTypeSku:  "Standard_D4s_v3",
						OperatingSystem: Linux,
						Priority:        OnDemand,
					},
				},
			},
			priceStore: &PriceStore{
				logger:        aksTestLogger,
				regionMapLock: &sync.RWMutex{},
				RegionMap: map[string]PriceByPriority{
					"westus": {
						OnDemand: {
							Linux: {
								"Standard_D4s_v3": {
									RetailPrice: 0.1,
								},
							},
						},
					},
				},
			},

			vmId:           "vmId",
			expectedErr:    ErrPriceInformationNotFound,
			expectedPrices: nil,
		},

		"base case": {
			machineStore: &MachineStore{
				logger:         aksTestLogger,
				machineMapLock: &sync.RWMutex{},
				MachineMap: map[string]*VirtualMachineInfo{
					"vmId": {
						Name:            "vmId",
						Id:              "vmId",
						Region:          "centralus",
						MachineTypeSku:  "Standard_D4s_v3",
						OperatingSystem: Linux,
						Priority:        OnDemand,
					},
				},
			},
			priceStore: &PriceStore{
				logger:        aksTestLogger,
				regionMapLock: &sync.RWMutex{},
				RegionMap: map[string]PriceByPriority{
					"centralus": {
						OnDemand: {
							Linux: {
								"Standard_D4s_v3": {
									RetailPrice: 0.1,
									MachinePricesBreakdown: &MachinePrices{
										PricePerCore: 0.2,
										PricePerGiB:  0.3,
									},
								},
							},
						},
					},
				},
			},

			vmId:        "vmId",
			expectedErr: nil,
			expectedPrices: &MachineSku{
				RetailPrice: 0.1,
				MachinePricesBreakdown: &MachinePrices{
					PricePerCore: 0.2,
					PricePerGiB:  0.3,
				},
			},
		},
	}

	for name, tc := range testTable {
		t.Run(name, func(t *testing.T) {
			fakeAksCollector := &Collector{}
			fakeAksCollector.MachineStore = tc.machineStore
			fakeAksCollector.PriceStore = tc.priceStore

			prices, err := fakeAksCollector.getMachinePrices(tc.vmId)
			if tc.expectedErr != nil {
				assert.ErrorIs(t, err, tc.expectedErr)
			}

			eq := reflect.DeepEqual(tc.expectedPrices, prices)
			assert.True(t, eq, fmt.Sprintf("prices are not equal: expected %+v, returned %+v", tc.expectedPrices, prices))
		})
	}
}
