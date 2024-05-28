package eks

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	mockec2 "github.com/grafana/cloudcost-exporter/mocks/pkg/aws/services/ec2"
	mockpricing "github.com/grafana/cloudcost-exporter/mocks/pkg/aws/services/pricing"
	ec2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
	pricingClient "github.com/grafana/cloudcost-exporter/pkg/aws/services/pricing"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

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
			client := mockec2.NewEC2(t)
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

func TestListComputeInstances(t *testing.T) {
	tests := map[string]struct {
		ctx               context.Context
		DescribeInstances func(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
		err               error
		want              []ec2Types.Reservation
		expectedCalls     int
	}{
		"No instance should return nothing": {
			ctx: context.Background(),
			DescribeInstances: func(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
				return &ec2.DescribeInstancesOutput{}, nil
			},
			err:           nil,
			want:          nil,
			expectedCalls: 1,
		},
		"Single instance should return a single instance": {
			ctx: context.Background(),
			DescribeInstances: func(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
				return &ec2.DescribeInstancesOutput{
					Reservations: []ec2Types.Reservation{
						{
							Instances: []ec2Types.Instance{
								{
									InstanceId:   aws.String("i-1234567890abcdef0"),
									InstanceType: ec2Types.InstanceTypeA1Xlarge,
								},
							},
						},
					},
				}, nil
			},
			err: nil,
			want: []ec2Types.Reservation{
				{
					Instances: []ec2Types.Instance{
						{
							InstanceId:   aws.String("i-1234567890abcdef0"),
							InstanceType: ec2Types.InstanceTypeA1Xlarge,
						},
					},
				},
			},
		},
		"Ensure errors propagate": {
			ctx: context.Background(),
			DescribeInstances: func(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
				return nil, assert.AnError
			},
			err:  assert.AnError,
			want: nil,
		},
		"NextToken should return multiple instances": {
			ctx: context.Background(),
			DescribeInstances: func(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
				if e.NextToken == nil {
					return &ec2.DescribeInstancesOutput{
						NextToken: aws.String("token"),
						Reservations: []ec2Types.Reservation{
							{
								Instances: []ec2Types.Instance{
									{
										InstanceId:   aws.String("i-1234567890abcdef0"),
										InstanceType: ec2Types.InstanceTypeA1Xlarge,
									},
								},
							},
						},
					}, nil
				}
				return &ec2.DescribeInstancesOutput{
					Reservations: []ec2Types.Reservation{
						{
							Instances: []ec2Types.Instance{
								{
									InstanceId:   aws.String("i-1234567890abcdef0"),
									InstanceType: ec2Types.InstanceTypeA1Xlarge,
								},
							},
						},
					},
				}, nil
			},

			err: nil,
			want: []ec2Types.Reservation{
				{
					Instances: []ec2Types.Instance{
						{
							InstanceId:   aws.String("i-1234567890abcdef0"),
							InstanceType: ec2Types.InstanceTypeA1Xlarge,
						},
					},
				},
				{
					Instances: []ec2Types.Instance{
						{
							InstanceId:   aws.String("i-1234567890abcdef0"),
							InstanceType: ec2Types.InstanceTypeA1Xlarge,
						},
					},
				},
			},
			expectedCalls: 2,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := mockec2.NewEC2(t)
			client.EXPECT().
				DescribeInstances(mock.Anything, mock.Anything, mock.Anything).
				RunAndReturn(tt.DescribeInstances).
				Times(tt.expectedCalls)

			got, err := ListComputeInstances(tt.ctx, client)
			assert.Equal(t, tt.err, err)
			assert.Equalf(t, tt.want, got, "ListComputeInstances(%v, %v)", tt.ctx, client)
		})
	}
}

