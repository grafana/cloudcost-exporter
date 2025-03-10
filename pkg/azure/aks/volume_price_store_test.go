package aks

import (
	"testing"

	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

func Test_getPremiumSSDTier(t *testing.T) {
	tests := map[string]struct {
		diskSizeGB int
		want       string
	}{
		"4GB":     {diskSizeGB: 4, want: "P1"},
		"8GB":     {diskSizeGB: 8, want: "P2"},
		"16GB":    {diskSizeGB: 16, want: "P3"},
		"32GB":    {diskSizeGB: 32, want: "P4"},
		"64GB":    {diskSizeGB: 64, want: "P6"},
		"128GB":   {diskSizeGB: 128, want: "P10"},
		"256GB":   {diskSizeGB: 256, want: "P15"},
		"512GB":   {diskSizeGB: 512, want: "P20"},
		"1024GB":  {diskSizeGB: 1024, want: "P30"},
		"2048GB":  {diskSizeGB: 2048, want: "P40"},
		"4096GB":  {diskSizeGB: 4096, want: "P50"},
		"8192GB":  {diskSizeGB: 8192, want: "P60"},
		"16384GB": {diskSizeGB: 16384, want: "P70"},
		"32768GB": {diskSizeGB: 32768, want: "P80"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := getPremiumSSDTier(tt.diskSizeGB)
			if got != tt.want {
				t.Errorf("getPremiumSSDTier() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getStandardSSDTier(t *testing.T) {
	tests := map[string]struct {
		diskSizeGB int
		want       string
	}{
		"4GB":     {diskSizeGB: 4, want: "E1"},
		"8GB":     {diskSizeGB: 8, want: "E2"},
		"16GB":    {diskSizeGB: 16, want: "E3"},
		"32GB":    {diskSizeGB: 32, want: "E4"},
		"64GB":    {diskSizeGB: 64, want: "E6"},
		"128GB":   {diskSizeGB: 128, want: "E10"},
		"256GB":   {diskSizeGB: 256, want: "E15"},
		"512GB":   {diskSizeGB: 512, want: "E20"},
		"1024GB":  {diskSizeGB: 1024, want: "E30"},
		"2048GB":  {diskSizeGB: 2048, want: "E40"},
		"4096GB":  {diskSizeGB: 4096, want: "E50"},
		"8192GB":  {diskSizeGB: 8192, want: "E60"},
		"16384GB": {diskSizeGB: 16384, want: "E70"},
		"32768GB": {diskSizeGB: 32768, want: "E80"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := getStandardSSDTier(tt.diskSizeGB)
			if got != tt.want {
				t.Errorf("getStandardSSDTier() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getStandardHDDTier(t *testing.T) {
	tests := map[string]struct {
		diskSizeGB int
		want       string
	}{
		"4GB":     {diskSizeGB: 4, want: "S1"},
		"8GB":     {diskSizeGB: 8, want: "S2"},
		"16GB":    {diskSizeGB: 16, want: "S3"},
		"32GB":    {diskSizeGB: 32, want: "S4"},
		"64GB":    {diskSizeGB: 64, want: "S6"},
		"128GB":   {diskSizeGB: 128, want: "S10"},
		"256GB":   {diskSizeGB: 256, want: "S15"},
		"512GB":   {diskSizeGB: 512, want: "S20"},
		"1024GB":  {diskSizeGB: 1024, want: "S30"},
		"2048GB":  {diskSizeGB: 2048, want: "S40"},
		"4096GB":  {diskSizeGB: 4096, want: "S50"},
		"8192GB":  {diskSizeGB: 8192, want: "S60"},
		"16384GB": {diskSizeGB: 16384, want: "S70"},
		"32768GB": {diskSizeGB: 32768, want: "S80"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := getStandardHDDTier(tt.diskSizeGB)
			if got != tt.want {
				t.Errorf("getStandardHDDTier() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getDiskSkuPricingName(t *testing.T) {
	tests := map[string]struct {
		computeSkuName string
		diskSizeGB     int
		want           string
	}{
		"Premium_LRS":     {computeSkuName: "Premium_LRS", diskSizeGB: 1024, want: "P30"},
		"StandardSSD_LRS": {computeSkuName: "StandardSSD_LRS", diskSizeGB: 1024, want: "E30"},
		"Standard_LRS":    {computeSkuName: "Standard_LRS", diskSizeGB: 1024, want: "S30"},
		"Premium_ZRS":     {computeSkuName: "Premium_ZRS", diskSizeGB: 1024, want: "P30"},
		"StandardSSD_ZRS": {computeSkuName: "StandardSSD_ZRS", diskSizeGB: 1024, want: "E30"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := getDiskSkuPricingName(tt.computeSkuName, tt.diskSizeGB)
			if got != tt.want {
				t.Errorf("getDiskSkuPricingName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVolumePriceStore_validateVolumePriceIsRelevantFromSku(t *testing.T) {
	tests := map[string]struct {
		sku  *retailPriceSdk.ResourceSKU
		want bool
	}{
		"nil case": {
			sku:  nil,
			want: false,
		},
		"Name without Disk Should be false": {
			sku: &retailPriceSdk.ResourceSKU{
				ProductName: "Testing",
			},
			want: false,
		},
		"Name with disk should return true": {
			sku: &retailPriceSdk.ResourceSKU{
				ProductName: "Standard SSD Disk",
			},
			want: true,
		},
		"Name with SSD should return true": {
			sku: &retailPriceSdk.ResourceSKU{
				ProductName: "Azure Persistent SSD V2",
			},
			want: true,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := validateVolumePriceIsRelevantFromSku(test.sku); got != test.want {
				t.Errorf("VolumePriceStore.validateVolumePriceIsRelevantFromSku(%s) = %v, want %v", test.sku.ProductName, got, test.want)
			}
		})
	}
}

func TestDiskToPricingMap(t *testing.T) {
	tests := map[string]struct {
		diskname string
		want     string
	}{
		"Standard Disk": {
			diskname: "Standard_LRS",
			want:     "Standard HDD Managed Disk",
		},
		"Standard SSD Disk LSR": {
			diskname: "StandardSSD_LRS",
			want:     "Standard SSD Managed Disk",
		},
		"Standard SSD Disk ZSR": {
			diskname: "StandardSSD_ZRS",
			want:     "Standard SSD Managed Disk",
		},
		"Premium SSD Disk LSR": {
			diskname: "PremiumSSD_LRS",
			want:     "Premium SSD Managed Disk",
		},
		"Premium SSD Disk ZSR": {
			diskname: "PremiumSSD_ZRS",
			want:     "Premium SSD Managed Disk",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := standardizeSkuNameFromDisk(test.diskname); got != test.want {
				t.Errorf("standardSkuNameFromDisk() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestZoneTypeFromSkuName(t *testing.T) {
	tests := map[string]struct {
		diskname string
		want     string
	}{
		"Bad disk": {
			diskname: "This_is_a_bad_disk",
			want:     "",
		},
		"Standard Disk": {
			diskname: "Standard_LRS",
			want:     "LRS",
		},
		"Standard SSD Disk LSR": {
			diskname: "StandardSSD_LRS",
			want:     "LRS",
		},
		"Standard SSD Disk ZSR": {
			diskname: "StandardSSD_ZRS",
			want:     "ZRS",
		},
		"Premium SSD Disk LSR": {
			diskname: "PremiumSSD_LRS",
			want:     "LRS",
		},
		"Premium SSD Disk ZSR": {
			diskname: "PremiumSSD_ZRS",
			want:     "ZRS",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if got := getZoneTypeFromSkuName(test.diskname); got != test.want {
				t.Errorf("standardSkuNameFromDisk() = %v, want %v", got, test.want)
			}
		})
	}
}
