package aks

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v8"
	"github.com/grafana/cloudcost-exporter/pkg/azure/client"
	"golang.org/x/sync/errgroup"

	"github.com/Azure/go-autorest/autorest/to"
)

const (
	ConcurrentGoroutineLimit = 10

	machineRefreshInterval = 5 * time.Minute
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

	azClientWrapper client.AzureClient

	MachineSizeMap     map[string]map[string]*armcompute.VirtualMachineSize
	machineSizeMapLock *sync.RWMutex

	MachineMap     map[string]*VirtualMachineInfo
	machineMapLock *sync.RWMutex
}

func NewMachineStore(parentCtx context.Context, parentLogger *slog.Logger, azClientWrapper client.AzureClient) (*MachineStore, error) {
	logger := parentLogger.With("subsystem", "machineStore")

	ms := &MachineStore{
		context: parentCtx,
		logger:  logger,

		azClientWrapper: azClientWrapper,

		MachineSizeMap:     make(map[string]map[string]*armcompute.VirtualMachineSize),
		machineSizeMapLock: &sync.RWMutex{},

		MachineMap:     make(map[string]*VirtualMachineInfo),
		machineMapLock: &sync.RWMutex{},
	}

	// Populate before using
	go ms.PopulateMachineStore(parentCtx)

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

	vmsInVmssList, err := m.azClientWrapper.ListVirtualMachineScaleSetsOwnedVms(ctx, rgName, vmssName)
	if err != nil {
		m.logger.LogAttrs(ctx, slog.LevelError, "failed to get VMs from VMSS from client", slog.String("error", err.Error()))
		return nil, err
	}

	for _, v := range vmsInVmssList {
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
			m.logger.LogAttrs(ctx, slog.LevelDebug, "no VM ID found",
				slog.String("machineName", vmName),
				slog.String("region", vmRegion),
				slog.String("sku", vmSku),
			)
			continue
		}
		vmId := to.String(v.Properties.VMID)

		vmSizeInfo, ok := m.MachineSizeMap[vmRegion][vmSku]
		if !ok {
			m.logger.LogAttrs(ctx, slog.LevelDebug, "no VM sizing info found",
				slog.String("machineName", vmName),
				slog.String("region", vmRegion),
				slog.String("sku", vmSku),
			)
			continue
		}

		m.logger.LogAttrs(ctx, slog.LevelDebug, "found machine information",
			slog.String("machineName", vmName),
			slog.String("machineId", vmId),
			slog.String("vmssName", vmssName),
			slog.String("vmssClusterName", cluster),
		)
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

	m.logger.LogAttrs(ctx, slog.LevelDebug, "finished collecting machine info for VMSS",
		slog.String("vmssName", vmssName),
		slog.String("cluster", cluster),
		slog.String("resourceGroup", rgName),
		slog.Int("machinesFound", len(vmInfo)),
	)
	return vmInfo, nil
}

func (m *MachineStore) getVMSSInfoFromResourceGroup(ctx context.Context, rgName, clusterName string) (map[string]*armcompute.VirtualMachineScaleSet, error) {
	m.logger.LogAttrs(ctx, slog.LevelInfo, "getting VMSS info from resource group of cluster", slog.String("resourceGroup", rgName), slog.String("cluster", clusterName))

	vmssList, err := m.azClientWrapper.ListVirtualMachineScaleSetsFromResourceGroup(ctx, rgName)
	if err != nil {
		return nil, err
	}

	vmssInfo := make(map[string]*armcompute.VirtualMachineScaleSet)
	for _, vmss := range vmssList {
		vmssName := to.String(vmss.Name)
		if len(vmssName) == 0 {
			m.logger.Error(fmt.Sprintf("unable to determine VMSS name: %+v", vmss))
			continue
		}

		vmssInfo[vmssName] = vmss
	}

	m.logger.LogAttrs(m.context, slog.LevelDebug, "finished collecting VMSS",
		slog.Int("numOfVmss", len(vmssInfo)),
		slog.String("resourceGroup", rgName),
		slog.String("cluster", clusterName),
	)
	return vmssInfo, nil
}

