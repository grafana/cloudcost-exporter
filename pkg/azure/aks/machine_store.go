package aks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"

	"github.com/Azure/go-autorest/autorest/to"
)

var (
	ErrMachineNotFound = errors.New("machine not found in map")
)

type VirtualMachineInfo struct {
	Name            string
	Region          string
	OwningVMSS      string
	OwningCluster   string
	MachineTypeSku  string
	OperatingSystem MachineOperatingSystem
	Priority        MachinePriority
}

type MachineStore struct {
	context context.Context
	logger  *slog.Logger

	azResourceGroupClient *armresources.ResourceGroupsClient
	azVMSSClient          *armcompute.VirtualMachineScaleSetsClient
	azVMSSVmClient        *armcompute.VirtualMachineScaleSetVMsClient
	azAksClient           *armcontainerservice.ManagedClustersClient

	MachineMap     map[string]*VirtualMachineInfo
	machineMapLock *sync.RWMutex
}

func NewMachineStore(parentCtx context.Context, parentLogger *slog.Logger, subscriptionId string, credentials *azidentity.DefaultAzureCredential) (*MachineStore, error) {
	logger := parentLogger.With("subsystem", "machineStore")

	rgClient, err := armresources.NewResourceGroupsClient(subscriptionId, credentials, nil)
	if err != nil {
		logger.LogAttrs(parentCtx, slog.LevelError, "unable to create resource group client", slog.String("err", err.Error()))
		return nil, ErrClientCreationFailure
	}

	computeClientFactory, err := armcompute.NewClientFactory(subscriptionId, credentials, nil)
	if err != nil {
		logger.LogAttrs(parentCtx, slog.LevelError, "unable to create compute client factory", slog.String("err", err.Error()))
		return nil, ErrClientCreationFailure
	}

	containerClientFactory, err := armcontainerservice.NewClientFactory(subscriptionId, credentials, nil)
	if err != nil {
		logger.LogAttrs(parentCtx, slog.LevelError, "unable to create container client factory", slog.String("err", err.Error()))
		return nil, ErrClientCreationFailure
	}

	ms := &MachineStore{
		context: parentCtx,
		logger:  logger,

		azResourceGroupClient: rgClient,
		azVMSSClient:          computeClientFactory.NewVirtualMachineScaleSetsClient(),
		azVMSSVmClient:        computeClientFactory.NewVirtualMachineScaleSetVMsClient(),
		azAksClient:           containerClientFactory.NewManagedClustersClient(),

		MachineMap:     make(map[string]*VirtualMachineInfo),
		machineMapLock: &sync.RWMutex{},
	}

	go func() {
		err := ms.PopulateMachineStore(ms.context)
		if err != nil {
			ms.logger.LogAttrs(ms.context, slog.LevelError, "error populating initial machine store", slog.String("error", err.Error()))
		}
	}()

	return ms, nil
}

func (m *MachineStore) getVmInfoByVmName(vmName string) (*VirtualMachineInfo, error) {
	m.machineMapLock.RLock()
	defer m.machineMapLock.RUnlock()

	vmInfo := m.MachineMap[vmName]
	if vmInfo == nil {
		return nil, ErrMachineNotFound
	}

	return vmInfo, nil
}

func (m *MachineStore) getVmInfoFromVmss(ctx context.Context, rgName string, vmssName string, priority MachinePriority, osInfo MachineOperatingSystem) (map[string]*VirtualMachineInfo, error) {
	vmInfo := make(map[string]*VirtualMachineInfo)

	opts := &armcompute.VirtualMachineScaleSetVMsClientListOptions{
		Expand: to.StringPtr("instanceView"),
	}
	pager := m.azVMSSVmClient.NewListPager(rgName, vmssName, opts)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			m.logger.LogAttrs(ctx, slog.LevelError, "unable to advance page in VMSS VM Client", slog.String("err", err.Error()))
			return nil, ErrPageAdvanceFailure
		}

		for _, v := range nextResult.Value {
			vmName, err := getMachineName(v)
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
			m.logger.LogAttrs(ctx, slog.LevelDebug, "found machine information", slog.String("machineName", vmName))
		}
	}

	return vmInfo, nil
}

