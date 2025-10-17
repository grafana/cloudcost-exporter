package client

import (
	"context"
	"fmt"
	"time"

	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	awsPricing "github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingTypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	ec2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
	pricingClient "github.com/grafana/cloudcost-exporter/pkg/aws/services/pricing"
)

type pricing struct {
	client    pricingClient.Pricing
	ec2Client ec2client.EC2
}

func newPricing(client pricingClient.Pricing, ec2 ec2client.EC2) *pricing {
	return &pricing{
		client:    client,
		ec2Client: ec2,
	}
}

func (p *pricing) listOnDemandPrices(ctx context.Context, region string) ([]string, error) {
	input := &awsPricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		Filters: []pricingTypes.Filter{
			{
				Field: aws.String("regionCode"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String(region),
			},
			{
				// Limit output to only base installs
				Field: aws.String("preInstalledSw"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String("NA"),
			},
			{
				// Limit to shared tenancy machines
				Field: aws.String("tenancy"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String("shared"),
			},
			{
				// Limit to ec2 instances(ie, not bare metal)
				Field: aws.String("productFamily"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String("Compute Instance"),
			},
			{
				// RunInstances is the operation that we're interested in.
				Field: aws.String("operation"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String("RunInstances"),
			},
			{
				// This effectively filters only for ondemand pricing
				Field: aws.String("capacitystatus"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String("UnusedCapacityReservation"),
			},
			{
				// Only care about Linux. If there's a request for windows, remove this flag and expand the pricing map to include a key for operating system
				Field: aws.String("operatingSystem"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String("Linux"),
			},
		},
	}

	return p.getPricesFromProductList(ctx, input)
}

func (p *pricing) listSpotPrices(ctx context.Context) ([]ec2Types.SpotPrice, error) {
	var spotPrices []ec2Types.SpotPrice
	startTime := time.Now().Add(-time.Hour)
	endTime := time.Now()
	sphi := &ec2.DescribeSpotPriceHistoryInput{
		ProductDescriptions: []string{
			"Linux/UNIX (Amazon VPC)",
		},

		StartTime: &startTime,
		EndTime:   &endTime,
	}
	for {
		resp, err := p.ec2Client.DescribeSpotPriceHistory(ctx, sphi)
		if err != nil {
			// If there's an error, return the set of processed spotPrices and the error.
			return spotPrices, err
		}
		spotPrices = append(spotPrices, resp.SpotPriceHistory...)
		if resp.NextToken == nil || *resp.NextToken == "" {
			break
		}
		sphi.NextToken = resp.NextToken
	}
	return spotPrices, nil
}

func (p *pricing) listStoragePrices(ctx context.Context, region string) ([]string, error) {
	input := &awsPricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		Filters: []pricingTypes.Filter{
			{
				Field: aws.String("regionCode"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String(region),
			},
			// Get prices for EBS Volumes
			{
				Field: aws.String("productFamily"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String("Storage"),
			},
		},
	}

	return p.getPricesFromProductList(ctx, input)
}

func (p *pricing) makeEC2ServiceInput(region string) *awsPricing.GetProductsInput {
	input := &awsPricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		Filters: []pricingTypes.Filter{
			{
				Field: aws.String("regionCode"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String(region),
			},
		},
	}
	return input
}

func (p *pricing) makeVPCServiceInput(region string) *awsPricing.GetProductsInput {
	input := &awsPricing.GetProductsInput{
		ServiceCode: aws.String("AmazonVPC"),
		Filters: []pricingTypes.Filter{
			{
				Field: aws.String("regionCode"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String(region),
			},
		},
	}
	return input
}

func (p *pricing) listEC2ServicePrices(ctx context.Context, region string, filters []pricingTypes.Filter) ([]string, error) {
	input := p.makeEC2ServiceInput(region)
	input.Filters = append(input.Filters, filters...)
	return p.getPricesFromProductList(ctx, input)
}

func (p *pricing) listVPCServicePrices(ctx context.Context, region string, filters []pricingTypes.Filter) ([]string, error) {
	// VPC service pricing (VPC Endpoints, Transit Gateway, Elastic IPs)
	input := p.makeVPCServiceInput(region)
	input.Filters = append(input.Filters, filters...)
	return p.getPricesFromProductList(ctx, input)
}

func (p *pricing) listELBPrices(ctx context.Context, region string) ([]string, error) {
	// Fetch ELB pricing from AWS Pricing API
	input := &awsPricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		Filters: []pricingTypes.Filter{
			{
				Field: aws.String("regionCode"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String(region),
			},
			{
				Field: aws.String("productFamily"),
				Type:  pricingTypes.FilterTypeContains,
				Value: aws.String("Load Balancer"),
			},
		},
	}
	var productOutputs []string
	for {
		products, err := p.client.GetProducts(ctx, input)
		if err != nil {
			return productOutputs, err
		}

		if products == nil {
			break
		}

		productOutputs = append(productOutputs, products.PriceList...)
		if products.NextToken == nil || *products.NextToken == "" {
			break
		}
		input.NextToken = products.NextToken
	}
	return productOutputs, nil
}

func (p *pricing) getPricesFromProductList(ctx context.Context, input *awsPricing.GetProductsInput) ([]string, error) {
	var productOutputs []string

	for {
		products, err := p.client.GetProducts(ctx, input)
		if err != nil {
			return productOutputs, err
		}

		if products == nil {
			break
		}

		productOutputs = append(productOutputs, products.PriceList...)
		if products.NextToken == nil || *products.NextToken == "" {
			break
		}
		input.NextToken = products.NextToken
	}
	return productOutputs, nil
}

func (p *pricing) getRDSUnitData(ctx context.Context, instType, region, deploymentOption, databaseEngine, locationType string) (string, error) {
	input := &awsPricing.GetProductsInput{
		ServiceCode: aws.String("AmazonRDS"),
		Filters: []pricingTypes.Filter{
			{
				Field: aws.String("productFamily"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String("Database Instance"),
			},
			{
				Field: aws.String("instanceType"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String(instType),
			},
			{
				Field: aws.String("regionCode"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String(region),
			},
			{
				Field: aws.String("deploymentOption"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String(deploymentOption),
			},
			{
				Field: aws.String("databaseEngine"),
				Type:  pricingTypes.FilterTypeContains,
				Value: aws.String(databaseEngine),
			},
			{
				Field: aws.String("locationType"),
				Type:  pricingTypes.FilterTypeTermMatch,
				Value: aws.String(locationType),
			},
		},
	}

	products, err := p.client.GetProducts(ctx, input)
	if err != nil {
		slog.ErrorContext(ctx, "error getting rds prices", "error", err)
		return "", err
	}

	if len(products.PriceList) != 1 {
		slog.ErrorContext(ctx, "expected 1 price list, got", "count", len(products.PriceList))
		return "", fmt.Errorf("expected 1 price list, got %d", len(products.PriceList))
	}
	return products.PriceList[0], nil
}
