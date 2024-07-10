package aks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v5"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"golang.org/x/sync/errgroup"

	"github.com/Azure/go-autorest/autorest/to"
)

const (
	ConcurrentGoroutineLimit = 10
)

var (
	ErrMachineNotFound       = errors.New("machine not found in map")
	ErrMachineFamilyNotFound = errors.New("machine family not able to be determined by SKU")
	ErrMachineTierNotFound   = errors.New("machine tier not found in VMSS object")

	// As annoying as this is, I am unable to find an API call for this
	// and performance of a map lookup will be quite faster
	// than maintaining lists of each family
	//
	// Based on this logic https://learn.microsoft.com/en-us/azure/virtual-machines/vm-naming-conventions
	MachineFamilyTypeMap map[byte]string = map[byte]string{
		'A': "General purpose",
		'B': "General purpose",
		'D': "General purpose",
		'F': "Compute optimized",
		'E': "Memory optimized",
		'M': "Memory optimized",
		'L': "Storage optimized",
		'N': "GPU accelerated",
		'H': "High performance compute",
	}
)

type VirtualMachineInfo struct {
	Name            string
	Id              string
	Region          string
	OwningVMSS      string
	OwningCluster   string
	MachineTypeSku  string
	MachineFamily   string
	OperatingSystem MachineOperatingSystem
	Priority        MachinePriority

	NumOfCores     float64
	MemoryInMiB    float64 // Note, the Azure Docs say MiB, the golang docs say MB, we're going with the Azure Docs :nervous:
	OsDiskSizeInMB float64
}

type MachineStore struct {
	context context.Context
	logger  *slog.Logger

	azResourceGroupClient *armresources.ResourceGroupsClient
	azVMSizesClient       *armcompute.VirtualMachineSizesClient
	azVMSSClient          *armcompute.VirtualMachineScaleSetsClient
	azVMSSVmClient        *armcompute.VirtualMachineScaleSetVMsClient
	azAksClient           *armcontainerservice.ManagedClustersClient

	MachineSizeMap     map[string]map[string]*armcompute.VirtualMachineSize
	machineSizeMapLock *sync.RWMutex

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
		azVMSizesClient:       computeClientFactory.NewVirtualMachineSizesClient(),
		azVMSSClient:          computeClientFactory.NewVirtualMachineScaleSetsClient(),
		azVMSSVmClient:        computeClientFactory.NewVirtualMachineScaleSetVMsClient(),
		azAksClient:           containerClientFactory.NewManagedClustersClient(),

		MachineSizeMap:     make(map[string]map[string]*armcompute.VirtualMachineSize),
		machineSizeMapLock: &sync.RWMutex{},

		MachineMap:     make(map[string]*VirtualMachineInfo),
		machineMapLock: &sync.RWMutex{},
	}

	go func() {
		// populate the store before it is used
		err := ms.PopulateMachineStore(ms.context)
		if err != nil {
			// if it fails, subsequent calls to Collect() will populate the store
			ms.logger.LogAttrs(ms.context, slog.LevelError, "error populating initial machine store", slog.String("error", err.Error()))
		}
	}()

	return ms, nil
}

func (m *MachineStore) getVmInfoByVmId(vmId string) (*VirtualMachineInfo, error) {
	m.machineMapLock.RLock()
	defer m.machineMapLock.RUnlock()

	if _, ok := m.MachineMap[vmId]; !ok {
		return nil, ErrMachineNotFound
	}
	return m.MachineMap[vmId], nil
}

