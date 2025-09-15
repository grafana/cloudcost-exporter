package aks

import (
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
)

const (
	pvcNamespaceAnnotation = "volume.beta.kubernetes.io/storage-provisioner"
	pvNameAnnotation       = "pv.kubernetes.io/provisioned-by"
	clusterNameTag         = "kubernetes.io-cluster-name"
	pvNameTag              = "kubernetes.io-created-for-pv-name"
)

type Disk struct {
	Name                 string
	ResourceGroup        string
	Location             string
	Size                 int32
	SKU                  string
	State                string
	Zone                 string
	PersistentVolumeName string
	Namespace            string
	ClusterName          string
	Tags                 map[string]*string
}

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

func (d *Disk) IsKubernetesPV() bool {
	return d.PersistentVolumeName != "" || d.ClusterName != ""
}

func (d *Disk) DiskType() string {
	if d.IsKubernetesPV() {
		return "persistent_volume"
	}
	return "unattached_disk"
}

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

func getStringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func extractResourceGroupFromID(id string) string {
	parts := strings.Split(id, "/")
	for i, part := range parts {
		if strings.EqualFold(part, "resourceGroups") && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
