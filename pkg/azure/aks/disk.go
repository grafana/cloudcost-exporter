// Package aks provides Azure Kubernetes Service (AKS) cost collection functionality.
// This file implements Azure Managed Disk support for persistent volume cost tracking.
package aks

import (
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
)

const (
	// Kubernetes annotations and tags used to identify persistent volumes
	pvcNamespaceAnnotation = "volume.beta.kubernetes.io/storage-provisioner"
	pvNameAnnotation       = "pv.kubernetes.io/provisioned-by"
	clusterNameTag         = "kubernetes.io-cluster-name"
	pvNameTag              = "kubernetes.io-created-for-pv-name"
)

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

	if clusterName, ok := d.Tags[clusterNameTag]; ok && clusterName != nil {
		d.ClusterName = *clusterName
	}

	if pvName, ok := d.Tags[pvNameTag]; ok && pvName != nil {
		d.PersistentVolumeName = *pvName
	}

	if namespace, ok := d.Tags["kubernetes.io-created-for-pvc-namespace"]; ok && namespace != nil {
		d.Namespace = *namespace
	}
}

// IsKubernetesPV returns true if this disk is a Kubernetes persistent volume.
// Determined by presence of cluster name or PV name tags.
func (d *Disk) IsKubernetesPV() bool {
	return d.PersistentVolumeName != "" || d.ClusterName != ""
}

// DiskType returns a string classification of the disk type for metrics.
func (d *Disk) DiskType() string {
	if d.IsKubernetesPV() {
		return "persistent_volume"
	}
	return "unattached_disk"
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
