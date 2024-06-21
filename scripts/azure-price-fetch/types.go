package main

import "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"

type VirtualMachineInfo struct {
	Name        string
	OwningVMSS  string
	MachineType string
}

type VmMap struct {
	RegionMap map[string]map[string]VirtualMachineInfo
}

type PriceMap struct {
	RegionMap map[string]map[string]sdk.ResourceSKU
}