func (m *MachineStore) getVmInfoFromResourceGroup(ctx context.Context, rgName, clusterName string) (map[string]*VirtualMachineInfo, error) {
	vmInfoMap := make(map[string]*VirtualMachineInfo)

	pager := m.azVMSSClient.NewListPager(rgName, nil)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			m.logger.LogAttrs(ctx, slog.LevelError, "unable to advance page in VMSS Client", slog.String("err", err.Error()))
			return nil, ErrPageAdvanceFailure
		}

		for _, v := range nextResult.Value {
			vmssName := to.String(v.Name)
			vmssPriority := getMachineScaleSetPriority(v)
			vmssOperationSystem := getMachineScaleSetOperatingSystem(v)

			vmInfo, err := m.getVmInfoFromVmss(ctx, rgName, vmssName, vmssPriority, vmssOperationSystem)
			if err != nil {
				return nil, err
			}

			for name, info := range vmInfo {
				info.OwningCluster = clusterName
				vmInfoMap[name] = info
			}
		}
	}

	return vmInfoMap, nil
}

func (m *MachineStore) getClustersInSubscription(ctx context.Context) ([]*armcontainerservice.ManagedCluster, error) {
	clusterList := []*armcontainerservice.ManagedCluster{}

	pager := m.azAksClient.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			m.logger.LogAttrs(ctx, slog.LevelError, "unable to advance page in AKS Client", slog.String("err", err.Error()))
			return nil, ErrPageAdvanceFailure
		}
		clusterList = append(clusterList, page.Value...)
	}

	return clusterList, nil
}

func (m *MachineStore) PopulateMachineStore(ctx context.Context) error {
	startTime := time.Now()
	m.logger.Info("populating machine store")

	m.machineMapLock.Lock()
	defer m.machineMapLock.Unlock()

	clusterList, err := m.getClustersInSubscription(ctx)
	if err != nil {
		return err
	}

	clear(m.MachineMap)

	for _, cluster := range clusterList {
		clusterName := to.String(cluster.Name)
		rgName := to.String(cluster.Properties.NodeResourceGroup)

		if len(clusterName) == 0 {
			m.logger.Error("cluster name not found")
			continue
		}

		if len(rgName) == 0 {
			m.logger.Error("resource group name not found")
			continue
		}

		vmInfo, err := m.getVmInfoFromResourceGroup(ctx, rgName, clusterName)
		if err != nil {
			return err
		}

		for name, info := range vmInfo {
			m.MachineMap[name] = info
		}

	}

	m.logger.LogAttrs(m.context, slog.LevelInfo, "machine store populated", slog.Duration("duration", time.Since(startTime)))
	return nil
}

func getMachineScaleSetPriority(vmss *armcompute.VirtualMachineScaleSet) MachinePriority {
	if vmss.Properties.VirtualMachineProfile.Priority != nil && *vmss.Properties.VirtualMachineProfile.Priority == armcompute.VirtualMachinePriorityTypesSpot {
		return Spot
	}
	return OnDemand
}

func getMachineScaleSetOperatingSystem(vmss *armcompute.VirtualMachineScaleSet) MachineOperatingSystem {
	if vmss.Properties.VirtualMachineProfile.OSProfile.LinuxConfiguration != nil {
		return Linux
	}
	return Windows
}

func getMachineName(vm *armcompute.VirtualMachineScaleSetVM) (string, error) {
	if vm.Properties.InstanceView == nil {
		return "", fmt.Errorf("unable to determine machine name, instanceView property not set: %v", vm)
	}

	computerName := to.String(vm.Properties.InstanceView.ComputerName)
	if len(computerName) == 0 {
		return "", fmt.Errorf("unable to determine machine name for: %v", vm)
	}

	return computerName, nil
}
