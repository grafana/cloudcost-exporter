package aks

import (
	"sync"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/stretchr/testify/assert"
)

func TestMachineStoreMapCreation(t *testing.T) {
	// TODO - mock
	t.Skip()
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
			machine, err := fakeMachineStore.getVmInfoByVmName(tc.machineName)

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
		expectedPriority MachinePriority
	}{
		"spot": {
			vmssObject: &armcompute.VirtualMachineScaleSet{
				Properties: &armcompute.VirtualMachineScaleSetProperties{
					VirtualMachineProfile: &armcompute.VirtualMachineScaleSetVMProfile{
						Priority: (*armcompute.VirtualMachinePriorityTypes)(to.StringPtr("Spot")),
					},
				},
			},
			expectedPriority: Spot,
		},
		"on demand": {
			vmssObject: &armcompute.VirtualMachineScaleSet{
				Properties: &armcompute.VirtualMachineScaleSetProperties{
					VirtualMachineProfile: &armcompute.VirtualMachineScaleSetVMProfile{
						Priority: (*armcompute.VirtualMachinePriorityTypes)(to.StringPtr("Regular")),
					},
				},
			},
			expectedPriority: OnDemand,
		},
	}

	for name, tc := range testTable {
		t.Run(name, func(t *testing.T) {
			priority := getMachineScaleSetPriority(tc.vmssObject)
			assert.Equal(t, tc.expectedPriority, priority)
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
			machineName, err := getMachineName(tc.vmObject)

			if tc.expectedErr {
				assert.NotNil(t, err)
			}

			assert.Equal(t, tc.expectedName, machineName)
		})
	}
}
