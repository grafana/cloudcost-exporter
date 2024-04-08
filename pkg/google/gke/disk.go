package gke

import (
	"encoding/json"
	"log"
	"strings"

	"google.golang.org/api/compute/v1"

	gcpCompute "github.com/grafana/cloudcost-exporter/pkg/google/compute"
)

var (
	keys = []string{"kubernetes.io/created-for/pvc/namespace", "kubernetes.io-created-for/pvc-namespace"}
)

const (
	BootDiskLabel = "goog-gke-node"
)

type Disk struct {
	Cluster     string
	DiskName    string // Name of the disk as it appears in the GCP console. Used as a backup if the name can't be extracted from the description
	Zone        string
	Project     string
	Labels      map[string]string
	Description map[string]string
	Type        string
}

func NewDisk(disk *compute.Disk, project string) *Disk {
	clusterName := disk.Labels[gcpCompute.GkeClusterLabel]
	d := &Disk{
		Cluster:     clusterName,
		DiskName:    disk.Name,
		Project:     project,
		Zone:        disk.Zone,
		Type:        disk.Type,
		Labels:      disk.Labels,
		Description: make(map[string]string),
	}
	err := extractLabelsFromDesc(disk.Description, d.Description)
	if err != nil {
		log.Printf("error extracting labels from disk(%s) description: %v", d.Name(), err)
	}
	return d
}

// Namespace will search through the description fields for the namespace of the disk. If the namespace can't be determined
// An empty string is return.
func (d *Disk) Namespace() string {
	return coalesce(d.Description, keys...)
}

// Region will return the region of the disk by search through the zone field and returning the region. If the region can't be determined
// It will return an empty string
func (d *Disk) Region() string {
	zone := d.Labels[gcpCompute.GkeRegionLabel]
	if zone == "" {
		// This would be a case where the disk is no longer mounted _or_ the disk is associated with a Compute instance
		zone = d.Zone[strings.LastIndex(d.Zone, "/")+1:]
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
func (d *Disk) Name() string {
	if d.Description == nil {
		return d.DiskName
	}
	// first check that the key exists in the map, if it does return the value
	name := coalesce(d.Description, "kubernetes.io/created-for/pv/name", "kubernetes.io-created-for/pv-name")
	if name != "" {
		return name
	}
	return d.DiskName
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
func (d *Disk) StorageClass() string {
	diskType := strings.Split(d.Type, "/")
	return diskType[len(diskType)-1]
}

// BootDisk will search through the labels for the existing of the BootDiskLabel and return true if it exists, otherwise false.
func (d *Disk) BootDisk() bool {
	if _, ok := d.Labels[BootDiskLabel]; ok {
		return true
	}
	return false
}
