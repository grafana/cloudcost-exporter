package gke

import (
	"log"
	"strings"

	"google.golang.org/api/compute/v1"

	gcpCompute "github.com/grafana/cloudcost-exporter/pkg/google/compute"
)

type Disk struct {
	Cluster      string
	Region       string
	Namespace    string
	Name         string
	StorageClass string
	Project      string
	Labels       map[string]string
	Description  map[string]string
}

func NewDisk(disk *compute.Disk, project string) *Disk {
	clusterName := disk.Labels[gcpCompute.GkeClusterLabel]
	region := getRegionFromDisk(disk)

	namespace := getNamespaceFromDisk(disk)
	name := getNameFromDisk(disk)
	diskType := strings.Split(disk.Type, "/")
	d := &Disk{
		Cluster:      clusterName,
		Region:       region,
		Namespace:    namespace,
		Name:         name,
		StorageClass: diskType[len(diskType)-1],
		Project:      project,
	}
	err := extractLabelsFromDesc(disk.Description, d.Description)
	if err != nil {
		log.Printf("error extracting labels from disk(%s) description: %v", d.Name, err)
	}
	return d
}