func TestCollector_ListOnDemandPrices(t *testing.T) {
	tests := map[string]struct {
		ctx           context.Context
		region        string
		err           error
		GetProducts   func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error)
		want          []string
		expectedCalls int
	}{
		"No products should return nothing": {
			ctx:    context.Background(),
			region: "us-east-1",
			err:    nil,
			want:   nil,
			GetProducts: func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
				return &pricing.GetProductsOutput{
					PriceList: []string{},
				}, nil
			},
		},
		"Single product should return a single product": {
			ctx:    context.Background(),
			region: "us-east-1",
			err:    nil,
			want: []string{
				"This is definitely an accurate test",
			},
			GetProducts: func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
				return &pricing.GetProductsOutput{
					PriceList: []string{
						"This is definitely an accurate test",
					},
				}, nil
			},
		},
		"Ensure errors propagate": {
			ctx:    context.Background(),
			region: "us-east-1",
			err:    assert.AnError,
			want:   nil,
			GetProducts: func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
				return nil, assert.AnError
			},
		},
		"NextToken should return multiple products": {
			ctx:    context.Background(),
			region: "us-east-1",
			err:    nil,
			want: []string{
				"This is definitely an accurate test",
				"This is definitely an accurate test",
			},
			GetProducts: func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
				if input.NextToken == nil {
					return &pricing.GetProductsOutput{
						NextToken: aws.String("token"),
						PriceList: []string{
							"This is definitely an accurate test",
						},
					}, nil
				}
				return &pricing.GetProductsOutput{
					PriceList: []string{
						"This is definitely an accurate test",
					},
				}, nil
			},
			expectedCalls: 2,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client := mockpricing.NewPricing(t)
			client.EXPECT().
				GetProducts(mock.Anything, mock.Anything, mock.Anything).
				RunAndReturn(tt.GetProducts).
				Times(tt.expectedCalls)
			got, err := ListOnDemandPrices(tt.ctx, tt.region, client)
			if tt.err != nil {
				assert.Equal(t, tt.err, err)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewCollector(t *testing.T) {
	tests := map[string]struct {
		region         string
		profile        string
		scrapeInternal time.Duration
		ps             pricingClient.Pricing
		ec2s           ec2client.EC2
	}{
		"Empty region and profile should return a collector": {
			region:         "",
			profile:        "",
			scrapeInternal: 0,
			ps:             nil,
			ec2s:           nil,
		},
		"Region and profile should return a collector": {
			region:         "us-east-1",
			profile:        "default",
			scrapeInternal: 0,
			ps:             nil,
			ec2s:           nil,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			collector := New(tt.region, tt.profile, tt.scrapeInternal, tt.ps, tt.ec2s, nil, nil)
			assert.NotNil(t, collector)
		})
	}
}

func TestCollector_Name(t *testing.T) {
	t.Run("Name should return the same name as the subsystem const", func(t *testing.T) {
		collector := New("", "", 0, nil, nil, nil, nil)
		assert.Equal(t, subsystem, collector.Name())
	})
}

func Test_clusterNameFromInstance(t *testing.T) {
	tests := map[string]struct {
		instance ec2Types.Instance
		want     string
	}{
		"Instance with no tags should return an empty string": {
			instance: ec2Types.Instance{},
			want:     "",
		},
		"Instance with a tag should return the cluster name": {
			instance: ec2Types.Instance{
				Tags: []ec2Types.Tag{
					{
						Key:   aws.String("cluster"),
						Value: aws.String("cluster-name"),
					},
				},
			},
			want: "cluster-name",
		},
		"Instance with eks:clustername should return the cluster name": {
			instance: ec2Types.Instance{
				Tags: []ec2Types.Tag{
					{
						Key:   aws.String("eks:cluster-name"),
						Value: aws.String("cluster-name"),
					},
				},
			},
			want: "cluster-name",
		},
		"Instance with aws:eks:cluster-name should return the cluster name": {
			instance: ec2Types.Instance{
				Tags: []ec2Types.Tag{
					{
						Key:   aws.String("eks:cluster-name"),
						Value: aws.String("cluster-name"),
					},
				},
			},
			want: "cluster-name",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equalf(t, tt.want, clusterNameFromInstance(tt.instance), "clusterNameFromInstance(%v)", tt.instance)
		})
	}
}

