package client

import (
	"fmt"
	"log"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

// billingToRegionMap maps the AWS billing region code to the AWS region
// Billing codes: https://docs.aws.amazon.com/AmazonS3/latest/userguide/aws-usage-report-understand.html
// Regions: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-regions-availability-zones.html#concepts-available-regions
var BillingToRegionMap = map[string]string{
	"APE1":                   "ap-east-1",      // Hong Kong
	"APN1":                   "ap-northeast-1", // Tokyo
	"APN2":                   "ap-northeast-2", // Seoul
	"APN3":                   "ap-northeast-3", // Osaka
	"APS1":                   "ap-southeast-1", // Singapore
	"APS2":                   "ap-southeast-2", // Sydney
	"APS3":                   "ap-south-1",     // Mumbai
	"APS4":                   "ap-southeast-3", // Jakarta is APS4, but is southeast-3
	"APS5":                   "ap-south-2",     // Hyderabad
	"APS6":                   "ap-southeast-4", // Melbourne
	"CAN1":                   "ca-central-1",   // Canada
	"CNN1":                   "cn-north-1",     // Beijing
	"CNW1":                   "cn-northwest-1", // Ningxia
	"CPT1":                   "af-south-1",     // Cape Town
	"EUC1":                   "eu-central-1",   // Frankfurt
	"EUC2":                   "eu-central-2",   // Zurich
	"EU":                     "eu-west-1",      // Ireland
	"EUW2":                   "eu-west-2",      // London
	"EUW3":                   "eu-west-3",      // Paris
	"EUN1":                   "eu-north-1",     // Stockholm
	"EUS1":                   "eu-south-1",     // Milan
	"EUS2":                   "eu-south-2",     // Spain
	"MEC1":                   "me-central-1",   // UAE
	"MES1":                   "me-south-1",     // Bahrain
	"SAE1":                   "sa-east-1",      // Sao Paulo
	"US":                     "us-east-1",      // N. Virginia, documentations state there could be no prefix
	"USE1":                   "us-east-1",      // N. Virginia
	"USE2":                   "us-east-2",      // Ohio
	"USW1":                   "us-west-1",      // N. California
	"USW2":                   "us-west-2",      // Oregon
	"AWS GovCloud (US-East)": "us-gov-east-1",
	"AWS GovCloud (US)":      "us-gov-west-1",
}

// BillingData is the struct for the data we will be collecting
type BillingData struct {
	// Regions is a map where string is the region and PricingModel is the value
	Regions map[string]*PricingModel
}

// AddMetricGroup adds a metric group to the Region. If the key is empty, it will not add the metric group
// to the Region. If the dimension is empty, it will not add the metric group to the Region.
// Dimensions are cumulative and will be added to the same dimension if the dimension already exists.
func (s *BillingData) AddMetricGroup(region string, component string, group types.Group) {
	if region == "" || component == "" {
		return
	}

	// Check if the region is in the billingToRegionMap
	// If not we need to instantiate the map, otherwise it will panic
	if _, ok := s.Regions[region]; !ok {
		s.Regions[region] = &PricingModel{
			Model: make(map[string]*Pricing),
		}
	}

	// Check if the component is in the map
	// If not we need to instantiate the map, otherwise it will panic
	if _, ok := s.Regions[region].Model[component]; !ok {
		s.Regions[region].Model[component] = &Pricing{}
	}

	componentsMap := s.Regions[region].Model[component]
	for name, metric := range group.Metrics {
		if metric.Amount == nil {
			fmt.Printf("Error parsing amount: amount is nil\n")
			continue
		}

		switch name {
		case "UsageQuantity":
			usageAmount, err := strconv.ParseFloat(*metric.Amount, 64)
			if err != nil {
				fmt.Printf("Error parsing usage amount: %v\n", err)
				continue
			}
			componentsMap.Usage += usageAmount

			if metric.Unit == nil {
				fmt.Printf("Error parsing amount: unit is nil\n")
				continue
			}
			componentsMap.Units = *metric.Unit
		case "UnblendedCost":
			cost, err := strconv.ParseFloat(*metric.Amount, 64)
			if err != nil {
				fmt.Printf("Error parsing cost amount: %v\n", err)
				continue
			}
			componentsMap.Cost += cost
		}
	}

	componentsMap.UnitCost = unitCostForComponent(component, componentsMap)
}

// unitCostForComponent will calculate the unit cost for a given component. This is necessary because the
// unit cost will depend on the type of component.
func unitCostForComponent(component string, pricing *Pricing) float64 {
	// If the usage is 0, we don't want to divide by 0 which would result in NaN metrics _or_ +Inf
	// TODO: Assess if we should return the pricing.Cost instead
	if pricing.Usage == 0 {
		log.Printf("Usage is 0 for component: %s\n", component)
		return 0
	}

	switch component {
	case "Requests-Tier1", "Requests-Tier2":
		return pricing.Cost / (pricing.Usage / 1000)
	case "TimedStorage":
		return (pricing.Cost / utils.HoursInMonth) / pricing.Usage
	default:
		return pricing.Cost / pricing.Usage
	}
}

type PricingModel struct {
	// Model is a map where string is the component and Pricing is the usage, cost and unit
	Model map[string]*Pricing
}

type Pricing struct {
	Usage    float64
	Cost     float64
	Units    string
	UnitCost float64
}
