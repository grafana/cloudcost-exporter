// Package aks provides Azure Kubernetes Service (AKS) cost collection functionality.
// This file implements Azure Managed Disk support for persistent volume cost tracking.
package aks

import (
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
)

const aksPVTagName = "kubernetes.io-created-for-pv-name"
const aksPVCNamespaceTagName = "kubernetes.io-created-for-pvc-namespace"

// Disk represents an Azure Managed Disk with Kubernetes metadata extracted from tags.
// Used for cost tracking of persistent volumes in AKS clusters.
type Disk struct {
	Name                 string             // Azure disk name
	ResourceGroup        string             // Azure resource group containing the disk
	Location             string             // Azure region (e.g., "centralus", "westeurope")
	Size                 int32              // Disk size in GB
	SKU                  string             // Azure disk SKU (e.g., "Premium_LRS", "StandardSSD_LRS")
	State                string             // Disk state (e.g., "Attached", "Unattached")
	Zone                 string             // Availability zone
	PersistentVolumeName string             // Kubernetes PV name extracted from tags
	Namespace            string             // Kubernetes namespace extracted from tags
	ClusterName          string             // AKS cluster name extracted from tags
	Tags                 map[string]*string // Azure resource tags
}

// NewDisk creates a Disk instance from an Azure ARM compute Disk resource.
// Extracts Kubernetes metadata from Azure tags to identify persistent volumes.
func NewDisk(disk *armcompute.Disk) *Disk {
	if disk == nil {
		return nil
	}

	d := &Disk{
		Name:          getStringValue(disk.Name),
		ResourceGroup: extractResourceGroupFromID(getStringValue(disk.ID)),
		Location:      getStringValue(disk.Location),
		Tags:          disk.Tags,
	}

	if disk.Properties != nil {
		if disk.Properties.DiskSizeGB != nil {
			d.Size = *disk.Properties.DiskSizeGB
		}
		if disk.Properties.DiskState != nil {
			d.State = string(*disk.Properties.DiskState)
		}
	}

	if disk.SKU != nil && disk.SKU.Name != nil {
		d.SKU = string(*disk.SKU.Name)
	}

	if len(disk.Zones) > 0 && disk.Zones[0] != nil {
		d.Zone = *disk.Zones[0]
	}

	d.extractKubernetesInfo()

	return d
}

// extractKubernetesInfo extracts Kubernetes metadata from Azure disk tags.
// Looks for cluster name, persistent volume name, and namespace tags.
func (d *Disk) extractKubernetesInfo() {
	if d.Tags == nil {
		return
	}

	// Extract PV name
	if pvName, ok := d.Tags[aksPVTagName]; ok && pvName != nil {
		d.PersistentVolumeName = *pvName
	}

	// Extract namespace
	if namespace, ok := d.Tags[aksPVCNamespaceTagName]; ok && namespace != nil {
		d.Namespace = *namespace
	}

	// Extract cluster name from kubernetes.io-cluster-<cluster-name> tags
	// Azure CSI driver creates tags like "kubernetes.io-cluster-<cluster-name>: owned"
	for tagName, tagValue := range d.Tags {
		if strings.HasPrefix(tagName, "kubernetes.io-cluster-") && tagValue != nil && *tagValue == "owned" {
			// Extract cluster name from tag like "kubernetes.io-cluster-my-cluster"
			clusterName := strings.TrimPrefix(tagName, "kubernetes.io-cluster-")
			if clusterName != "" {
				d.ClusterName = strings.ToLower(clusterName)
				break
			}
		}
	}

	// If this is a Kubernetes disk but we don't have cluster name, try inference methods
	if d.PersistentVolumeName != "" && d.ClusterName == "" {
		// Try common alternative tag patterns for cluster identification
		alternatePatterns := map[string]string{
			"cluster":                    "",
			"cluster-name":               "",
			"aks-cluster":                "",
			"aks-cluster-name":           "",
			"kubernetes-cluster":         "",
			"k8s-cluster":                "",
			"kubernetes.io/cluster":      "",
			"kubernetes.io/cluster-name": "",
		}

		for tagName, tagValue := range d.Tags {
			if tagValue != nil {
				for pattern := range alternatePatterns {
					if strings.Contains(strings.ToLower(tagName), pattern) {
						d.ClusterName = strings.ToLower(*tagValue)
						break
					}
				}
				if d.ClusterName != "" {
					break
				}
			}
		}

		// If still no cluster name, try to infer from resource group name
		// AKS typically creates resource groups like "MC_<resource-group>_<cluster-name>_<region>"
		if d.ClusterName == "" && strings.HasPrefix(d.ResourceGroup, "MC_") {
			parts := strings.Split(d.ResourceGroup, "_")
			if len(parts) >= 3 {
				d.ClusterName = strings.ToLower(parts[2]) // cluster name is typically the 3rd part, lowercase
			}
		}
	}
}

// GetSKUForPricing maps Azure disk SKUs to user-friendly names for pricing metrics.
// Maps technical SKUs like "Premium_LRS" to "Premium SSD" for consistent naming.
func (d *Disk) GetSKUForPricing() string {
	switch d.SKU {
	case "Standard_LRS":
		return "Standard HDD"
	case "StandardSSD_LRS":
		return "Standard SSD"
	case "Premium_LRS":
		return "Premium SSD"
	case "PremiumV2_LRS":
		return "Premium SSD v2"
	case "UltraSSD_LRS":
		return "Ultra Disk"
	default:
		return d.SKU
	}
}

// GetPriceTier returns the Azure pricing tier for this disk based on size and SKU.
// Maps disk size to Azure pricing tiers like "P15", "E10", "S4", etc.
// Reuses existing pricing functions from DiskStore to extract tier information.
func (d *Disk) GetPriceTier(ds *DiskStore) string {
	switch d.SKU {
	case "Standard_LRS":
		return extractTierFromSKU(ds.getStandardHDDSKU(d.Size))
	case "StandardSSD_LRS":
		return extractTierFromSKU(ds.getStandardSSDSKU(d.Size))
	case "Premium_LRS":
		return extractTierFromSKU(ds.getPremiumSSDSKU(d.Size))
	case "PremiumV2_LRS":
		return "PremiumV2"
	case "UltraSSD_LRS":
		return "Ultra"
	default:
		return "Unknown"
	}
}

// getStringValue safely dereferences a string pointer, returning empty string if nil.
func getStringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// extractResourceGroupFromID parses an Azure resource ID to extract the resource group name.
// Azure resource IDs follow the format: /subscriptions/{sub}/resourceGroups/{rg}/providers/{provider}/...
func extractResourceGroupFromID(id string) string {
	parts := strings.Split(id, "/")
	for i, part := range parts {
		if strings.EqualFold(part, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
