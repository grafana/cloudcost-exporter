package aks

import (
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

var (
	aksTestLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))
)

func TestNew(t *testing.T) {
	t.Skip()
	// Note - testing the new functionality doesn't really do anything useful.
	// We test the Populate* functions in their respective tests, and we'd really just
	// be wrapping those tests.
}

func TestCollect(t *testing.T) {
	// Note, this will not test a ton of the underlying functionality of the
	// Machine Store and the Pricing Store, as those are individually tested
	// in their respective *_test.go files
	testTable := map[string]struct {
		machineStore *MachineStore
		priceStore   *PriceStore
		diskStore    *DiskStore

		expectedErr error
	}{
		"error getting machine prices": {
			machineStore: &MachineStore{
				logger:         aksTestLogger,
				machineMapLock: &sync.RWMutex{},
				MachineMap: map[string]*VirtualMachineInfo{
					"vmId": {
						Name:            "vmId",
						Id:              "vmId",
						Region:          "centralus",
						MachineTypeSku:  "Standard_D4s_v3",
						MachineFamily:   "General purpose",
						OwningCluster:   "cluster",
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
									MachinePricesBreakdown: &MachinePrices{
										PricePerCore: 0.1,
										PricePerGiB:  0.1,
									},
								},
							},
						},
					},
				},
			},
			diskStore: &DiskStore{
				logger:      aksTestLogger,
				mu:          sync.RWMutex{},
				disks:       make(map[string]*Disk),
				diskPricing: make(map[string]*DiskPricing),
			},

			expectedErr: nil,
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
						MachineFamily:   "General purpose",
						OwningCluster:   "cluster",
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
										PricePerCore: 0.1,
										PricePerGiB:  0.1,
									},
								},
							},
						},
					},
				},
			},
			diskStore: &DiskStore{
				logger:      aksTestLogger,
				mu:          sync.RWMutex{},
				disks:       make(map[string]*Disk),
				diskPricing: make(map[string]*DiskPricing),
			},

			expectedErr: nil,
		},
	}

	for name, tc := range testTable {
		t.Run(name, func(t *testing.T) {
			fakeAksCollector := &Collector{
				logger: aksTestLogger,
			}
			fakeAksCollector.MachineStore = tc.machineStore
			fakeAksCollector.PriceStore = tc.priceStore
			fakeAksCollector.DiskStore = tc.diskStore

			promCh := make(chan prometheus.Metric)

			go func() {
				err := fakeAksCollector.Collect(t.Context(), promCh)
				if tc.expectedErr != nil {
					assert.ErrorIs(t, err, tc.expectedErr)
				}
				close(promCh)
			}()

			for metric := range promCh {
				assert.NotNil(t, metric)
				assert.Contains(t, metric.Desc().String(), "cloudcost_azure_aks")
			}
		})
	}

}

func TestGetMachinePrices(t *testing.T) {
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
