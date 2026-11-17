package natgateway

import (
	aws "github.com/aws/aws-sdk-go-v2/aws"
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
)

const (
	// NAT Gateway usage types
	NATGatewayHours         = "NatGateway-Hours"
	NATGatewayBytes         = "NatGateway-Bytes"
	RegionalNATGatewayHours = "RegionalNatGateway-Hours"
	RegionalNATGatewayBytes = "RegionalNatGateway-Bytes"
)

var (
	NATGatewayFilters = []pricingTypes.Filter{
		{
			Field: aws.String("productFamily"),
			Type:  pricingTypes.FilterTypeTermMatch,
			Value: aws.String("NAT Gateway"),
		},
	}
)
