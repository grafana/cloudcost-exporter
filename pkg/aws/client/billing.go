package client

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	cost "github.com/grafana/cloudcost-exporter/pkg/aws/services/costexplorer"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
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
	slog.Info("Getting billing data", "start", startDate.Format("2006-01-02"), "end", endDate.Format("2006-01-02"))
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
			slog.Error("Error getting cost and usage", "error", err)
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
					slog.Warn("skipping group without keys")
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

// getRegionFromKey returns the region from the key. Keys without a recognisable
// region prefix are returned as "unknown".
func getRegionFromKey(key string) string {
	region := utils.RegionUnknown
	if key == "Requests-Tier1" || key == "Requests-Tier2" {
		return region
	}

	split := strings.Split(key, "-")
	if len(split) < 2 {
		return region
	}

	billingRegion := split[0]
	if region, ok := BillingToRegionMap[billingRegion]; ok {
		return region
	}

	// Per AWS S3 documentation, usage types for us-east-1 may appear without a
	// region prefix (e.g. "TimedStorage-ByteHrs" instead of "USE1-TimedStorage-ByteHrs").
	if billingRegion == "TimedStorage" {
		return BillingToRegionMap["USE1"]
	}
	return region
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
	// Handle known S3 components that appear without a region prefix (no-prefix = us-east-1).
	if split[0] == "TimedStorage" {
		return "TimedStorage"
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

const capacityBlockFeeMarker = "CapacityBlockFee"

// getCapacityBlockCosts fetches the net upfront fees for EC2 Capacity Block for ML
// reservations from Cost Explorer over [startDate, endDate).
//
// Capacity Block fees are booked as one-off charges (RECORD_TYPE=Upfront) under a
// usage type like "USE2-CapacityBlockFee:p5.48xlarge"; Cost Explorer does not
// amortize them. We filter server-side to the Upfront and Refund charge types and
// group by USAGE_TYPE, then sum UnblendedCost per usage type: refunds carry the
// same usage type as a negative amount, so summing nets out cancelled
// reservations. Upfront alone would double-count a cancelled-then-refunded block.
func (b *billing) getCapacityBlockCosts(ctx context.Context, startDate time.Time, endDate time.Time) (*CapacityBlockCosts, error) {
	slog.Info("Getting capacity block costs", "start", startDate.Format("2006-01-02"), "end", endDate.Format("2006-01-02"))
	input := &costexplorer.GetCostAndUsageInput{
		TimePeriod: &types.DateInterval{
			Start: aws.String(startDate.Format("2006-01-02")),
			End:   aws.String(endDate.Format("2006-01-02")),
		},
		Granularity: types.GranularityDaily,
		Metrics:     []string{"UnblendedCost"},
		GroupBy: []types.GroupDefinition{
			{
				Type: types.GroupDefinitionTypeDimension,
				Key:  aws.String("USAGE_TYPE"),
			},
		},
		Filter: &types.Expression{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionRecordType,
				Values: []string{"Upfront", "Refund"},
			},
		},
	}

	var outputs []*costexplorer.GetCostAndUsageOutput
	for {
		b.m.RequestCount.Inc()
		output, err := b.costExplorerService.GetCostAndUsage(ctx, input)
		if err != nil {
			slog.Error("Error getting capacity block costs", "error", err)
			b.m.RequestErrorsCount.Inc()
			return nil, err
		}
		outputs = append(outputs, output)
		if output.NextPageToken == nil {
			break
		}
		input.NextPageToken = output.NextPageToken
	}

	return parseCapacityBlockCosts(outputs), nil
}

// parseCapacityBlockCosts sums Capacity Block fees per region+instance type across
// all returned time periods and record types.
func parseCapacityBlockCosts(outputs []*costexplorer.GetCostAndUsageOutput) *CapacityBlockCosts {
	costs := &CapacityBlockCosts{Regions: make(map[string]map[string]float64)}
	for _, output := range outputs {
		for _, result := range output.ResultsByTime {
			day := ""
			if result.TimePeriod != nil && result.TimePeriod.Start != nil {
				day = *result.TimePeriod.Start
			}
			for _, group := range result.Groups {
				if len(group.Keys) == 0 {
					continue
				}
				usageType := group.Keys[0]
				region, instanceType, ok := parseCapacityBlockUsageType(usageType)
				if !ok {
					slog.Debug("skipping non-capacity-block usage type", "day", day, "usage_type", usageType)
					continue
				}
				metric, ok := group.Metrics["UnblendedCost"]
				if !ok || metric.Amount == nil {
					slog.Debug("capacity block group missing UnblendedCost", "day", day, "usage_type", usageType)
					continue
				}
				fee, err := strconv.ParseFloat(*metric.Amount, 64)
				if err != nil {
					slog.Warn("error parsing capacity block fee", "usage_type", usageType, "error", err)
					continue
				}
				costs.addFee(region, instanceType, fee)
				slog.Debug("processed capacity block fee",
					"day", day,
					"usage_type", usageType,
					"region", region,
					"instance_type", instanceType,
					"amount_usd", fee,
					"running_net_usd", costs.Regions[region][instanceType],
				)
			}
		}
	}
	return costs
}

// parseCapacityBlockUsageType parses a Cost Explorer Capacity Block fee usage type
// (e.g. "USE2-CapacityBlockFee:p5.48xlarge") into its region and instance type.
// Returns ok=false for usage types that are not Capacity Block fees. Usage types
// for us-east-1 may omit the region prefix.
func parseCapacityBlockUsageType(usageType string) (region string, instanceType string, ok bool) {
	if !strings.Contains(usageType, capacityBlockFeeMarker) {
		return "", "", false
	}
	colon := strings.LastIndex(usageType, ":")
	if colon < 0 || colon == len(usageType)-1 {
		return "", "", false
	}
	instanceType = usageType[colon+1:]

	// The billing-region prefix, if present, precedes "-CapacityBlockFee".
	prefixEnd := strings.Index(usageType, "-"+capacityBlockFeeMarker)
	if prefixEnd <= 0 {
		// No region prefix: us-east-1 usage types can appear without one.
		return BillingToRegionMap["US"], instanceType, true
	}
	region, ok = BillingToRegionMap[usageType[:prefixEnd]]
	if !ok {
		return "", "", false
	}
	return region, instanceType, true
}
