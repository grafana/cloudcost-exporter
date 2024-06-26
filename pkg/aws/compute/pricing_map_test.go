package compute

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	ec22 "github.com/grafana/cloudcost-exporter/mocks/pkg/aws/services/ec2"
)

func TestStructuredPricingMap_AddToPricingMap(t *testing.T) {
	tests := map[string]struct {
		spm        *StructuredPricingMap
		Attributes []Attributes
		Prices     []float64
		want       *StructuredPricingMap
	}{
		"No attributes": {
			spm:        &StructuredPricingMap{},
			Attributes: []Attributes{},
			want:       &StructuredPricingMap{},
		},
		"Single attribute": {
			spm: NewStructuredPricingMap(),
			Attributes: []Attributes{
				{
					Region:         "us-east-1a",
					InstanceType:   "m5.large",
					VCPU:           "1",
					Memory:         "1 GiB",
					InstanceFamily: "General purpose",
				},
			},
			Prices: []float64{1},
			want: &StructuredPricingMap{
				Regions: map[string]*FamilyPricing{
					"us-east-1a": {
						Family: map[string]*Prices{
							"m5.large": {
								Cpu:   0.65,
								Ram:   0.35,
								Total: 1.0,
							},
						},
					},
				},
				InstanceDetails: make(map[string]Attributes),
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			for i, attr := range tt.Attributes {
				err := tt.spm.AddToPricingMap(tt.Prices[i], attr)
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, tt.spm)
		})
	}
}