func TestCollector_Collect(t *testing.T) {
	regions := []ec2Types.Region{
		{
			RegionName: aws.String("us-east-1"),
		},
	}
	t.Run("Collect should return no error", func(t *testing.T) {
		collector := New("", "", 0, nil, nil, nil, nil)
		ch := make(chan prometheus.Metric)
		go func() {
			err := collector.Collect(ch)
			close(ch)
			assert.NoError(t, err)
		}()
	})

	t.Run("Collect should return an error if ListOnDemandPrices returns an error", func(t *testing.T) {
		regions := []ec2Types.Region{
			{
				RegionName: aws.String("us-east-1"),
			},
		}
		ps := mockpricing.NewPricing(t)
		ps.EXPECT().GetProducts(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
					return nil, assert.AnError
				}).Times(1)
		collector := New("us-east-1", "", 0, ps, nil, regions, nil)
		ch := make(chan prometheus.Metric)
		err := collector.Collect(ch)
		close(ch)
		assert.Error(t, err)
	})
	t.Run("Collect should return a ClientNotFound Error if the client is nil", func(t *testing.T) {
		ps := mockpricing.NewPricing(t)
		ps.EXPECT().GetProducts(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
					return &pricing.GetProductsOutput{
						PriceList: []string{},
					}, nil
				}).Times(1)
		collector := New("", "", 0, ps, nil, regions, nil)
		ch := make(chan prometheus.Metric)
		err := collector.Collect(ch)
		close(ch)
		assert.ErrorIs(t, err, ErrClientNotFound)
	})
	t.Run("Collect should return an error if ListComputeInstances returns an error", func(t *testing.T) {
		ec2s := mockec2.NewEC2(t)
		ec2s.EXPECT().DescribeSpotPriceHistory(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
					return nil, assert.AnError
				}).Times(1)
		ps := mockpricing.NewPricing(t)
		ps.EXPECT().GetProducts(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
					return &pricing.GetProductsOutput{
						PriceList: []string{},
					}, nil
				}).Times(1)
		regionClientMap := make(map[string]ec2client.EC2)
		for _, r := range regions {
			regionClientMap[*r.RegionName] = ec2s
		}
		collector := New("us-east-1", "", 0, ps, ec2s, regions, regionClientMap)
		ch := make(chan prometheus.Metric)
		err := collector.Collect(ch)
		close(ch)
		assert.ErrorIs(t, err, ListSpotPricesErr)
	})
	t.Run("Collect should return an error if GeneratePricingMap returns an error", func(t *testing.T) {
		ec2s := mockec2.NewEC2(t)
		ec2s.EXPECT().DescribeSpotPriceHistory(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
					return &ec2.DescribeSpotPriceHistoryOutput{
						SpotPriceHistory: []ec2Types.SpotPrice{
							{
								AvailabilityZone: aws.String("us-east-1a"),
								InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
								SpotPrice:        aws.String("0.4680000000"),
							},
						},
					}, nil
				}).Times(1)
		ps := mockpricing.NewPricing(t)
		ps.EXPECT().GetProducts(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
					return &pricing.GetProductsOutput{
						PriceList: []string{
							"Unparsable String into json",
						},
					}, nil
				}).Times(1)
		regionClientMap := make(map[string]ec2client.EC2)
		for _, r := range regions {
			regionClientMap[*r.RegionName] = ec2s
		}
		collector := New("us-east-1", "", 0, ps, ec2s, regions, regionClientMap)
		ch := make(chan prometheus.Metric)
		defer close(ch)
		assert.ErrorIs(t, collector.Collect(ch), GeneratePricingMapErr)
	})
	t.Run("Collect should return an error if GeneratePricingMap returns an error", func(t *testing.T) {
		ec2s := mockec2.NewEC2(t)
		ec2s.EXPECT().DescribeSpotPriceHistory(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
					return &ec2.DescribeSpotPriceHistoryOutput{
						SpotPriceHistory: []ec2Types.SpotPrice{
							{
								AvailabilityZone: aws.String("us-east-1a"),
								InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
								SpotPrice:        aws.String("0.4680000000"),
							},
						},
					}, nil
				}).Times(1)
		ec2s.EXPECT().DescribeInstances(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, e *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
					return &ec2.DescribeInstancesOutput{
						Reservations: []ec2Types.Reservation{
							{
								Instances: []ec2Types.Instance{
									{
										InstanceId:   aws.String("i-1234567890abcdef0"),
										InstanceType: ec2Types.InstanceTypeC5ad2xlarge,
										Tags: []ec2Types.Tag{
											{
												Key:   aws.String("eks:cluster-name"),
												Value: aws.String("cluster-name"),
											},
										},
										PrivateDnsName: aws.String("ip-172-31-0-1.ec2.internal"),
										Placement: &ec2Types.Placement{
											AvailabilityZone: aws.String("us-east-1a"),
										},
										InstanceLifecycle: ec2Types.InstanceLifecycleTypeSpot,
									},
									{
										InstanceId:   aws.String("i-1234567891abcdef0"),
										InstanceType: ec2Types.InstanceTypeC5ad2xlarge,
										Tags: []ec2Types.Tag{
											{
												Key:   aws.String("eks:cluster-name"),
												Value: aws.String("cluster-name"),
											},
										},
										PrivateDnsName: aws.String("ip-172-31-0-2.ec2.internal"),
										Placement: &ec2Types.Placement{
											AvailabilityZone: aws.String("not-existent"),
										},
										InstanceLifecycle: ec2Types.InstanceLifecycleTypeCapacityBlock,
									},
									{
										InstanceId:   aws.String("i-1234567891abcdef0"),
										InstanceType: ec2Types.InstanceTypeC5ad2xlarge,
										Tags: []ec2Types.Tag{
											{
												Key:   aws.String("eks:cluster-name"),
												Value: aws.String("cluster-name"),
											},
										},
										PrivateDnsName: aws.String("ip-172-31-0-2.ec2.internal"),
										Placement: &ec2Types.Placement{
											AvailabilityZone: aws.String("us-east-1a"),
										},
										InstanceLifecycle: ec2Types.InstanceLifecycleTypeCapacityBlock,
									},
								},
							},
						},
					}, nil
				}).Times(1)
		ps := mockpricing.NewPricing(t)
		ps.EXPECT().GetProducts(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
					return &pricing.GetProductsOutput{
						PriceList: []string{
							`{"product":{"productFamily":"Compute Instance","attributes":{"enhancedNetworkingSupported":"Yes","intelTurboAvailable":"No","memory":"16 GiB","dedicatedEbsThroughput":"Up to 3170 Mbps","vcpu":"8","classicnetworkingsupport":"false","capacitystatus":"UnusedCapacityReservation","locationType":"AWS Region","storage":"1 x 300 NVMe SSD","instanceFamily":"Compute optimized","operatingSystem":"Linux","intelAvx2Available":"No","regionCode":"us-east-1","physicalProcessor":"AMD EPYC 7R32","clockSpeed":"3.3 GHz","ecu":"NA","networkPerformance":"Up to 10 Gigabit","servicename":"Amazon Elastic Compute Cloud","instancesku":"Q7GDF95MM7MZ7Y5Q","gpuMemory":"NA","vpcnetworkingsupport":"true","instanceType":"c5ad.2xlarge","tenancy":"Shared","usagetype":"AFS1-UnusedBox:c5ad.2xlarge","normalizationSizeFactor":"16","intelAvxAvailable":"No","processorFeatures":"AMD Turbo; AVX; AVX2","servicecode":"AmazonEC2","licenseModel":"No License required","currentGeneration":"Yes","preInstalledSw":"NA","location":"Africa (Cape Town)","processorArchitecture":"64-bit","marketoption":"OnDemand","operation":"RunInstances","availabilityzone":"NA"},"sku":"2257YY4K7BWZ4F46"},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"2257YY4K7BWZ4F46.JRTCKXETXF":{"priceDimensions":{"2257YY4K7BWZ4F46.JRTCKXETXF.6YS6EN2CT7":{"unit":"Hrs","endRange":"Inf","description":"$0.468 per Unused Reservation Linux c5ad.2xlarge Instance Hour","appliesTo":[],"rateCode":"2257YY4K7BWZ4F46.JRTCKXETXF.6YS6EN2CT7","beginRange":"0","pricePerUnit":{"USD":"0.4680000000"}}},"sku":"2257YY4K7BWZ4F46","effectiveDate":"2024-04-01T00:00:00Z","offerTermCode":"JRTCKXETXF","termAttributes":{}}}},"version":"20240508191027","publicationDate":"2024-05-08T19:10:27Z"}`,
						},
					}, nil
				}).Times(1)
		regionClientMap := make(map[string]ec2client.EC2)
		for _, r := range regions {
			regionClientMap[*r.RegionName] = ec2s
		}
		collector := New("us-east-1", "", 0, ps, ec2s, regions, regionClientMap)

		ch := make(chan prometheus.Metric)
		go func() {
			if err := collector.Collect(ch); err != nil {
				assert.NoError(t, err)
			}
			close(ch)
		}()

		var metrics []*utils.MetricResult
		for metric := range ch {
			assert.NotNil(t, metric)
			metrics = append(metrics, utils.ReadMetrics(metric))
		}
		assert.Len(t, metrics, 4)
	})
}
