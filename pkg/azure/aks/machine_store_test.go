package aks

import (
	"errors"
	"log/slog"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v8"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/grafana/cloudcost-exporter/pkg/azure/client"
	mock_client "github.com/grafana/cloudcost-exporter/pkg/azure/client/mocks"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

var (
	machineStoreTestLogger *slog.Logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
)

func TestPopulateMachineStore(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	newFakeMachineStore := func(t *testing.T, cli client.AzureClient) *MachineStore {
		return &MachineStore{
			context: t.Context(),
			logger:  machineStoreTestLogger,

			azClientWrapper: cli,

			MachineSizeMap:     make(map[string]map[string]*armcompute.VirtualMachineSize),
			machineSizeMapLock: &sync.RWMutex{},

			MachineMap:     make(map[string]*VirtualMachineInfo),
			machineMapLock: &sync.RWMutex{},
		}
	}

	testTable := map[string]struct {
		fakeMachineStore *MachineStore

		listClustersReturn        []*armcontainerservice.ManagedCluster
		listClustersExpectedError error

		listMachineTypesReturn        []*armcompute.VirtualMachineSize
		listMachineTypesExpectedError error

		listVMSSFromRgExpectedReturn []*armcompute.VirtualMachineScaleSet
		listVMSSFromRgExpectedErr    error

		ListVirtualMachineScaleSetsOwnedVmsExpectedReturn []*armcompute.VirtualMachineScaleSetVM
		ListVirtualMachineScaleSetsOwnedVmsExpectedErr    error

		expectedMachineMap      map[string]*VirtualMachineInfo
		expectedMachineSizesMap map[string]map[string]*armcompute.VirtualMachineSize
	}{
		"list clusters error": {
			listClustersReturn:        nil,
			listClustersExpectedError: errors.New("bad list clusters"),
			expectedMachineMap:        map[string]*VirtualMachineInfo{},
			expectedMachineSizesMap:   map[string]map[string]*armcompute.VirtualMachineSize{},
		},

		"cluster does not contain resource group": {
			listClustersReturn: []*armcontainerservice.ManagedCluster{
				{
					Name:       to.StringPtr("clusterName"),
					Location:   to.StringPtr("centralus"),
					Properties: &armcontainerservice.ManagedClusterProperties{},
				},
			},
			listClustersExpectedError: nil,

			listMachineTypesReturn: []*armcompute.VirtualMachineSize{
				{
					Name:           to.StringPtr("Standard_D4s_v3"),
					MemoryInMB:     to.Int32Ptr(100),
					NumberOfCores:  to.Int32Ptr(10),
					OSDiskSizeInMB: to.Int32Ptr(100),
				},
			},
			listMachineTypesExpectedError: nil,

			listVMSSFromRgExpectedReturn: nil,
			listVMSSFromRgExpectedErr:    nil,

			ListVirtualMachineScaleSetsOwnedVmsExpectedReturn: nil,
			ListVirtualMachineScaleSetsOwnedVmsExpectedErr:    nil,

			expectedMachineMap: map[string]*VirtualMachineInfo{},
			expectedMachineSizesMap: map[string]map[string]*armcompute.VirtualMachineSize{
				"centralus": {
					"Standard_D4s_v3": {
						Name:           to.StringPtr("Standard_D4s_v3"),
						MemoryInMB:     to.Int32Ptr(100),
						NumberOfCores:  to.Int32Ptr(10),
						OSDiskSizeInMB: to.Int32Ptr(100),
					},
				},
			},
		},

		"error getting machine types": {
			listClustersReturn: []*armcontainerservice.ManagedCluster{
				{
					Name:       to.StringPtr("clusterName"),
					Location:   to.StringPtr("centralus"),
					Properties: &armcontainerservice.ManagedClusterProperties{NodeResourceGroup: to.StringPtr("rg1")},
				},
			},
			listClustersExpectedError: nil,

			listMachineTypesReturn:        nil,
			listMachineTypesExpectedError: errors.New("bad list machine types"),

			expectedMachineMap:      map[string]*VirtualMachineInfo{},
			expectedMachineSizesMap: map[string]map[string]*armcompute.VirtualMachineSize{},
		},

		"error getting scale sets from resource group": {
			listClustersReturn: []*armcontainerservice.ManagedCluster{
				{
					Name:       to.StringPtr("clusterName"),
					Location:   to.StringPtr("centralus"),
					Properties: &armcontainerservice.ManagedClusterProperties{NodeResourceGroup: to.StringPtr("rg1")},
				},
			},
			listClustersExpectedError: nil,

			listMachineTypesReturn: []*armcompute.VirtualMachineSize{
				{
					Name:           to.StringPtr("Standard_D4s_v3"),
					MemoryInMB:     to.Int32Ptr(100),
					NumberOfCores:  to.Int32Ptr(10),
					OSDiskSizeInMB: to.Int32Ptr(100),
				},
			},
			listMachineTypesExpectedError: nil,

			listVMSSFromRgExpectedReturn: nil,
			listVMSSFromRgExpectedErr:    errors.New("bad vmss from rg"),

			expectedMachineMap: map[string]*VirtualMachineInfo{},
			expectedMachineSizesMap: map[string]map[string]*armcompute.VirtualMachineSize{
				"centralus": {
					"Standard_D4s_v3": {
						Name:           to.StringPtr("Standard_D4s_v3"),
						MemoryInMB:     to.Int32Ptr(100),
						NumberOfCores:  to.Int32Ptr(10),
						OSDiskSizeInMB: to.Int32Ptr(100),
					},
				},
			},
		},

		"error getting vm info from scale sets": {
			listClustersReturn: []*armcontainerservice.ManagedCluster{
				{
					Name:       to.StringPtr("clusterName"),
					Location:   to.StringPtr("centralus"),
					Properties: &armcontainerservice.ManagedClusterProperties{NodeResourceGroup: to.StringPtr("rg1")},
				},
			},
			listClustersExpectedError: nil,

			listMachineTypesReturn: []*armcompute.VirtualMachineSize{
				{
					Name:           to.StringPtr("Standard_D4s_v3"),
					MemoryInMB:     to.Int32Ptr(100),
					NumberOfCores:  to.Int32Ptr(10),
					OSDiskSizeInMB: to.Int32Ptr(100),
				},
			},
			listMachineTypesExpectedError: nil,

			listVMSSFromRgExpectedReturn: []*armcompute.VirtualMachineScaleSet{
				{
					Name:     to.StringPtr("vmssName"),
					Location: to.StringPtr("centralus"),
					Properties: &armcompute.VirtualMachineScaleSetProperties{
						VirtualMachineProfile: &armcompute.VirtualMachineScaleSetVMProfile{
							Priority: (*armcompute.VirtualMachinePriorityTypes)(to.StringPtr("Regular")),
							OSProfile: &armcompute.VirtualMachineScaleSetOSProfile{
								LinuxConfiguration: &armcompute.LinuxConfiguration{},
							},
						},
					},
				},
			},
			listVMSSFromRgExpectedErr: nil,

			ListVirtualMachineScaleSetsOwnedVmsExpectedReturn: nil,
			ListVirtualMachineScaleSetsOwnedVmsExpectedErr:    errors.New("bad vmss owned VMs"),

			expectedMachineMap: map[string]*VirtualMachineInfo{},
			expectedMachineSizesMap: map[string]map[string]*armcompute.VirtualMachineSize{
				"centralus": {
					"Standard_D4s_v3": {
						Name:           to.StringPtr("Standard_D4s_v3"),
						MemoryInMB:     to.Int32Ptr(100),
						NumberOfCores:  to.Int32Ptr(10),
						OSDiskSizeInMB: to.Int32Ptr(100),
					},
				},
			},
		},

		"base case": {
			listClustersReturn: []*armcontainerservice.ManagedCluster{
				{
					Name:       to.StringPtr("clusterName"),
					Location:   to.StringPtr("centralus"),
					Properties: &armcontainerservice.ManagedClusterProperties{NodeResourceGroup: to.StringPtr("rg1")},
				},
			},
			listClustersExpectedError: nil,

			listMachineTypesReturn: []*armcompute.VirtualMachineSize{
				{
					Name:           to.StringPtr("Standard_D4s_v3"),
					MemoryInMB:     to.Int32Ptr(100),
					NumberOfCores:  to.Int32Ptr(10),
					OSDiskSizeInMB: to.Int32Ptr(100),
				},
			},
			listMachineTypesExpectedError: nil,

			listVMSSFromRgExpectedReturn: []*armcompute.VirtualMachineScaleSet{
				{
					Name:     to.StringPtr("vmssName"),
					Location: to.StringPtr("centralus"),
					Properties: &armcompute.VirtualMachineScaleSetProperties{
						VirtualMachineProfile: &armcompute.VirtualMachineScaleSetVMProfile{
							Priority: (*armcompute.VirtualMachinePriorityTypes)(to.StringPtr("Regular")),
							OSProfile: &armcompute.VirtualMachineScaleSetOSProfile{
								LinuxConfiguration: &armcompute.LinuxConfiguration{},
							},
						},
					},
				},
			},
			listVMSSFromRgExpectedErr: nil,

			ListVirtualMachineScaleSetsOwnedVmsExpectedReturn: []*armcompute.VirtualMachineScaleSetVM{
				{
					Location: to.StringPtr("centralus"),
					Properties: &armcompute.VirtualMachineScaleSetVMProperties{
						InstanceView: &armcompute.VirtualMachineScaleSetVMInstanceView{
							ComputerName: to.StringPtr("vmName"),
						},
						VMID: to.StringPtr("vmId"),
					},
					SKU: &armcompute.SKU{
						Name: to.StringPtr("Standard_D4s_v3"),
					},
				},
			},
			ListVirtualMachineScaleSetsOwnedVmsExpectedErr: nil,

			expectedMachineMap: map[string]*VirtualMachineInfo{
				"vmId": {
					Name:           "vmname",
					Id:             "vmId",
					Region:         "centralus",
					OwningVMSS:     "vmssName",
					OwningCluster:  "clusterName",
					MachineTypeSku: "Standard_D4s_v3",
					MachineFamily:  "General purpose",
					Priority:       OnDemand,
					NumOfCores:     10,
					MemoryInMiB:    100,
					OsDiskSizeInMB: 100,
				},
			},
			expectedMachineSizesMap: map[string]map[string]*armcompute.VirtualMachineSize{
				"centralus": {
					"Standard_D4s_v3": {
						Name:           to.StringPtr("Standard_D4s_v3"),
						MemoryInMB:     to.Int32Ptr(100),
						NumberOfCores:  to.Int32Ptr(10),
						OSDiskSizeInMB: to.Int32Ptr(100),
					},
				},
			},
		},
	}

	for name, tc := range testTable {
		t.Run(name, func(t *testing.T) {
			azClientWrapper := mock_client.NewMockAzureClient(ctrl)

			ms := newFakeMachineStore(t, azClientWrapper)

			azClientWrapper.EXPECT().ListClustersInSubscription(gomock.Any()).AnyTimes().Return(tc.listClustersReturn, tc.listClustersExpectedError)
			azClientWrapper.EXPECT().ListMachineTypesByLocation(gomock.Any(), gomock.Any()).AnyTimes().Return(tc.listMachineTypesReturn, tc.listMachineTypesExpectedError)
			azClientWrapper.EXPECT().ListVirtualMachineScaleSetsFromResourceGroup(gomock.Any(), gomock.Any()).AnyTimes().Return(tc.listVMSSFromRgExpectedReturn, tc.listVMSSFromRgExpectedErr)
			azClientWrapper.EXPECT().ListVirtualMachineScaleSetsOwnedVms(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(tc.ListVirtualMachineScaleSetsOwnedVmsExpectedReturn, tc.ListVirtualMachineScaleSetsOwnedVmsExpectedErr)

			ms.PopulateMachineStore(t.Context())

			machineMapEq := reflect.DeepEqual(tc.expectedMachineMap, ms.MachineMap)
			machineSizeMapEq := reflect.DeepEqual(tc.expectedMachineSizesMap, ms.MachineSizeMap)
			assert.True(t, machineMapEq)
			assert.True(t, machineSizeMapEq)
		})
	}

}

func TestGetListOfVmsForSubscription(t *testing.T) {
	fakeMachineStore := &MachineStore{
		MachineMap:     make(map[string]*VirtualMachineInfo),
		machineMapLock: &sync.RWMutex{},
	}
	fakeMachineStore.MachineMap["vm1"] = &VirtualMachineInfo{
		Name: "vm1",
	}
	fakeMachineStore.MachineMap["vm2"] = &VirtualMachineInfo{
		Name: "vm2",
	}

	testTable := map[string]struct {
		expectedNames []string
	}{
		"base_case": {
			expectedNames: []string{"vm1", "vm2"},
		},
	}

	for name, tc := range testTable {
		t.Run(name, func(t *testing.T) {
			vmList := fakeMachineStore.GetListOfVmsForSubscription()
			var vmNameList []string

			for _, v := range vmList {
				vmNameList = append(vmNameList, v.Name)
			}

			assert.ElementsMatch(t, tc.expectedNames, vmNameList)
		})
	}
}

func TestGetVmInfoByName(t *testing.T) {
	fakeMachineStore := &MachineStore{
		MachineMap:     make(map[string]*VirtualMachineInfo),
		machineMapLock: &sync.RWMutex{},
	}

	fakeMachineStore.MachineMap["vm1"] = &VirtualMachineInfo{}
	fakeMachineStore.MachineMap["vm2"] = &VirtualMachineInfo{}

	testTable := map[string]struct {
		machineName string
		expectedNil bool
		expectedErr error
	}{
		"found machine": {
			machineName: "vm1",
			expectedNil: false,
			expectedErr: nil,
		},
		"didnt find machine": {
			machineName: "vm3",
			expectedNil: true,
			expectedErr: ErrMachineNotFound,
		},
	}

	for name, tc := range testTable {
		t.Run(name, func(t *testing.T) {
			machine, err := fakeMachineStore.getVmInfoByVmId(tc.machineName)

			if tc.expectedNil {
				assert.Nil(t, machine)
			} else {
				assert.NotNil(t, machine)
			}

			if tc.expectedErr != nil {
				assert.Equal(t, tc.expectedErr, err)
			}
		})
	}
}

func TestGetMachineScaleSetPriority(t *testing.T) {
	testTable := map[string]struct {
		vmssObject       *armcompute.VirtualMachineScaleSet
		expectedPriority string
	}{
		"spot": {
			vmssObject: &armcompute.VirtualMachineScaleSet{
				Properties: &armcompute.VirtualMachineScaleSetProperties{
					VirtualMachineProfile: &armcompute.VirtualMachineScaleSetVMProfile{
						Priority: (*armcompute.VirtualMachinePriorityTypes)(to.StringPtr("Spot")),
					},
				},
			},
			expectedPriority: "spot",
		},
		"on demand": {
			vmssObject: &armcompute.VirtualMachineScaleSet{
				Properties: &armcompute.VirtualMachineScaleSetProperties{
					VirtualMachineProfile: &armcompute.VirtualMachineScaleSetVMProfile{
						Priority: (*armcompute.VirtualMachinePriorityTypes)(to.StringPtr("Regular")),
					},
				},
			},
			expectedPriority: "ondemand",
		},
	}

	for name, tc := range testTable {
		t.Run(name, func(t *testing.T) {
			priority := getMachineScaleSetPriority(tc.vmssObject)
			assert.Equal(t, tc.expectedPriority, priority.String())
		})
	}
}

func TestGetMachineScaleSetOperatingSystem(t *testing.T) {
	testTable := map[string]struct {
		vmssObject *armcompute.VirtualMachineScaleSet
		expectedOs MachineOperatingSystem
	}{
		"base case": {
			vmssObject: &armcompute.VirtualMachineScaleSet{
				Properties: &armcompute.VirtualMachineScaleSetProperties{
					VirtualMachineProfile: &armcompute.VirtualMachineScaleSetVMProfile{
						OSProfile: &armcompute.VirtualMachineScaleSetOSProfile{
							LinuxConfiguration: &armcompute.LinuxConfiguration{
								ProvisionVMAgent: to.BoolPtr(true),
							},
						},
					},
				},
			},
			expectedOs: Linux,
		},
		"windows case": {
			vmssObject: &armcompute.VirtualMachineScaleSet{
				Properties: &armcompute.VirtualMachineScaleSetProperties{
					VirtualMachineProfile: &armcompute.VirtualMachineScaleSetVMProfile{
						OSProfile: &armcompute.VirtualMachineScaleSetOSProfile{},
					},
				},
			},
			expectedOs: Windows,
		},
	}

	for name, tc := range testTable {
		t.Run(name, func(t *testing.T) {
			os := getMachineScaleSetOperatingSystem(tc.vmssObject)
			assert.Equal(t, tc.expectedOs, os)
		})
	}
}

func TestGetMachineName(t *testing.T) {
	fakeMachineStore := &MachineStore{
		logger: slog.Default(),
	}
	testTable := map[string]struct {
		vmObject     *armcompute.VirtualMachineScaleSetVM
		expectedName string
		expectedErr  bool
	}{
		"name exists": {
			vmObject: &armcompute.VirtualMachineScaleSetVM{
				Properties: &armcompute.VirtualMachineScaleSetVMProperties{
					InstanceView: &armcompute.VirtualMachineScaleSetVMInstanceView{
						ComputerName: to.StringPtr("aks-vmss-machine"),
					},
				},
			},
			expectedName: "aks-vmss-machine",
			expectedErr:  false,
		},
		"name is uppercase": {
			vmObject: &armcompute.VirtualMachineScaleSetVM{
				Properties: &armcompute.VirtualMachineScaleSetVMProperties{
					InstanceView: &armcompute.VirtualMachineScaleSetVMInstanceView{
						ComputerName: to.StringPtr("AKS-VMSS-machine"),
					},
				},
			},
			expectedName: "aks-vmss-machine",
			expectedErr:  false,
		},
		"instanceView not retrieved": {
			vmObject: &armcompute.VirtualMachineScaleSetVM{
				Properties: &armcompute.VirtualMachineScaleSetVMProperties{
					InstanceView: nil,
				},
			},
			expectedName: "",
			expectedErr:  true,
		},
		"instanceView partially retrieved": {
			vmObject: &armcompute.VirtualMachineScaleSetVM{
				Properties: &armcompute.VirtualMachineScaleSetVMProperties{
					InstanceView: &armcompute.VirtualMachineScaleSetVMInstanceView{},
				},
			},
			expectedName: "",
			expectedErr:  true,
		},
	}

	for name, tc := range testTable {
		t.Run(name, func(t *testing.T) {
			machineName, err := fakeMachineStore.getMachineName(tc.vmObject)

			if tc.expectedErr {
				assert.NotNil(t, err)
			}

			assert.Equal(t, tc.expectedName, machineName)
		})
	}
}

func TestGetMachineFamily(t *testing.T) {
	fakeMachineStore := &MachineStore{
		logger: slog.Default(),
	}
	testTable := map[string]struct {
		skuName        string
		expectedFamily string
		expectedErr    bool
	}{
		"General Purpose": {
			skuName:        "D5v2",
			expectedFamily: "General purpose",
			expectedErr:    false,
		},
		"General Purpose - standard": {
			skuName:        "Standard_D16_v3",
			expectedFamily: "General purpose",
			expectedErr:    false,
		},
		"Memory Optimized": {
			skuName:        "M416ms_v2",
			expectedFamily: "Memory optimized",
			expectedErr:    false,
		},
		"GPU Accelerated": {
			skuName:        "NC4as_T4_v3",
			expectedFamily: "GPU accelerated",
			expectedErr:    false,
		},
	}

	for name, tc := range testTable {
		t.Run(name, func(t *testing.T) {
			machineFamily, err := fakeMachineStore.getMachineFamilyFromSku(tc.skuName)
			if tc.expectedErr {
				assert.NotNil(t, err)
			}

			assert.Equal(t, tc.expectedFamily, machineFamily)
		})
	}
}
