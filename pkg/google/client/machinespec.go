package client

import (
	"log"
	"regexp"
	"strings"

	"google.golang.org/api/compute/v1"
)

var (
	re              = regexp.MustCompile(`\bin\b`)
	GkeClusterLabel = "goog-k8s-cluster-name"
	GkeRegionLabel  = "goog-k8s-cluster-location"
)

// MachineSpec is a slimmed down representation of a google compute.Instance struct
type MachineSpec struct {
	Instance     string
	Zone         string
	Region       string
	Family       string
	MachineType  string
	SpotInstance bool
	Labels       map[string]string
	PriceTier    string
}

// NewMachineSpec will create a new MachineSpec from compute.Instance objects.
// It's responsible for determining the machine family and region that it operates in
func NewMachineSpec(instance *compute.Instance) *MachineSpec {
	zone := instance.Zone[strings.LastIndex(instance.Zone, "/")+1:]
	region := getRegionFromZone(zone)
	machineType := getMachineTypeFromURL(instance.MachineType)
	family := getMachineFamily(machineType)
	spot := isSpotInstance(instance.Scheduling.ProvisioningModel)
	priceTier := priceTierForInstance(spot)

	return &MachineSpec{
		Instance:     instance.Name,
		Zone:         zone,
		Region:       region,
		MachineType:  machineType,
		Family:       family,
		SpotInstance: spot,
		Labels:       instance.Labels,
		PriceTier:    priceTier,
	}
}

func isSpotInstance(model string) bool {
	return model == "SPOT"
}

func getRegionFromZone(zone string) string {
	return zone[:strings.LastIndex(zone, "-")]
}

func getMachineTypeFromURL(url string) string {
	return url[strings.LastIndex(url, "/")+1:]
}

func getMachineFamily(machineType string) string {
	if !strings.Contains(machineType, "-") {
		log.Printf("Machine type %s doesn't contain a -", machineType)
		return ""
	}
	split := strings.Split(machineType, "-")
	return strings.ToLower(split[0])
}

func stripOutKeyFromDescription(description string) string {
	// Except for commitments, the description will have running in it
	runningInIndex := strings.Index(description, "running in")

	if runningInIndex > 0 {
		description = description[:runningInIndex]
		return strings.Trim(description, " ")
	}
	// If we can't find running in, try to find Commitment v1:
	splitString := strings.Split(description, "Commitment v1:")
	if len(splitString) == 1 {
		log.Printf("No running in or commitment found in description: %s", description)
		return ""
	}
	// Take everything after the Commitment v1
	// TODO: Evaluate if we want to consider leaving in Commitment V1
	split := splitString[1]
	// Now something a bit more tricky, we need to find an exact match of "in"
	// Turns out that locations such as Berlin break this assumption
	// SO we need to use a regexp to find the first instance of "in"
	foundIndex := re.FindStringIndex(split)
	if len(foundIndex) == 0 {
		log.Printf("No in found in description: %s", description)
		return ""
	}
	str := split[:foundIndex[0]]
	return strings.Trim(str, " ")
}

func priceTierForInstance(spotInstance bool) string {
	if spotInstance {
		return "spot"
	}
	// TODO: Handle if it's a commitment
	return "ondemand"
}

func (m *MachineSpec) GetClusterName() string {
	if clusterName, ok := m.Labels[GkeClusterLabel]; ok {
		return clusterName
	}
	return ""
}
