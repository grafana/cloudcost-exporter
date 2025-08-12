package common

import (
	"errors"
	"regexp"
	"strings"

	"github.com/aws/smithy-go"

	awscostexplorer "github.com/aws/aws-sdk-go-v2/service/costexplorer"
	awsclient "github.com/grafana/cloudcost-exporter/pkg/aws/client"
)

// Helpers
// Parse the billing data into shared billing structure
func ParseBilling(outputs []*awscostexplorer.GetCostAndUsageOutput, componentReStr string) *awsclient.BillingData {
	componentRe := regexp.MustCompile(componentReStr)
	b := &awsclient.BillingData{Regions: make(map[string]*awsclient.PricingModel)}
	for _, out := range outputs {
		for _, r := range out.ResultsByTime {
			for _, g := range r.Groups {
				if g.Keys == nil || len(g.Keys) < 2 {
					continue
				}
				service := g.Keys[0]
				// Omit any tax lines entirely
				if strings.EqualFold(service, "Tax") || strings.Contains(strings.ToLower(service), "tax") {
					continue
				}
				usageKey := g.Keys[1]
				region := getRegionFromKey(usageKey)
				component := getComponentFromKey(usageKey)
				// Only include NAT Gateway usage lines
				if !componentRe.MatchString(component) {
					continue
				}
				// Prefix component with service to carry it forward
				b.AddMetricGroup(region, service+"|"+component, g)
			}
		}
	}
	return b
}

func getRegionFromKey(key string) string {
	split := strings.Split(key, "-")
	if len(split) < 2 {
		return ""
	}
	billingRegion := split[0]
	if region, ok := awsclient.BillingToRegionMap[billingRegion]; ok {
		return region
	}
	return ""
}

func getComponentFromKey(key string) string {
	split := strings.Split(key, "-")
	if len(split) < 2 {
		return ""
	}
	return strings.Join(split[1:], "-")
}

// isBillExpirationError determines whether the error returned by Cost Explorer
// indicates that the bill changed during pagination and the request should be retried.
func IsBillExpirationError(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		if apiErr.ErrorCode() == "BillExpirationException" {
			return true
		}
	}
	// Fallback on message substring in case the error is not typed
	return strings.Contains(strings.ToLower(err.Error()), "bill has been updated since last call")
}