func (m *MachineStore) getVmInfoFromVmss(ctx context.Context, rgName, vmssName, cluster string, priority MachinePriority, osInfo MachineOperatingSystem) (map[string]*VirtualMachineInfo, error) {
	vmInfo := make(map[string]*VirtualMachineInfo)

	m.machineSizeMapLock.RLock()
	defer m.machineSizeMapLock.RUnlock()

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
			vmName, err := m.getMachineName(v)
			if err != nil {
				continue
			}

			if v.SKU == nil || v.SKU.Name == nil {
				m.logger.LogAttrs(ctx, slog.LevelDebug, "no VM Sku found", slog.String("machineName", vmName))
				continue
			}
			vmSku := to.String(v.SKU.Name)

			vmFamily, err := m.getMachineFamilyFromSku(vmSku)
			if err != nil {
				continue
			}

			vmRegion := to.String(v.Location)
			if len(vmRegion) == 0 {
				m.logger.LogAttrs(ctx, slog.LevelDebug, "no VM region found", slog.String("machineName", vmName))
				continue
			}

			if v.Properties == nil || v.Properties.VMID == nil {
				m.logger.LogAttrs(ctx, slog.LevelDebug, "no VM ID found", slog.String("machineName", vmName))
				continue
			}
			vmId := to.String(v.Properties.VMID)

			vmSizeInfo, ok := m.MachineSizeMap[vmRegion][vmSku]
			if !ok {
				m.logger.LogAttrs(ctx, slog.LevelDebug, "no VM sizing info found", slog.String("machineName", vmName))
				continue
			}

			m.logger.LogAttrs(ctx, slog.LevelDebug, "found machine information", slog.String("machineName", vmName))
			vmInfo[vmId] = &VirtualMachineInfo{
				Name:            vmName,
				Id:              vmId,
				Region:          vmRegion,
				OwningVMSS:      vmssName,
				OwningCluster:   cluster,
				MachineTypeSku:  vmSku,
				MachineFamily:   vmFamily,
				Priority:        priority,
				OperatingSystem: osInfo,

				NumOfCores:     float64(to.Int32(vmSizeInfo.NumberOfCores)),
				MemoryInMiB:    float64(to.Int32(vmSizeInfo.MemoryInMB)),
				OsDiskSizeInMB: float64(to.Int32(vmSizeInfo.OSDiskSizeInMB)),
			}
		}
	}

	return vmInfo, nil
}

func (m *MachineStore) getVMSSInfoFromResourceGroup(ctx context.Context, rgName, clusterName string) (map[string]*armcompute.VirtualMachineScaleSet, error) {
	m.logger.LogAttrs(ctx, slog.LevelInfo, "getting VMSS info from resource group of cluster", slog.String("resourceGroup", rgName), slog.String("cluster", clusterName))
	vmssInfo := make(map[string]*armcompute.VirtualMachineScaleSet)

	pager := m.azVMSSClient.NewListPager(rgName, nil)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			m.logger.LogAttrs(ctx, slog.LevelError, "unable to advance page in VMSS Client", slog.String("err", err.Error()))
			return nil, ErrPageAdvanceFailure
		}

		for _, v := range nextResult.Value {
			vmssName := to.String(v.Name)
			if len(vmssName) == 0 {
				m.logger.Error(fmt.Sprintf("unable to determine VMSS name: %+v", v))
				continue
			}

			vmssInfo[vmssName] = v
		}
	}

	return vmssInfo, nil
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

func (m *MachineStore) getMachineTypesByLocation(ctx context.Context, location string) error {
	m.machineSizeMapLock.Lock()
	defer m.machineSizeMapLock.Unlock()

	m.MachineSizeMap[location] = make(map[string]*armcompute.VirtualMachineSize)

	pager := m.azVMSizesClient.NewListPager(location, nil)
	for pager.More() {
		nextResult, err := pager.NextPage(ctx)
		if err != nil {
			m.logger.LogAttrs(ctx, slog.LevelError, "unable to advance page in VM Sizes Client", slog.String("err", err.Error()))
			return ErrPageAdvanceFailure
		}

		for _, v := range nextResult.Value {
			sizeId := to.String(v.Name)

			m.MachineSizeMap[location][sizeId] = v
		}
	}
	return nil
}

func (m *MachineStore) CheckReadiness() bool {
	return m.machineSizeMapLock.TryRLock() && m.machineMapLock.TryRLock()
}

