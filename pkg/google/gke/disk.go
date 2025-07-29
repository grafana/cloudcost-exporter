package gke

import (
	"encoding/json"
	"log"
	"strings"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"google.golang.org/api/compute/v1"
)

const (
	BootDiskLabel        = "goog-gke-node"
	pvcNamespaceKey      = "kubernetes.io/created-for/pvc/namespace"
	pvcNamespaceShortKey = "kubernetes.io-created-for/pvc-namespace"
	pvNameKey            = "kubernetes.io/created-for/pv/name"
	pvNameShortKey       = "kubernetes.io-created-for/pv-name"
	idleDisk             = "idle"
	inUseDisk            = "in-use"
)

type Disk struct {
	Cluster string

	Project     string
	name        string // Name of the disk as it appears in the GCP console. Used as a backup if the name can't be extracted from the description
	zone        string
	labels      map[string]string
	description map[string]string
	diskType    string // type is a reserved word, which is why we're using diskType
	Size        int64
	users       []string
}

func NewDisk(disk *compute.Disk, project string) *Disk {
	clusterName := disk.Labels[client.GkeClusterLabel]
	d := &Disk{
		Cluster:     clusterName,
		Project:     project,
		name:        disk.Name,
		zone:        disk.Zone,
		diskType:    disk.Type,
		labels:      disk.Labels,
		description: make(map[string]string),
		Size:        disk.SizeGb,
		users:       disk.Users,
	}
	err := extractLabelsFromDesc(disk.Description, d.description)
	if err != nil {
		log.Printf("error extracting labels from disk(%s) description: %v", d.Name(), err)
	}
	return d
}

// Namespace will search through the description fields for the namespace of the disk. If the namespace can't be determined
// An empty string is return.
func (d Disk) Namespace() string {
	return coalesce(d.description, pvcNamespaceKey, pvcNamespaceShortKey)
}

// Region will return the region of the disk by search through the zone field and returning the region. If the region can't be determined
// It will return an empty string
func (d Disk) Region() string {
	zone := d.labels[client.GkeRegionLabel]
	if zone == "" {
		// This would be a case where the disk is no longer mounted _or_ the disk is associated with a Compute instance
		zone = d.zone[strings.LastIndex(d.zone, "/")+1:]
	}
	// If zone _still_ is empty we can't determine the region, so we return an empty string
	// This prevents an index out of bounds error
	if zone == "" {
		return ""
	}
	if strings.Count(zone, "-") < 2 {
		return zone
	}
	return zone[:strings.LastIndex(zone, "-")]
}

// Name will return the name of the disk. If the disk has a label "kubernetes.io/created-for/pv/name" it will return the value stored in that key.
// otherwise it will return the disk name that is directly associated with the disk.
func (d Disk) Name() string {
	if d.description == nil {
		return d.name
	}
	// first check that the key exists in the map, if it does return the value
	name := coalesce(d.description, pvNameKey, pvNameShortKey)
	if name != "" {
		return name
	}
	return d.name
}

// coalesce will take a map and a list of keys and return the first value that is found in the map. If no value is found it will return an empty string
func coalesce(desc map[string]string, keys ...string) string {
	for _, key := range keys {
		if val, ok := desc[key]; ok {
			return val
		}
	}
	return ""
}

// extractLabelsFromDesc will take a description string and extract the labels from it. GKE disks store their description as
// a json blob in the description field. This function will extract the labels from that json blob and return them as a map
// Some useful information about the json blob are name, cluster, namespace, and pvc's that the disk is associated with
func extractLabelsFromDesc(description string, labels map[string]string) error {
	if description == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(description), &labels); err != nil {
		return err
	}
	return nil
}

// StorageClass will return the storage class of the disk by looking at the type. Type in GCP is represented as a URL and as such
// we're looking for the last part of the URL to determine the storage class
func (d Disk) StorageClass() string {
	diskType := strings.Split(d.diskType, "/")
	return diskType[len(diskType)-1]
}

// DiskType will search through the labels to determine the type of disk. If the disk has a label "goog-gke-node" it will return "boot_disk"
// Otherwise it returns persistent_volume
func (d Disk) DiskType() string {
	if _, ok := d.labels[BootDiskLabel]; ok {
		return "boot_disk"
	}
	return "persistent_volume"
}

// UseStatus will return two constant strings to tell apart disks that are sitting idle from those that are mounted to a pod
// It's named UseStatus and not just Status because the GCP API already has a field Status that holds a different concept that
// we don't want to overwrite. From their docs:
// Status: [Output Only] The status of disk creation. - CREATING: Disk is
// provisioning. - RESTORING: Source data is being copied into the disk. -
// FAILED: Disk creation failed. - READY: Disk is ready for use. - DELETING:
// Disk is deleting. UNAVAILABLE - Disk is currently unavailable and cannot be accessed,
func (d Disk) UseStatus() string {
	if len(d.users) == 0 {
		return idleDisk
	}

	return inUseDisk
}
