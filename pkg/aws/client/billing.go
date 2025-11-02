package client

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	cost "github.com/grafana/cloudcost-exporter/pkg/aws/services/costexplorer"
)

type billing struct {
	costExplorerService cost.CostExplorer
	m                   *Metrics
}

func newBilling(costExplorerService cost.CostExplorer, m *Metrics) *billing {
	return &billing{
		costExplorerService: costExplorerService,
		m:                   m,
	}
}

func (b *billing) getBillingData(ctx context.Context, startDate time.Time, endDate time.Time) (*BillingData, error) {
	log.Printf("Getting billing data for %s to %s\n", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	input := &costexplorer.GetCostAndUsageInput{
		TimePeriod: &types.DateInterval{
			Start: aws.String(startDate.Format("2006-01-02")), // Specify the start date
			End:   aws.String(endDate.Format("2006-01-02")),   // Specify the end date
		},
		Granularity: types.GranularityDaily,
		Metrics:     []string{"UsageQuantity", "UnblendedCost"},
		// NB: You can only pass in one USAGE_TYPE per query
		GroupBy: []types.GroupDefinition{
			{
				Type: types.GroupDefinitionTypeDimension,
				Key:  aws.String("USAGE_TYPE"),
			},
		},
		Filter: &types.Expression{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionService,
				Values: []string{"Amazon Simple Storage Service"},
			},
		},
	}

	var outputs []*costexplorer.GetCostAndUsageOutput
	for {
		b.m.RequestCount.Inc()
		output, err := b.costExplorerService.GetCostAndUsage(ctx, input)
		if err != nil {
			log.Printf("Error getting cost and usage: %v\n", err)
			b.m.RequestErrorsCount.Inc()
			return nil, err
		}
		outputs = append(outputs, output)
		if output.NextPageToken == nil {
			break
		}
		input.NextPageToken = output.NextPageToken
	}

	return parseBillingData(outputs), nil
}

// parseBillingData takes the output from the AWS Cost Explorer API and parses it into a S3BillingData struct
func parseBillingData(outputs []*costexplorer.GetCostAndUsageOutput) *BillingData {
	billingData := &BillingData{
		Regions: make(map[string]*PricingModel),
	}

	// Process the billing data in the 'output' variable
	for _, output := range outputs {
		for _, result := range output.ResultsByTime {
			for _, group := range result.Groups {
				if group.Keys == nil {
					log.Printf("skipping group without keys")
					continue
				}
				key := group.Keys[0]
				region := getRegionFromKey(key)
				component := getComponentFromKey(key)
				if region == "" || component == "" {
					continue
				}
				billingData.AddMetricGroup(region, component, group)
			}
		}
	}
	return billingData
}

// getRegionFromKey returns the region from the key. If the key is requests, it will return an empty string because there is no region associated with it.
func getRegionFromKey(key string) string {
	if key == "Requests-Tier1" || key == "Requests-Tier2" {
		return ""
	}

	split := strings.Split(key, "-")
	if len(split) < 2 {
		log.Printf("Could not find region in key: %s\n", key)
		return ""
	}

	billingRegion := split[0]
	if region, ok := BillingToRegionMap[billingRegion]; ok {
		return region
	}
	log.Printf("Could not find mapped region: %s:%s\n", key, billingRegion)
	return ""
}

// getComponentFromKey returns the component from the key. If the component does not contain a region, it will return
// an empty string. If the component is requests, it will return the tier as well.
func getComponentFromKey(key string) string {
	if key == "Requests-Tier1" || key == "Requests-Tier2" {
		return ""
	}
	val := ""
	split := strings.Split(key, "-")
	if len(split) < 2 {
		return val
	}
	val = split[1]
	// Check to see if the value is a region. If so, set val to empty string to skip the dimension
	// Currently this is such a minor part of our bill that it's not worth it.
	if _, ok := BillingToRegionMap[val]; ok {
		val = ""
	}
	// If it's requests, we want to include if it's tier 1 or tier 2
	if val == "Requests" {
		val += "-" + split[2]
	}
	return val
}
