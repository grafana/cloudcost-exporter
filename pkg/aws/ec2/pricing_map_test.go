package ec2

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputePricingMap_AddToComputePricingMap(t *testing.T) {
	tests := map[string]struct {
		cpm        *ComputePricingMap
		Attributes []InstanceAttributes
		Prices     []float64
		want       map[string]*FamilyPricing
	}{
		"No attributes": {
			cpm:        &ComputePricingMap{},
			Attributes: []InstanceAttributes{},
		},
		"Single attribute": {
			cpm: NewComputePricingMap(logger, &Config{}),
			Attributes: []InstanceAttributes{
				{
					Region:         "us-east-1a",
					InstanceType:   "m5.large",
					VCPU:           "1",
					Memory:         "1 GiB",
					InstanceFamily: "General purpose",
				},
			},
			Prices: []float64{1},
			want: map[string]*FamilyPricing{
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
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			for i, attr := range tt.Attributes {
				err := tt.cpm.AddToComputePricingMap(tt.Prices[i], attr)
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, tt.cpm.Regions)
		})
	}
}

func TestComputePricingMap_GenerateComputePricingMap(t *testing.T) {
	tests := map[string]struct {
		regions        []ec2Types.Region
		ondemandPrices []string
		spotPrices     []ec2Types.SpotPrice
		want           *ComputePricingMap
		ondemandErr    error
		spotErr        error
		expectedErr    error
	}{
		"No ondemand or spot prices input": {
			regions:        []ec2Types.Region{{RegionName: aws.String("us-east-1")}},
			ondemandPrices: []string{},
			spotPrices:     []ec2Types.SpotPrice{},
			want: &ComputePricingMap{
				Regions:         map[string]*FamilyPricing{},
				InstanceDetails: map[string]InstanceAttributes{},
			},
		},
		"Just ondemand prices as input": {
			regions: []ec2Types.Region{{RegionName: aws.String("af-south-1")}},
			ondemandPrices: []string{
				`{"product":{"productFamily":"Compute Instance","attributes":{"enhancedNetworkingSupported":"Yes","intelTurboAvailable":"No","memory":"16 GiB","dedicatedEbsThroughput":"Up to 3170 Mbps","vcpu":"8","classicnetworkingsupport":"false","capacitystatus":"UnusedCapacityReservation","locationType":"AWS Region","storage":"1 x 300 NVMe SSD","instanceFamily":"Compute optimized","operatingSystem":"Linux","intelAvx2Available":"No","regionCode":"af-south-1","physicalProcessor":"AMD EPYC 7R32","clockSpeed":"3.3 GHz","ecu":"NA","networkPerformance":"Up to 10 Gigabit","servicename":"Amazon Elastic Compute Cloud","instancesku":"Q7GDF95MM7MZ7Y5Q","gpuMemory":"NA","vpcnetworkingsupport":"true","instanceType":"c5ad.2xlarge","tenancy":"Shared","usagetype":"AFS1-UnusedBox:c5ad.2xlarge","normalizationSizeFactor":"16","intelAvxAvailable":"No","processorFeatures":"AMD Turbo; AVX; AVX2","servicecode":"AmazonEC2","licenseModel":"No License required","currentGeneration":"Yes","preInstalledSw":"NA","location":"Africa (Cape Town)","processorArchitecture":"64-bit","marketoption":"OnDemand","operation":"RunInstances","availabilityzone":"NA"},"sku":"2257YY4K7BWZ4F46"},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"2257YY4K7BWZ4F46.JRTCKXETXF":{"priceDimensions":{"2257YY4K7BWZ4F46.JRTCKXETXF.6YS6EN2CT7":{"unit":"Hrs","endRange":"Inf","description":"$0.468 per Unused Reservation Linux c5ad.2xlarge Instance Hour","appliesTo":[],"rateCode":"2257YY4K7BWZ4F46.JRTCKXETXF.6YS6EN2CT7","beginRange":"0","pricePerUnit":{"USD":"0.4680000000"}}},"sku":"2257YY4K7BWZ4F46","effectiveDate":"2024-04-01T00:00:00Z","offerTermCode":"JRTCKXETXF","termAttributes":{}}}},"version":"20240508191027","publicationDate":"2024-05-08T19:10:27Z"}`,
			},
			spotPrices: []ec2Types.SpotPrice{},
			want: &ComputePricingMap{
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
				InstanceDetails: map[string]InstanceAttributes{
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
		"Just spot prices as input": {
			regions:        []ec2Types.Region{{RegionName: aws.String("af-south-1")}},
			ondemandPrices: []string{},
			spotPrices: []ec2Types.SpotPrice{
				{
					AvailabilityZone: aws.String("af-south-1a"),
					InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
					SpotPrice:        aws.String("0.4680000000"),
				},
			},
			want: &ComputePricingMap{
				Regions:         map[string]*FamilyPricing{},
				InstanceDetails: map[string]InstanceAttributes{},
			},
		},
		"Ondemand and spot prices": {
			regions: []ec2Types.Region{{RegionName: aws.String("af-south-1")}},
			ondemandPrices: []string{
				`{"product":{"productFamily":"Compute Instance","attributes":{"enhancedNetworkingSupported":"Yes","intelTurboAvailable":"No","memory":"16 GiB","dedicatedEbsThroughput":"Up to 3170 Mbps","vcpu":"8","classicnetworkingsupport":"false","capacitystatus":"UnusedCapacityReservation","locationType":"AWS Region","storage":"1 x 300 NVMe SSD","instanceFamily":"Compute optimized","operatingSystem":"Linux","intelAvx2Available":"No","regionCode":"af-south-1","physicalProcessor":"AMD EPYC 7R32","clockSpeed":"3.3 GHz","ecu":"NA","networkPerformance":"Up to 10 Gigabit","servicename":"Amazon Elastic Compute Cloud","instancesku":"Q7GDF95MM7MZ7Y5Q","gpuMemory":"NA","vpcnetworkingsupport":"true","instanceType":"c5ad.2xlarge","tenancy":"Shared","usagetype":"AFS1-UnusedBox:c5ad.2xlarge","normalizationSizeFactor":"16","intelAvxAvailable":"No","processorFeatures":"AMD Turbo; AVX; AVX2","servicecode":"AmazonEC2","licenseModel":"No License required","currentGeneration":"Yes","preInstalledSw":"NA","location":"Africa (Cape Town)","processorArchitecture":"64-bit","marketoption":"OnDemand","operation":"RunInstances","availabilityzone":"NA"},"sku":"2257YY4K7BWZ4F46"},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"2257YY4K7BWZ4F46.JRTCKXETXF":{"priceDimensions":{"2257YY4K7BWZ4F46.JRTCKXETXF.6YS6EN2CT7":{"unit":"Hrs","endRange":"Inf","description":"$0.468 per Unused Reservation Linux c5ad.2xlarge Instance Hour","appliesTo":[],"rateCode":"2257YY4K7BWZ4F46.JRTCKXETXF.6YS6EN2CT7","beginRange":"0","pricePerUnit":{"USD":"0.4680000000"}}},"sku":"2257YY4K7BWZ4F46","effectiveDate":"2024-04-01T00:00:00Z","offerTermCode":"JRTCKXETXF","termAttributes":{}}}},"version":"20240508191027","publicationDate":"2024-05-08T19:10:27Z"}`,
			},
			spotPrices: []ec2Types.SpotPrice{
				{
					AvailabilityZone: aws.String("af-south-1a"),
					InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
					SpotPrice:        aws.String("0.4680000000"),
				},
			},
			want: &ComputePricingMap{
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
				InstanceDetails: map[string]InstanceAttributes{
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
		"Returns error when ListOnDemandPrices fails": {
			regions: []ec2Types.Region{
				{RegionName: aws.String("us-east-1")},
			},
			ondemandErr: errors.New("listing error"),
			expectedErr: ErrListOnDemandPrices,
		},
		"Returns error when ListSpotPrices fails": {
			regions: []ec2Types.Region{
				{RegionName: aws.String("us-east-1")},
			},
			spotErr:     errors.New("listing error"),
			expectedErr: ErrListSpotPrices,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mock := &mockClient{
				ondemandPrices: tt.ondemandPrices,
				spotPrices:     tt.spotPrices,
				ondemandErr:    tt.ondemandErr,
				spotErr:        tt.spotErr,
			}

			regionName := *tt.regions[0].RegionName
			config := &Config{
				Regions: tt.regions,
				RegionMap: map[string]client.Client{
					regionName: mock,
				},
			}

			cpm := NewComputePricingMap(logger, config)
			err := cpm.GenerateComputePricingMap(context.Background())
			assert.ErrorIs(t, err, tt.expectedErr)

			if tt.want != nil {
				assert.Equal(t, tt.want.Regions, cpm.Regions)
				assert.Equal(t, tt.want.InstanceDetails, cpm.InstanceDetails)
			}
		})
	}
}

func TestStoragePricingMap_GenerateStoragePricingMap(t *testing.T) {
	tests := map[string]struct {
		regions          []ec2Types.Region
		prices           []string
		listStorageError error
		expected         map[string]*StoragePricing
		expectedError    error
	}{
		"Empty if AWS returns no volume prices": {
			regions:  []ec2Types.Region{{RegionName: aws.String("us-east-1")}},
			prices:   []string{},
			expected: map[string]*StoragePricing{},
		},
		"Parses AWS volume prices response": {
			regions: []ec2Types.Region{{RegionName: aws.String("af-south-1")}},
			prices: []string{
				`{"product":{"productFamily":"Storage","attributes":{"maxThroughputvolume":"1000 MiB/s","volumeType":"General Purpose","maxIopsvolume":"16000","usagetype":"AFS1-EBS:VolumeUsage.gp3","locationType":"AWS Region","maxVolumeSize":"16 TiB","storageMedia":"SSD-backed","regionCode":"af-south-1","servicecode":"AmazonEC2","volumeApiName":"gp3","location":"Africa (Cape Town)","servicename":"Amazon Elastic Compute Cloud","operation":""},"sku":"XWCTMRRUJM7TGYST"},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"XWCTMRRUJM7TGYST.JRTCKXETXF":{"priceDimensions":{"XWCTMRRUJM7TGYST.JRTCKXETXF.6YS6EN2CT7":{"unit":"GB-Mo","endRange":"Inf","description":"$0.1047 per GB-month of General Purpose (gp3) provisioned storage - Africa (Cape Town)","appliesTo":[],"rateCode":"XWCTMRRUJM7TGYST.JRTCKXETXF.6YS6EN2CT7","beginRange":"0","pricePerUnit":{"USD":"0.1047000000"}}},"sku":"XWCTMRRUJM7TGYST","effectiveDate":"2024-07-01T00:00:00Z","offerTermCode":"JRTCKXETXF","termAttributes":{}}}},"version":"20240705013454","publicationDate":"2024-07-05T01:34:54Z"}`,
			},
			expected: map[string]*StoragePricing{
				"af-south-1": {
					Storage: map[string]float64{
						"gp3": 0.1047,
					},
				},
			},
		},
		"Returns error when ListStoragePrices fails": {
			regions:          []ec2Types.Region{{RegionName: aws.String("us-east-1")}},
			listStorageError: errors.New("listing error"),
			expected:         map[string]*StoragePricing{},
			expectedError:    ErrListStoragePrices,
		},
		"Returns error when parsing invalid JSON": {
			regions:       []ec2Types.Region{{RegionName: aws.String("af-south-1")}},
			prices:        []string{"invalid json response"},
			expected:      map[string]*StoragePricing{},
			expectedError: ErrGeneratePricingMap,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			mock := &mockClient{
				storagePrices: tt.prices,
				storageErr:    tt.listStorageError,
			}

			regionName := *tt.regions[0].RegionName
			config := &Config{
				Regions: tt.regions,
				RegionMap: map[string]client.Client{
					regionName: mock,
				},
			}
			spm := NewStoragePricingMap(logger, config)
			err := spm.GenerateStoragePricingMap(context.Background())
			assert.ErrorIs(t, err, tt.expectedError)

			assert.Equal(t, tt.expected, spm.Regions)
		})
	}
}

func TestStructuredPricingMap_GetPriceForInstanceType(t *testing.T) {
	tests := map[string]struct {
		cpm          *ComputePricingMap
		region       string
		instanceType string
		err          error
		want         *Prices
	}{
		"An empty structured pricing map should return a no region found error": {
			cpm:          NewComputePricingMap(logger, &Config{}),
			region:       "us-east-1",
			instanceType: "m5.large",
			err:          ErrRegionNotFound,
		},
		"An empty region should return a no instance type found error": {
			cpm: &ComputePricingMap{
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
			cpm: &ComputePricingMap{
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
			price, err := tt.cpm.GetPriceForInstanceType(tt.region, tt.instanceType)
			if tt.err != nil {
				require.ErrorIs(t, tt.err, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, price)
		})
	}
}

func TestStoragePricingMap_GetPriceForVolumeType(t *testing.T) {
	tests := map[string]struct {
		spm        *StoragePricingMap
		region     string
		volumeType string
		size       int32
		err        error
		expected   float64
	}{
		"an empty map should return a no region found error": {
			spm:        NewStoragePricingMap(logger, &Config{}),
			region:     "us-east-1",
			volumeType: "gp3",
			size:       100,
			err:        ErrRegionNotFound,
		},
		"volume not found in region should return an error": {
			spm: &StoragePricingMap{
				Regions: map[string]*StoragePricing{
					"us-east-1": {},
				},
			},
			region:     "us-east-1",
			volumeType: "gp3",
			size:       100,
			err:        ErrVolumeTypeNotFound,
		},
		"price should account for volume size and monthly to hourly price conversion": {
			spm: &StoragePricingMap{
				Regions: map[string]*StoragePricing{
					"us-east-1": {
						Storage: map[string]float64{
							"gp3": .4,
						},
					},
				},
			},
			region:     "us-east-1",
			volumeType: "gp3",
			size:       100,
			expected:   .4 / 30 / 24 * 100,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			price, err := tt.spm.GetPriceForVolumeType(tt.region, tt.volumeType, tt.size)
			if tt.err != nil {
				require.ErrorIs(t, tt.err, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, price)
		})
	}
}

func Test_weightedPriceForInstance(t *testing.T) {
	tests := map[string]struct {
		price      float64
		attributes InstanceAttributes
		err        error
		want       *Prices
	}{
		"No attributes should return a parse error": {
			price:      0.65,
			attributes: InstanceAttributes{},
			err:        ErrParseAttributes,
		},
		"No memory should return a parse error": {
			price: 0.65,
			attributes: InstanceAttributes{
				VCPU: "1",
			},
			err: ErrParseAttributes,
		},
		"Handle a machine that's general purpose": {
			price: 1.0,
			attributes: InstanceAttributes{
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
			attributes: InstanceAttributes{
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
			attributes: InstanceAttributes{
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
			attributes: InstanceAttributes{
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
			attributes: InstanceAttributes{
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
			attributes: InstanceAttributes{
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

func TestComputePricingMap_GenerateComputePricingMap_Refresh(t *testing.T) {
	regions := []ec2Types.Region{{RegionName: aws.String("us-east-1")}}

	t.Run("Refresh should update prices when called multiple times", func(t *testing.T) {
		// Initial price: $0.10 for m5.large
		initialPrice := `{"product":{"productFamily":"Compute Instance","attributes":{"memory":"8 GiB","vcpu":"2","regionCode":"us-east-1","instanceFamily":"General purpose","operatingSystem":"Linux","instanceType":"m5.large","tenancy":"Shared","usagetype":"BoxUsage:m5.large","marketoption":"OnDemand","physicalProcessor":"Intel Xeon Platinum 8175","clockSpeed":"2.5 GHz"}},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"OFFER.JRTCKXETXF":{"priceDimensions":{"OFFER.JRTCKXETXF.6YS6EN2CT7":{"unit":"Hrs","pricePerUnit":{"USD":"0.1000000000"}}}}}}}`

		// Updated price: $0.20 for m5.large
		updatedPrice := `{"product":{"productFamily":"Compute Instance","attributes":{"memory":"8 GiB","vcpu":"2","regionCode":"us-east-1","instanceFamily":"General purpose","operatingSystem":"Linux","instanceType":"m5.large","tenancy":"Shared","usagetype":"BoxUsage:m5.large","marketoption":"OnDemand","physicalProcessor":"Intel Xeon Platinum 8175","clockSpeed":"2.5 GHz"}},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"OFFER.JRTCKXETXF":{"priceDimensions":{"OFFER.JRTCKXETXF.6YS6EN2CT7":{"unit":"Hrs","pricePerUnit":{"USD":"0.2000000000"}}}}}}}`

		mock := &mockClient{
			ondemandPrices: []string{initialPrice},
			spotPrices:     []ec2Types.SpotPrice{},
		}

		config := &Config{
			Regions: regions,
			RegionMap: map[string]client.Client{
				"us-east-1": mock,
			},
		}

		cpm := NewComputePricingMap(logger, config)

		err := cpm.GenerateComputePricingMap(context.Background())
		require.NoError(t, err)

		// Verify initial price
		price1, err := cpm.GetPriceForInstanceType("us-east-1", "m5.large")
		require.NoError(t, err)
		assert.Equal(t, 0.1, price1.Total, "Initial price should be $0.10")

		mock.ondemandPrices = []string{updatedPrice}

		// Second call - refresh with new prices
		err = cpm.GenerateComputePricingMap(context.Background())
		require.NoError(t, err)

		price2, err := cpm.GetPriceForInstanceType("us-east-1", "m5.large")
		require.NoError(t, err)
		assert.Equal(t, 0.2, price2.Total, "After refresh, price should be updated to $0.20")
	})
}

func TestStoragePricingMap_GenerateStoragePricingMap_Refresh(t *testing.T) {
	regions := []ec2Types.Region{{RegionName: aws.String("us-east-1")}}

	t.Run("Refresh should update prices when called multiple times", func(t *testing.T) {
		// Initial price: $0.08 for gp3
		initialPrice := `{"product":{"productFamily":"Storage","attributes":{"volumeType":"General Purpose","regionCode":"us-east-1","volumeApiName":"gp3","location":"US East (N. Virginia)"}},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"GP3.JRTCKXETXF":{"priceDimensions":{"GP3.JRTCKXETXF.6YS6EN2CT7":{"unit":"GB-Mo","pricePerUnit":{"USD":"0.0800000000"}}}}}}}`

		// Updated price: $0.10 for gp3
		updatedPrice := `{"product":{"productFamily":"Storage","attributes":{"volumeType":"General Purpose","regionCode":"us-east-1","volumeApiName":"gp3","location":"US East (N. Virginia)"}},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"GP3.JRTCKXETXF":{"priceDimensions":{"GP3.JRTCKXETXF.6YS6EN2CT7":{"unit":"GB-Mo","pricePerUnit":{"USD":"0.1000000000"}}}}}}}`

		mock := &mockClient{
			storagePrices: []string{initialPrice},
		}

		config := &Config{
			Regions: regions,
			RegionMap: map[string]client.Client{
				"us-east-1": mock,
			},
		}

		spm := NewStoragePricingMap(logger, config)

		err := spm.GenerateStoragePricingMap(context.Background())
		require.NoError(t, err)

		// Verify initial price
		price1, err := spm.GetPriceForVolumeType("us-east-1", "gp3", 100)
		require.NoError(t, err)
		expectedPrice1 := 0.08 * 100 / 730
		assert.InDelta(t, expectedPrice1, price1, 0.001, "Initial price should be based on $0.08/GB-month")

		mock.storagePrices = []string{updatedPrice}

		// Second call - refresh with new prices
		err = spm.GenerateStoragePricingMap(context.Background())
		require.NoError(t, err)

		price2, err := spm.GetPriceForVolumeType("us-east-1", "gp3", 100)
		require.NoError(t, err)
		expectedPrice2 := 0.10 * 100 / 730 // $0.10 per GB-month * 100GB / 730 hours
		assert.InDelta(t, expectedPrice2, price2, 0.001, "After refresh, price should be updated to $0.10/GB-month")
	})
}