func (m *MachineStore) PopulateMachineStore(ctx context.Context) error {
	startTime := time.Now()
	m.logger.Info("populating machine store")

	clusterList, err := m.getClustersInSubscription(ctx)
	if err != nil {
		return err
	}

	locationSet := make(map[string]bool)
	for _, c := range clusterList {
		locationSet[to.String(c.Location)] = true
	}

	m.machineMapLock.Lock()
	defer m.machineMapLock.Unlock()

	clear(m.MachineMap)

	// Note that this needs to be immediately unlocked because it will be re-locked
	// and repopulated below
	m.machineSizeMapLock.Lock()
	clear(m.MachineSizeMap)
	m.machineSizeMapLock.Unlock()

	vmInfoMap := make(map[string]*VirtualMachineInfo)
	vmInfoLock := sync.Mutex{}

	eg, nestedCtx := errgroup.WithContext(ctx)
	eg.SetLimit(ConcurrentGoroutineLimit)

	// Populate Machine Types
	for location := range locationSet {
		eg.Go(func() error {
			return m.getMachineTypesByLocation(nestedCtx, location)
		})
	}

	// Populate Machines
	for _, cluster := range clusterList {
		clusterName := to.String(cluster.Name)
		rgName := to.String(cluster.Properties.NodeResourceGroup)

		if len(clusterName) == 0 {
			m.logger.Error(fmt.Sprintf("cluster name not found: %+v", cluster))
			continue
		}

		if len(rgName) == 0 {
			m.logger.Error(fmt.Sprintf("resource group name not found: %+v", cluster))
			continue
		}

		eg.Go(func() error {
			vmssMap, err := m.getVMSSInfoFromResourceGroup(nestedCtx, rgName, clusterName)
			if err != nil {
				return err
			}

			for vmssName, vmssInfo := range vmssMap {
				eg.Go(func() error {
					vmssPriority := getMachineScaleSetPriority(vmssInfo)
					vmssOperatingSystem := getMachineScaleSetOperatingSystem(vmssInfo)
					if err != nil {
						return err
					}

					vmssVmInfo, err := m.getVmInfoFromVmss(nestedCtx, rgName, vmssName, clusterName, vmssPriority, vmssOperatingSystem)
					if err != nil {
						return err
					}

					vmInfoLock.Lock()
					for vmName, vmInfo := range vmssVmInfo {
						vmInfoMap[vmName] = vmInfo
					}
					vmInfoLock.Unlock()
					return nil
				})
			}
			return nil
		})
	}

	err = eg.Wait()
	if err != nil {
		return err
	}

	m.MachineMap = vmInfoMap
	m.logger.LogAttrs(m.context, slog.LevelInfo, "machine store populated", slog.Duration("duration", time.Since(startTime)))
	return nil
}

func (m *MachineStore) getMachineName(vm *armcompute.VirtualMachineScaleSetVM) (string, error) {
	if vm.Properties.InstanceView == nil {
		m.logger.Error(fmt.Sprintf("unable to determine machine name, instanceView property not set: %+v", vm))
		return "", fmt.Errorf("unable to determine machine name, instanceView property not set: %+v", vm)
	}

	computerName := to.String(vm.Properties.InstanceView.ComputerName)
	if len(computerName) == 0 {
		m.logger.Error(fmt.Sprintf("unable to determine machine name: %+v", vm))
		return "", fmt.Errorf("unable to determine machine name: %+v", vm)
	}

	return computerName, nil
}

// Based on this logic https://learn.microsoft.com/en-us/azure/virtual-machines/vm-naming-conventions
func (m *MachineStore) getMachineFamilyFromSku(sku string) (string, error) {
	sku = strings.TrimPrefix(sku, "Standard_")
	skuStartsWith := sku[0]

	family, ok := MachineFamilyTypeMap[skuStartsWith]
	if !ok {
		m.logger.Error(ErrMachineFamilyNotFound.Error())
		return "", ErrMachineFamilyNotFound
	}

	return family, nil
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