func TestStructuredPricingMap_GeneratePricingMap(t *testing.T) {
	tests := map[string]struct {
		smp        *StructuredPricingMap
		prices     []string
		spotPrices []ec2Types.SpotPrice
		want       *StructuredPricingMap
	}{
		"No prices input": {
			smp:        NewStructuredPricingMap(),
			prices:     []string{},
			spotPrices: []ec2Types.SpotPrice{},
			want:       NewStructuredPricingMap(),
		},
		"Just prices as input": {
			smp: NewStructuredPricingMap(),
			prices: []string{
				`{"product":{"productFamily":"Compute Instance","attributes":{"enhancedNetworkingSupported":"Yes","intelTurboAvailable":"No","memory":"16 GiB","dedicatedEbsThroughput":"Up to 3170 Mbps","vcpu":"8","classicnetworkingsupport":"false","capacitystatus":"UnusedCapacityReservation","locationType":"AWS Region","storage":"1 x 300 NVMe SSD","instanceFamily":"Compute optimized","operatingSystem":"Linux","intelAvx2Available":"No","regionCode":"af-south-1","physicalProcessor":"AMD EPYC 7R32","clockSpeed":"3.3 GHz","ecu":"NA","networkPerformance":"Up to 10 Gigabit","servicename":"Amazon Elastic Compute Cloud","instancesku":"Q7GDF95MM7MZ7Y5Q","gpuMemory":"NA","vpcnetworkingsupport":"true","instanceType":"c5ad.2xlarge","tenancy":"Shared","usagetype":"AFS1-UnusedBox:c5ad.2xlarge","normalizationSizeFactor":"16","intelAvxAvailable":"No","processorFeatures":"AMD Turbo; AVX; AVX2","servicecode":"AmazonEC2","licenseModel":"No License required","currentGeneration":"Yes","preInstalledSw":"NA","location":"Africa (Cape Town)","processorArchitecture":"64-bit","marketoption":"OnDemand","operation":"RunInstances","availabilityzone":"NA"},"sku":"2257YY4K7BWZ4F46"},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"2257YY4K7BWZ4F46.JRTCKXETXF":{"priceDimensions":{"2257YY4K7BWZ4F46.JRTCKXETXF.6YS6EN2CT7":{"unit":"Hrs","endRange":"Inf","description":"$0.468 per Unused Reservation Linux c5ad.2xlarge Instance Hour","appliesTo":[],"rateCode":"2257YY4K7BWZ4F46.JRTCKXETXF.6YS6EN2CT7","beginRange":"0","pricePerUnit":{"USD":"0.4680000000"}}},"sku":"2257YY4K7BWZ4F46","effectiveDate":"2024-04-01T00:00:00Z","offerTermCode":"JRTCKXETXF","termAttributes":{}}}},"version":"20240508191027","publicationDate":"2024-05-08T19:10:27Z"}`,
			},
			spotPrices: []ec2Types.SpotPrice{},
			want: &StructuredPricingMap{
				Regions: map[string]*FamilyPricing{
					"af-south-1": {
						Family: map[string]*Prices{
							"c5ad.2xlarge": {
								Cpu:   0.051480000000000005,
								Ram:   0.00351,
								Total: 0.4680000000,
							},
						},
					},
				},
				InstanceDetails: map[string]Attributes{
					"c5ad.2xlarge": {
						Region:            "af-south-1",
						InstanceType:      "c5ad.2xlarge",
						VCPU:              "8",
						Memory:            "16 GiB",
						InstanceFamily:    "Compute optimized",
						PhysicalProcessor: "AMD EPYC 7R32",
						Tenancy:           "Shared",
						MarketOption:      "OnDemand",
						OperatingSystem:   "Linux",
						ClockSpeed:        "3.3 GHz",
						UsageType:         "AFS1-UnusedBox:c5ad.2xlarge",
					},
				},
			},
		},
		"Price and a spot price": {
			smp: NewStructuredPricingMap(),
			prices: []string{
				`{"product":{"productFamily":"Compute Instance","attributes":{"enhancedNetworkingSupported":"Yes","intelTurboAvailable":"No","memory":"16 GiB","dedicatedEbsThroughput":"Up to 3170 Mbps","vcpu":"8","classicnetworkingsupport":"false","capacitystatus":"UnusedCapacityReservation","locationType":"AWS Region","storage":"1 x 300 NVMe SSD","instanceFamily":"Compute optimized","operatingSystem":"Linux","intelAvx2Available":"No","regionCode":"af-south-1","physicalProcessor":"AMD EPYC 7R32","clockSpeed":"3.3 GHz","ecu":"NA","networkPerformance":"Up to 10 Gigabit","servicename":"Amazon Elastic Compute Cloud","instancesku":"Q7GDF95MM7MZ7Y5Q","gpuMemory":"NA","vpcnetworkingsupport":"true","instanceType":"c5ad.2xlarge","tenancy":"Shared","usagetype":"AFS1-UnusedBox:c5ad.2xlarge","normalizationSizeFactor":"16","intelAvxAvailable":"No","processorFeatures":"AMD Turbo; AVX; AVX2","servicecode":"AmazonEC2","licenseModel":"No License required","currentGeneration":"Yes","preInstalledSw":"NA","location":"Africa (Cape Town)","processorArchitecture":"64-bit","marketoption":"OnDemand","operation":"RunInstances","availabilityzone":"NA"},"sku":"2257YY4K7BWZ4F46"},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"2257YY4K7BWZ4F46.JRTCKXETXF":{"priceDimensions":{"2257YY4K7BWZ4F46.JRTCKXETXF.6YS6EN2CT7":{"unit":"Hrs","endRange":"Inf","description":"$0.468 per Unused Reservation Linux c5ad.2xlarge Instance Hour","appliesTo":[],"rateCode":"2257YY4K7BWZ4F46.JRTCKXETXF.6YS6EN2CT7","beginRange":"0","pricePerUnit":{"USD":"0.4680000000"}}},"sku":"2257YY4K7BWZ4F46","effectiveDate":"2024-04-01T00:00:00Z","offerTermCode":"JRTCKXETXF","termAttributes":{}}}},"version":"20240508191027","publicationDate":"2024-05-08T19:10:27Z"}`,
			},
			spotPrices: []ec2Types.SpotPrice{
				{
					AvailabilityZone: aws.String("af-south-1a"),
					InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
					SpotPrice:        aws.String("0.4680000000"),
				},
			},
			want: &StructuredPricingMap{
				Regions: map[string]*FamilyPricing{
					"af-south-1": {
						Family: map[string]*Prices{
							"c5ad.2xlarge": {
								Cpu:   0.051480000000000005,
								Ram:   0.00351,
								Total: 0.4680000000,
							},
						},
					},
					"af-south-1a": {
						Family: map[string]*Prices{
							"c5ad.2xlarge": {
								Cpu:   0.051480000000000005,
								Ram:   0.00351,
								Total: 0.4680000000,
							},
						},
					},
				},
				InstanceDetails: map[string]Attributes{
					"c5ad.2xlarge": {
						Region:            "af-south-1",
						InstanceType:      "c5ad.2xlarge",
						VCPU:              "8",
						Memory:            "16 GiB",
						InstanceFamily:    "Compute optimized",
						PhysicalProcessor: "AMD EPYC 7R32",
						Tenancy:           "Shared",
						MarketOption:      "OnDemand",
						OperatingSystem:   "Linux",
						ClockSpeed:        "3.3 GHz",
						UsageType:         "AFS1-UnusedBox:c5ad.2xlarge",
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := tt.smp.GeneratePricingMap(tt.prices, tt.spotPrices)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, tt.smp)
		})
	}
}

func TestStructuredPricingMap_GetPriceForInstanceType(t *testing.T) {
	tests := map[string]struct {
		spm          *StructuredPricingMap
		region       string
		instanceType string
		err          error
		want         *Prices
	}{
		"An empty structured pricing map should return a no Region found error": {
			spm:          NewStructuredPricingMap(),
			region:       "us-east-1",
			instanceType: "m5.large",
			err:          ErrRegionNotFound,
		},
		"An empty region should return a no instance type found error": {
			spm: &StructuredPricingMap{
				Regions: map[string]*FamilyPricing{
					"us-east-1": {
						Family: map[string]*Prices{},
					},
				},
			},
			region:       "us-east-1",
			instanceType: "m5.large",
			err:          ErrInstanceTypeNotFound,
		},
		"A region with an instance type should return the price": {
			spm: &StructuredPricingMap{
				Regions: map[string]*FamilyPricing{
					"us-east-1": {
						Family: map[string]*Prices{
							"m5.large": {
								Cpu: 0.65,
								Ram: 0.35,
							},
						},
					},
				},
			},
			region:       "us-east-1",
			instanceType: "m5.large",
			want: &Prices{
				Cpu: 0.65,
				Ram: 0.35,
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			price, err := tt.spm.GetPriceForInstanceType(tt.region, tt.instanceType)
			if tt.err != nil {
				require.ErrorIs(t, tt.err, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, price)
		})
	}
}

func Test_weightedPriceForInstance(t *testing.T) {
	tests := map[string]struct {
		price      float64
		attributes Attributes
		err        error
		want       *Prices
	}{
		"No attributes should return a parse error": {
			price:      0.65,
			attributes: Attributes{},
			err:        ErrParseAttributes,
		},
		"No memory should return a parse error": {
			price: 0.65,
			attributes: Attributes{
				VCPU: "1",
			},
			err: ErrParseAttributes,
		},
		"Handle a machine that's general purpose": {
			price: 1.0,
			attributes: Attributes{
				VCPU:           "1",
				Memory:         "1 GiB",
				InstanceFamily: "General purpose",
			},
			want: &Prices{
				Cpu: 0.65,
				Ram: 0.35,
			},
		},
		"Handle a machine that's compute optimized": {
			price: 1.0,
			attributes: Attributes{
				VCPU:           "1",
				Memory:         "1 GiB",
				InstanceFamily: "Compute optimized",
			},
			want: &Prices{
				Cpu: 0.88,
				Ram: 0.12,
			},
		},
		"Handle a machine that's memory optimized": {
			price: 1.0,
			attributes: Attributes{
				VCPU:           "1",
				Memory:         "1 GiB",
				InstanceFamily: "Memory optimized",
			},
			want: &Prices{
				Cpu: 0.48,
				Ram: 0.52,
			},
		},
		"Handle a machine that's storage optimized": {
			price: 1.0,
			attributes: Attributes{
				VCPU:           "1",
				Memory:         "1 GiB",
				InstanceFamily: "Storage optimized",
			},
			want: &Prices{
				Cpu: 0.48,
				Ram: 0.52,
			},
		},
		"Handle a machine that doesn't have an instance family": {
			price: 1.0,
			attributes: Attributes{
				VCPU:   "1",
				Memory: "1 GiB",
			},
			want: &Prices{
				Cpu: 0.65,
				Ram: 0.35,
			},
		},
		"Handle a machine that has a family that doesn't exist": {
			price: 1.0,
			attributes: Attributes{
				VCPU:           "1",
				Memory:         "1 GiB",
				InstanceFamily: "Totally a real instance family",
			},
			want: &Prices{
				Cpu: 0.65,
				Ram: 0.35,
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := weightedPriceForInstance(tt.price, tt.attributes)
			if tt.err != nil {
				assert.ErrorIs(t, err, tt.err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestListSpotPrices(t *testing.T) {
	tests := map[string]struct {
		ctx                      context.Context
		DescribeSpotPriceHistory func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error)
		err                      error
		want                     []ec2Types.SpotPrice
		expectedCalls            int
	}{
		"No instance should return nothing": {
			ctx: context.Background(),
			DescribeSpotPriceHistory: func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
				return &ec2.DescribeSpotPriceHistoryOutput{}, nil
			},
			err:           nil,
			want:          nil,
			expectedCalls: 1,
		},
		"Single instance should return a single instance": {
			ctx: context.Background(),
			DescribeSpotPriceHistory: func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
				return &ec2.DescribeSpotPriceHistoryOutput{
					SpotPriceHistory: []ec2Types.SpotPrice{
						{
							AvailabilityZone: aws.String("us-east-1a"),
							InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
							SpotPrice:        aws.String("0.4680000000"),
						},
					},
				}, nil
			},
			err: nil,
			want: []ec2Types.SpotPrice{
				{
					AvailabilityZone: aws.String("us-east-1a"),
					InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
					SpotPrice:        aws.String("0.4680000000"),
				},
			},
			expectedCalls: 1,
		},
		"Ensure errors propagate": {
			ctx: context.Background(),
			DescribeSpotPriceHistory: func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
				return nil, assert.AnError
			},
			err:           assert.AnError,
			want:          nil,
			expectedCalls: 1,
		},
		"NextToken should return multiple instances": {
			ctx: context.Background(),
			DescribeSpotPriceHistory: func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
				if input.NextToken == nil {
					return &ec2.DescribeSpotPriceHistoryOutput{
						NextToken: aws.String("token"),
						SpotPriceHistory: []ec2Types.SpotPrice{
							{
								AvailabilityZone: aws.String("us-east-1a"),
								InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
								SpotPrice:        aws.String("0.4680000000"),
							},
						},
					}, nil
				}
				return &ec2.DescribeSpotPriceHistoryOutput{
					SpotPriceHistory: []ec2Types.SpotPrice{
						{
							AvailabilityZone: aws.String("us-east-1a"),
							InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
							SpotPrice:        aws.String("0.4680000000"),
						},
					},
				}, nil
			},
			err: nil,
			want: []ec2Types.SpotPrice{
				{
					AvailabilityZone: aws.String("us-east-1a"),
					InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
					SpotPrice:        aws.String("0.4680000000"),
				},
				{
					AvailabilityZone: aws.String("us-east-1a"),
					InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
					SpotPrice:        aws.String("0.4680000000"),
				},
			},
			expectedCalls: 2,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := ec22.NewEC2(t)
			client.EXPECT().
				DescribeSpotPriceHistory(mock.Anything, mock.Anything, mock.Anything).
				RunAndReturn(tt.DescribeSpotPriceHistory).
				Times(tt.expectedCalls)

			got, err := ListSpotPrices(tt.ctx, client)
			if tt.err != nil {
				assert.Equal(t, tt.err, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