func (m *MachineStore) getClustersInSubscription(ctx context.Context) ([]*armcontainerservice.ManagedCluster, error) {
	m.logger.Info("fetching cluster list from subscription")

	clusterList, err := m.azClientWrapper.ListClustersInSubscription(ctx)
	if err != nil {
		return nil, err
	}

	m.logger.LogAttrs(m.context, slog.LevelDebug, "found clusters", slog.Int("numOfClusters", len(clusterList)))
	return clusterList, nil
}

func (m *MachineStore) getMachineTypesByLocation(ctx context.Context, location string) error {
	m.logger.LogAttrs(ctx, slog.LevelInfo, "fetching machine types in location", slog.String("location", location))

	machineSizeList, err := m.azClientWrapper.ListMachineTypesByLocation(ctx, location)
	if err != nil {
		return err
	}

	m.machineSizeMapLock.Lock()
	defer m.machineSizeMapLock.Unlock()

	clear(m.MachineSizeMap[location])
	m.MachineSizeMap[location] = make(map[string]*armcompute.VirtualMachineSize)

	for _, v := range machineSizeList {
		sizeId := to.String(v.Name)

		m.MachineSizeMap[location][sizeId] = v
		m.logger.LogAttrs(m.context, slog.LevelDebug, "found machine size",
			slog.String("machineSizeMapRegion", location),
			slog.String("sizeId", sizeId),
		)
	}

	return nil
}

func (m *MachineStore) GetListOfVmsForSubscription() []*VirtualMachineInfo {
	m.machineMapLock.RLock()
	defer m.machineMapLock.RUnlock()

	vmi := make([]*VirtualMachineInfo, 0, len(m.MachineMap))
	for _, vmInfo := range m.MachineMap {
		vmi = append(vmi, vmInfo)
	}

	return vmi
}

func (m *MachineStore) PopulateMachineStore(ctx context.Context) {
	m.logger.Info("populating machine store")

	clusterList, err := m.getClustersInSubscription(ctx)
	if err != nil {
		return
	}

	locationSet := make(map[string]bool)
	for _, c := range clusterList {
		locationSet[to.String(c.Location)] = true
	}

	m.machineMapLock.Lock()
	defer m.machineMapLock.Unlock()
	clear(m.MachineMap)

	m.machineSizeMapLock.Lock()
	clear(m.MachineSizeMap)
	// Note that this needs to be immediately unlocked because it will be re-locked
	// and repopulated below
	m.machineSizeMapLock.Unlock()

	vmInfoMap := make(map[string]*VirtualMachineInfo)
	vmInfoLock := sync.Mutex{}

	machineSizesEg, nestedCtx := errgroup.WithContext(ctx)
	machineSizesEg.SetLimit(ConcurrentGoroutineLimit)

	// Populate Machine Types
	for location := range locationSet {
		machineSizesEg.Go(func() error {
			return m.getMachineTypesByLocation(nestedCtx, location)
		})
	}

	err = machineSizesEg.Wait()
	if err != nil {
		m.logger.LogAttrs(m.context, slog.LevelError, "Error populating machine sizes", slog.String("err", err.Error()))
		return
	}

	machineInstancesEg, nestedCtx := errgroup.WithContext(ctx)
	machineInstancesEg.SetLimit(ConcurrentGoroutineLimit)

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

		machineInstancesEg.Go(func() error {
			vmssMap, err := m.getVMSSInfoFromResourceGroup(nestedCtx, rgName, clusterName)
			if err != nil {
				return err
			}

			for vmssName, vmssInfo := range vmssMap {
				machineInstancesEg.Go(func() error {
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

	err = machineInstancesEg.Wait()
	if err != nil {
		m.logger.LogAttrs(m.context, slog.LevelError, "Error populating Machine Store", slog.String("err", err.Error()))
		return
	}

	m.MachineMap = vmInfoMap
	m.logger.LogAttrs(m.context, slog.LevelInfo, "machine store populated",
		slog.Int("numOfMachines", len(m.MachineMap)),
		slog.Int("numOfClusters", len(clusterList)),
	)
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

	return strings.ToLower(computerName), nil
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
