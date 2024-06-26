package aks

import (
	"testing"

	"github.com/Azure/go-autorest/autorest/to"
	"github.com/stretchr/testify/assert"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

func TestMapCreation(t *testing.T) {
	// TODO - mock
	t.Skip()
}

func TestBuildQueryFilter(t *testing.T) {
	p := PriceStore{}
	testTable := map[string]struct {
		locationList   []string
		expectedFilter string
	}{
		"no location list": {
			locationList:   nil,
			expectedFilter: `serviceName eq 'Virtual Machines' and priceType eq 'Consumption'`,
		},
		"location list with one item": {
			locationList:   []string{"eastus"},
			expectedFilter: `serviceName eq 'Virtual Machines' and priceType eq 'Consumption' and (armRegionName eq 'eastus')`,
		},
		"location list with many items": {
			locationList:   []string{"eastus", "asiapacific", "Global"},
			expectedFilter: `serviceName eq 'Virtual Machines' and priceType eq 'Consumption' and (armRegionName eq 'eastus' or armRegionName eq 'asiapacific' or armRegionName eq 'Global')`,
		},
	}

	for name, test := range testTable {
		t.Run(name, func(t *testing.T) {
			queryFilter := p.buildQueryFilter(test.locationList)
			assert.Equal(t, test.expectedFilter, queryFilter)
		})
	}
}

func TestBuildListOptions(t *testing.T) {
	p := PriceStore{}
	testTable := map[string]struct {
		locationList []string
		expectedOpts *retailPriceSdk.RetailPricesClientListOptions
	}{
		"no location list": {
			locationList: nil,
			expectedOpts: &retailPriceSdk.RetailPricesClientListOptions{
				APIVersion:  to.StringPtr("2023-01-01-preview"),
				Filter:      to.StringPtr(`serviceName eq 'Virtual Machines' and priceType eq 'Consumption'`),
				MeterRegion: to.StringPtr(`'primary'`),
			},
		},
		"location list with one item": {
			locationList: []string{"eastus"},
			expectedOpts: &retailPriceSdk.RetailPricesClientListOptions{
				APIVersion:  to.StringPtr("2023-01-01-preview"),
				Filter:      to.StringPtr(`serviceName eq 'Virtual Machines' and priceType eq 'Consumption' and (armRegionName eq 'eastus')`),
				MeterRegion: to.StringPtr(`'primary'`),
			},
		},
		"location list with many items": {
			locationList: []string{"eastus", "asiapacific", "Global"},
			expectedOpts: &retailPriceSdk.RetailPricesClientListOptions{
				APIVersion:  to.StringPtr("2023-01-01-preview"),
				Filter:      to.StringPtr(`serviceName eq 'Virtual Machines' and priceType eq 'Consumption' and (armRegionName eq 'eastus' or armRegionName eq 'asiapacific' or armRegionName eq 'Global')`),
				MeterRegion: to.StringPtr(`'primary'`),
			},
		},
	}

	for name, test := range testTable {
		t.Run(name, func(t *testing.T) {
			opts := p.buildListOptions(test.locationList)
			assert.Equal(t, test.expectedOpts, opts)
		})
	}
}

func TestDetermineMachineOperatingSystem(t *testing.T) {
	p := PriceStore{}
	testTable := map[string]struct {
		sku             retailPriceSdk.ResourceSKU
		expectedMachine MachineOperatingSystem
	}{
		"Linux": {
			sku: retailPriceSdk.ResourceSKU{
				ProductName: "Standard D4",
			},
			expectedMachine: Linux,
		},
		"Windows": {
			sku: retailPriceSdk.ResourceSKU{
				ProductName: "Standard D4 Windows",
			},
			expectedMachine: Windows,
		},
	}

	for name, test := range testTable {
		t.Run(name, func(t *testing.T) {
			machineOs := p.determineMachineOperatingSystem(test.sku)
			assert.Equal(t, test.expectedMachine, machineOs)
		})
	}
}

func TestDetermineMachinePriority(t *testing.T) {
	p := PriceStore{}
	testTable := map[string]struct {
		sku              retailPriceSdk.ResourceSKU
		expectedPriority MachinePriority
	}{
		"OnDemand": {
			sku: retailPriceSdk.ResourceSKU{
				ProductName: "Standard D4",
			},
			expectedPriority: OnDemand,
		},
		"Spot": {
			sku: retailPriceSdk.ResourceSKU{
				ProductName: "Standard D4 Windows",
			},
			expectedPriority: Spot,
		},
	}

	for name, test := range testTable {
		t.Run(name, func(t *testing.T) {
			machinePriority := p.determineMachineOperatingSystem(test.sku)
			assert.Equal(t, test.expectedPriority, machinePriority)
		})
	}
}
