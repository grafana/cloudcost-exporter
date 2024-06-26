package aks

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/go-autorest/autorest/to"
)

type VirtualMachineInfo struct {
	Id              string
	Region          string
	OwningVMSS      string
	MachineTypeSku  string
	MachineTypeName string
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

	m.logger.LogAttrs(m.context, slog.LevelInfo, "machine store populated", slog.Duration("duration", time.Since(startTime)))
	return nil
}
