package aks

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/go-autorest/autorest/to"
)

type VirtualMachineInfo struct {
	Name            string
	Region          string
	OwningVMSS      string
	MachineTypeSku  string
	OperatingSystem MachineOperatingSystem
	Priority        MachinePriority

	// TODO - decide if this should be stored here or not
	// RetailPrice     float64
}

type MachineStore struct {
	context context.Context
	logger  *slog.Logger

	azResourceGroupClient *armresources.ResourceGroupsClient
	azVMSSClient          *armcompute.VirtualMachineScaleSetsClient
	azVMSSVmClient        *armcompute.VirtualMachineScaleSetVMsClient

	machineMapLock *sync.RWMutex

	RegionList []string
	MachineMap map[string]*VirtualMachineInfo
}

func NewMachineStore(parentCtx context.Context, parentLogger *slog.Logger, subscriptionId string, credentials *azidentity.DefaultAzureCredential) (*MachineStore, error) {
	logger := parentLogger.With("subsystem", "machineStore")

	rgClient, err := armresources.NewResourceGroupsClient(subscriptionId, credentials, nil)
	if err != nil {
		return nil, ErrClientCreationFailure
	}

	computeClientFactory, err := armcompute.NewClientFactory(subscriptionId, credentials, nil)
	if err != nil {
		return nil, ErrClientCreationFailure
	}

	ms := &MachineStore{
		context: parentCtx,
		logger:  logger,

		azResourceGroupClient: rgClient,
		azVMSSClient:          computeClientFactory.NewVirtualMachineScaleSetsClient(),
		azVMSSVmClient:        computeClientFactory.NewVirtualMachineScaleSetVMsClient(),

		machineMapLock: &sync.RWMutex{},

		RegionList: []string{},
		MachineMap: make(map[string]*VirtualMachineInfo),
	}

	go func() {
		err := ms.PopulateMachineStore()
		if err != nil {
			ms.logger.LogAttrs(ms.context, slog.LevelError, "error populating initial machine store", slog.String("error", err.Error()))
		}
	}()

	return ms, nil
}

func (m *MachineStore) getVmInfoFromVmss(rgName string, vmssName string, priority MachinePriority, osInfo MachineOperatingSystem) (map[string]*VirtualMachineInfo, error) {
	vmInfo := make(map[string]*VirtualMachineInfo)

	opts := &armcompute.VirtualMachineScaleSetVMsClientListOptions{
		Expand: to.StringPtr("instanceView"),
	}
	pager := m.azVMSSVmClient.NewListPager(rgName, vmssName, opts)
	for pager.More() {
		nextResult, err := pager.NextPage(m.context)
		if err != nil {
			return nil, ErrPageAdvanceFailure
		}

		for _, v := range nextResult.Value {
			vmName, err := determineMachineName(v)
			if err != nil {
				m.logger.Error(err.Error())
				continue
			}

			vmInfo[vmName] = &VirtualMachineInfo{
				Name:            vmName,
				Region:          to.String(v.Location),
				OwningVMSS:      vmssName,
				MachineTypeSku:  to.String(v.SKU.Name),
				Priority:        priority,
				OperatingSystem: osInfo,
			}
		}
	}

	return vmInfo, nil
}

func (m *MachineStore) getVmInfoFromResourceGroup(rgName string) (map[string]*VirtualMachineInfo, error) {
	vmInfoMap := make(map[string]*VirtualMachineInfo)

	pager := m.azVMSSClient.NewListPager(rgName, nil)
	for pager.More() {
		nextResult, err := pager.NextPage(m.context)
		if err != nil {
			return nil, ErrPageAdvanceFailure
		}

		for _, v := range nextResult.Value {
			vmssName := to.String(v.Name)
			vmssPriority := determineMachineScaleSetPriority(v)
			vmssOperationSystem := determineMachineScaleSetOperatingSystem(v)

			vmInfo, err := m.getVmInfoFromVmss(rgName, vmssName, vmssPriority, vmssOperationSystem)
			if err != nil {
				return nil, err
			}

			for name, info := range vmInfo {
				vmInfoMap[name] = info
			}
		}
	}

	return vmInfoMap, nil
}

func (m *MachineStore) setRegionListFromResourceGroupList(rgList []*armresources.ResourceGroup) {
	locationSet := make(map[string]bool)
	uniqueLocationList := []string{}

	for _, v := range rgList {
		locationSet[to.String(v.Location)] = true
	}

	for r := range locationSet {
		uniqueLocationList = append(uniqueLocationList, r)
	}

	m.RegionList = uniqueLocationList
}

func (m *MachineStore) getResourceGroupsForSubscription() ([]*armresources.ResourceGroup, error) {
	resourceGroups := []*armresources.ResourceGroup{}

	pager := m.azResourceGroupClient.NewListPager(nil)
	for pager.More() {
		nextResult, err := pager.NextPage(m.context)
		if err != nil {
			return nil, ErrPageAdvanceFailure
		}

		resourceGroups = append(resourceGroups, nextResult.Value...)
	}
	return resourceGroups, nil
}

func (m *MachineStore) PopulateMachineStore() error {
	startTime := time.Now()
	m.logger.Info("populating machine store")

	m.machineMapLock.Lock()
	defer m.machineMapLock.Unlock()

	resourceGroups, err := m.getResourceGroupsForSubscription()
	if err != nil {
		return err
	}

	go m.setRegionListFromResourceGroupList(resourceGroups)

	vmInfoStore := make(map[string]*VirtualMachineInfo)

	for _, rg := range resourceGroups {
		rgName := to.String(rg.Name)
		vmInfo, err := m.getVmInfoFromResourceGroup(rgName)
		if err != nil {
			return err
		}

		for name, info := range vmInfo {
			vmInfoStore[name] = info
		}

	}

	m.MachineMap = vmInfoStore

	m.logger.LogAttrs(m.context, slog.LevelInfo, "machine store populated", slog.Duration("duration", time.Since(startTime)))
	return nil
}

func determineMachineScaleSetPriority(vmss *armcompute.VirtualMachineScaleSet) MachinePriority {
	if vmss.Properties.VirtualMachineProfile.Priority != nil && *vmss.Properties.VirtualMachineProfile.Priority == armcompute.VirtualMachinePriorityTypesSpot {
		return Spot
	}
	return OnDemand
}

func determineMachineScaleSetOperatingSystem(vmss *armcompute.VirtualMachineScaleSet) MachineOperatingSystem {
	if vmss.Properties.VirtualMachineProfile.OSProfile.LinuxConfiguration != nil {
		return Linux
	}
	return Windows
}

func determineMachineName(vm *armcompute.VirtualMachineScaleSetVM) (string, error) {
	if vm.Properties.InstanceView == nil {
		return "", fmt.Errorf("unable to determine machine name, instanceView property not set: %v", vm)
	}

	return to.String(vm.Properties.InstanceView.ComputerName), nil
}
