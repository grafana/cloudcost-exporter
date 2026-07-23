package ec2

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	mock_client "github.com/grafana/cloudcost-exporter/pkg/aws/client/mocks"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var (
	logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
)

func TestCollector_Name(t *testing.T) {
	t.Run("Name should return the same name as the subsystem const", func(t *testing.T) {
		collector, err := New(context.Background(), &Config{
			ScrapeInterval: time.Minute,
			AccountID:      "123456789012",
		}, logger)
		require.NoError(t, err)
		assert.Equal(t, subsystem, collector.Name())
	})
}

func TestNew(t *testing.T) {
	regions := []ec2Types.Region{{RegionName: aws.String("us-east-1")}}
	t.Run("New should return ClientNotFound error when RegionMap is empty", func(t *testing.T) {
		_, err := New(context.Background(), &Config{
			Regions:        regions,
			ScrapeInterval: time.Minute,
			AccountID:      "123456789012",
			RegionMap:      map[string]client.Client{}, // Empty map - no client
		}, logger)
		assert.ErrorIs(t, err, ErrClientNotFound)
	})

	t.Run("New should return error when compute pricing initialization fails", func(t *testing.T) {
		mock := &mockClient{
			ondemandErr: errors.New("error"),
		}

		_, err := New(context.Background(), &Config{
			Regions:        regions,
			ScrapeInterval: time.Minute,
			AccountID:      "123456789012",
			RegionMap: map[string]client.Client{
				"us-east-1": mock,
			},
		}, logger)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrListOnDemandPrices)
	})

	t.Run("New should return error when storage pricing initialization fails", func(t *testing.T) {
		mock := &mockClient{
			ondemandPrices: []string{
				`{"product":{"productFamily":"Compute Instance","attributes":{"memory":"8 GiB","vcpu":"2","regionCode":"us-east-1","instanceFamily":"General purpose","operatingSystem":"Linux","instanceType":"m5.large","tenancy":"Shared","usagetype":"BoxUsage:m5.large","marketoption":"OnDemand","physicalProcessor":"Intel Xeon Platinum 8175","clockSpeed":"2.5 GHz"}},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"OFFER.JRTCKXETXF":{"priceDimensions":{"OFFER.JRTCKXETXF.6YS6EN2CT7":{"unit":"Hrs","pricePerUnit":{"USD":"0.0960000000"}}}}}}}`,
			},
			spotPrices: []ec2Types.SpotPrice{},
			storageErr: errors.New("error"),
		}

		_, err := New(context.Background(), &Config{
			Regions:        regions,
			ScrapeInterval: time.Minute,
			AccountID:      "123456789012",
			RegionMap: map[string]client.Client{
				"us-east-1": mock,
			},
		}, logger)
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrListStoragePrices)
	})

	t.Run("New should succeed with valid config and populated pricing maps", func(t *testing.T) {
		mock := &mockClient{
			ondemandPrices: []string{
				`{"product":{"productFamily":"Compute Instance","attributes":{"memory":"8 GiB","vcpu":"2","regionCode":"us-east-1","instanceFamily":"General purpose","operatingSystem":"Linux","instanceType":"m5.large","tenancy":"Shared","usagetype":"BoxUsage:m5.large","marketoption":"OnDemand","physicalProcessor":"Intel Xeon Platinum 8175","clockSpeed":"2.5 GHz"}},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"OFFER.JRTCKXETXF":{"priceDimensions":{"OFFER.JRTCKXETXF.6YS6EN2CT7":{"unit":"Hrs","pricePerUnit":{"USD":"0.0960000000"}}}}}}}`,
			},
			spotPrices: []ec2Types.SpotPrice{},
			storagePrices: []string{
				`{"product":{"productFamily":"Storage","attributes":{"volumeType":"General Purpose","regionCode":"us-east-1","volumeApiName":"gp3","location":"US East (N. Virginia)"}},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"GP3.JRTCKXETXF":{"priceDimensions":{"GP3.JRTCKXETXF.6YS6EN2CT7":{"unit":"GB-Mo","pricePerUnit":{"USD":"0.0800000000"}}}}}}}`,
			},
		}

		collector, err := New(context.Background(), &Config{
			Regions:        regions,
			ScrapeInterval: time.Minute,
			AccountID:      "123456789012",
			RegionMap: map[string]client.Client{
				"us-east-1": mock,
			},
		}, logger)
		require.NoError(t, err)
		assert.NotNil(t, collector)

		// Verify pricing maps were populated during initialization
		assert.NotEmpty(t, collector.computePricingMap.Regions, "Compute pricing map should be populated")
		assert.NotEmpty(t, collector.storagePricingMap.Regions, "Storage pricing map should be populated")
	})
}

func TestCollector_Collect(t *testing.T) {
	ctrl := gomock.NewController(t)
	regions := []ec2Types.Region{
		{
			RegionName: aws.String("us-east-1"),
		},
	}
	t.Run("Collect should return no error", func(t *testing.T) {
		collector, err := New(context.Background(), &Config{
			ScrapeInterval: time.Minute,
			AccountID:      "123456789012",
		}, logger)
		require.NoError(t, err)
		ch := make(chan prometheus.Metric)
		go func() {
			err := collector.Collect(t.Context(), ch)
			close(ch)
			assert.NoError(t, err)
		}()
	})

	t.Run("Test cpu, memory and total cost metrics emitted for each valid instance", func(t *testing.T) {
		c := mock_client.NewMockClient(ctrl)
		c.EXPECT().ListSpotPrices(gomock.Any()).
			DoAndReturn(
				func(ctx context.Context) ([]ec2Types.SpotPrice, error) {
					return []ec2Types.SpotPrice{
						{
							AvailabilityZone: aws.String("us-east-1a"),
							InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
							SpotPrice:        aws.String("0.4680000000"),
						},
					}, nil
				}).MinTimes(1)
		c.EXPECT().ListComputeInstances(gomock.Any()).
			DoAndReturn(
				func(ctx context.Context) ([]ec2Types.Reservation, error) {
					return []ec2Types.Reservation{
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
									InstanceLifecycle: ec2Types.InstanceLifecycleTypeScheduled,
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
									InstanceLifecycle: ec2Types.InstanceLifecycleTypeScheduled,
								},
							},
						},
					}, nil
				}).Times(1)
		c.EXPECT().ListOnDemandPrices(gomock.Any(), gomock.Any()).
			DoAndReturn(
				func(ctx context.Context, region string) ([]string, error) {
					return []string{
						`{"product":{"productFamily":"Compute Instance","attributes":{"enhancedNetworkingSupported":"Yes","intelTurboAvailable":"No","memory":"16 GiB","dedicatedEbsThroughput":"Up to 3170 Mbps","vcpu":"8","classicnetworkingsupport":"false","capacitystatus":"UnusedCapacityReservation","locationType":"AWS Region","storage":"1 x 300 NVMe SSD","instanceFamily":"Compute optimized","operatingSystem":"Linux","intelAvx2Available":"No","regionCode":"us-east-1","physicalProcessor":"AMD EPYC 7R32","clockSpeed":"3.3 GHz","ecu":"NA","networkPerformance":"Up to 10 Gigabit","servicename":"Amazon Elastic Compute Cloud","instancesku":"Q7GDF95MM7MZ7Y5Q","gpuMemory":"NA","vpcnetworkingsupport":"true","instanceType":"c5ad.2xlarge","tenancy":"Shared","usagetype":"AFS1-UnusedBox:c5ad.2xlarge","normalizationSizeFactor":"16","intelAvxAvailable":"No","processorFeatures":"AMD Turbo; AVX; AVX2","servicecode":"AmazonEC2","licenseModel":"No License required","currentGeneration":"Yes","preInstalledSw":"NA","location":"Africa (Cape Town)","processorArchitecture":"64-bit","marketoption":"OnDemand","operation":"RunInstances","availabilityzone":"NA"},"sku":"2257YY4K7BWZ4F46"},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"2257YY4K7BWZ4F46.JRTCKXETXF":{"priceDimensions":{"2257YY4K7BWZ4F46.JRTCKXETXF.6YS6EN2CT7":{"unit":"Hrs","endRange":"Inf","description":"$0.468 per Unused Reservation Linux c5ad.2xlarge Instance Hour","appliesTo":[],"rateCode":"2257YY4K7BWZ4F46.JRTCKXETXF.6YS6EN2CT7","beginRange":"0","pricePerUnit":{"USD":"0.4680000000"}}},"sku":"2257YY4K7BWZ4F46","effectiveDate":"2024-04-01T00:00:00Z","offerTermCode":"JRTCKXETXF","termAttributes":{}}}},"version":"20240508191027","publicationDate":"2024-05-08T19:10:27Z"}`,
					}, nil
				}).MinTimes(1)
		c.EXPECT().ListStoragePrices(gomock.Any(), gomock.Any()).
			DoAndReturn(
				func(ctx context.Context, region string) ([]string, error) {
					return []string{
						`{"product":{"productFamily":"Compute Instance","attributes":{"enhancedNetworkingSupported":"Yes","intelTurboAvailable":"No","memory":"16 GiB","dedicatedEbsThroughput":"Up to 3170 Mbps","vcpu":"8","classicnetworkingsupport":"false","capacitystatus":"UnusedCapacityReservation","locationType":"AWS Region","storage":"1 x 300 NVMe SSD","instanceFamily":"Compute optimized","operatingSystem":"Linux","intelAvx2Available":"No","regionCode":"us-east-1","physicalProcessor":"AMD EPYC 7R32","clockSpeed":"3.3 GHz","ecu":"NA","networkPerformance":"Up to 10 Gigabit","servicename":"Amazon Elastic Compute Cloud","instancesku":"Q7GDF95MM7MZ7Y5Q","gpuMemory":"NA","vpcnetworkingsupport":"true","instanceType":"c5ad.2xlarge","tenancy":"Shared","usagetype":"AFS1-UnusedBox:c5ad.2xlarge","normalizationSizeFactor":"16","intelAvxAvailable":"No","processorFeatures":"AMD Turbo; AVX; AVX2","servicecode":"AmazonEC2","licenseModel":"No License required","currentGeneration":"Yes","preInstalledSw":"NA","location":"Africa (Cape Town)","processorArchitecture":"64-bit","marketoption":"OnDemand","operation":"RunInstances","availabilityzone":"NA"},"sku":"2257YY4K7BWZ4F46"},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"2257YY4K7BWZ4F46.JRTCKXETXF":{"priceDimensions":{"2257YY4K7BWZ4F46.JRTCKXETXF.6YS6EN2CT7":{"unit":"Hrs","endRange":"Inf","description":"$0.468 per Unused Reservation Linux c5ad.2xlarge Instance Hour","appliesTo":[],"rateCode":"2257YY4K7BWZ4F46.JRTCKXETXF.6YS6EN2CT7","beginRange":"0","pricePerUnit":{"USD":"0.4680000000"}}},"sku":"2257YY4K7BWZ4F46","effectiveDate":"2024-04-01T00:00:00Z","offerTermCode":"JRTCKXETXF","termAttributes":{}}}},"version":"20240508191027","publicationDate":"2024-05-08T19:10:27Z"}`,
					}, nil
				}).MinTimes(1)
		c.EXPECT().ListEBSVolumes(gomock.Any()).
			DoAndReturn(
				func(ctx context.Context) ([]ec2Types.Volume, error) {
					return nil, nil
				}).Times(1)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		collector, err := New(ctx, &Config{
			Regions:        regions,
			ScrapeInterval: time.Minute,
			AccountID:      "123456789012",
			RegionMap: map[string]client.Client{
				"us-east-1": c,
			},
		}, logger)
		require.NoError(t, err)

		ch := make(chan prometheus.Metric)
		go func() {
			if err := collector.Collect(t.Context(), ch); err != nil {
				assert.NoError(t, err)
			}
			close(ch)
		}()

		var metrics []*utils.MetricResult
		for metric := range ch {
			assert.NotNil(t, metric)
			metrics = append(metrics, utils.ReadMetrics(metric))
		}
		assert.Len(t, metrics, 6)
	})
}

func Test_PopulateStoragePricingMap(t *testing.T) {
	ctrl := gomock.NewController(t)
	tests := map[string]struct {
		ctx                 context.Context
		regions             []ec2Types.Region
		ListStoragePricesFn func(ctx context.Context, region string) ([]string, error)
		expectedCalls       int
		err                 error
		expected            map[string]*StoragePricing
	}{
		"can populate storage pricing map": {
			ctx: t.Context(),
			regions: []ec2Types.Region{
				{
					RegionName: aws.String("af-south-1"),
				},
			},
			ListStoragePricesFn: func(ctx context.Context, region string) ([]string, error) {
				return []string{
					`{"product":{"productFamily":"Storage","attributes":{"maxThroughputvolume":"1000 MiB/s","volumeType":"General Purpose","maxIopsvolume":"16000","usagetype":"AFS1-EBS:VolumeUsage.gp3","locationType":"AWS Region","maxVolumeSize":"16 TiB","storageMedia":"SSD-backed","regionCode":"af-south-1","servicecode":"AmazonEC2","volumeApiName":"gp3","location":"Africa (Cape Town)","servicename":"Amazon Elastic Compute Cloud","operation":""},"sku":"XWCTMRRUJM7TGYST"},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"XWCTMRRUJM7TGYST.JRTCKXETXF":{"priceDimensions":{"XWCTMRRUJM7TGYST.JRTCKXETXF.6YS6EN2CT7":{"unit":"GB-Mo","endRange":"Inf","description":"$0.1047 per GB-month of General Purpose (gp3) provisioned storage - Africa (Cape Town)","appliesTo":[],"rateCode":"XWCTMRRUJM7TGYST.JRTCKXETXF.6YS6EN2CT7","beginRange":"0","pricePerUnit":{"USD":"0.1047000000"}}},"sku":"XWCTMRRUJM7TGYST","effectiveDate":"2024-07-01T00:00:00Z","offerTermCode":"JRTCKXETXF","termAttributes":{}}}},"version":"20240705013454","publicationDate":"2024-07-05T01:34:54Z"}`,
				}, nil
			},
			expectedCalls: 1,
			expected: map[string]*StoragePricing{
				"af-south-1": {
					Storage: map[string]float64{
						"gp3": 0.1047,
					},
				},
			},
		},
		"errors listing storage prices propagate": {
			ctx: t.Context(),
			regions: []ec2Types.Region{{
				RegionName: aws.String("af-south-1"),
			}},
			ListStoragePricesFn: func(ctx context.Context, region string) ([]string, error) {
				return nil, assert.AnError
			},
			expectedCalls: 1,
			err:           ErrListStoragePrices,
			expected:      map[string]*StoragePricing{},
		},
		"errors generating the map from listed prices propagate too": {
			ctx: t.Context(),
			regions: []ec2Types.Region{
				{
					RegionName: aws.String("af-south-1"),
				},
			},
			ListStoragePricesFn: func(ctx context.Context, region string) ([]string, error) {
				return []string{
					"invalid json response",
				}, nil
			},
			expectedCalls: 1,
			expected:      map[string]*StoragePricing{},
			err:           ErrGeneratePricingMap,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			c := mock_client.NewMockClient(ctrl)
			c.EXPECT().
				ListStoragePrices(gomock.Any(), gomock.Any()).
				DoAndReturn(test.ListStoragePricesFn).
				Times(test.expectedCalls)

			spm := NewStoragePricingMap(logger, &Config{
				Regions: test.regions,
				RegionMap: map[string]client.Client{
					*test.regions[0].RegionName: c,
				},
			})

			err := spm.GenerateStoragePricingMap(test.ctx)
			if test.err != nil {
				assert.ErrorIs(t, err, test.err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, test.expected, spm.Regions)
		})
	}
}

// setupPricingExpectations sets up minimal pricing expectations on a mock client
// that are needed for EC2 collector initialization
func setupPricingExpectations(mockClient *mock_client.MockClient) {
	mockClient.EXPECT().
		ListOnDemandPrices(gomock.Any(), gomock.Any()).
		Return([]string{}, nil).
		AnyTimes()

	mockClient.EXPECT().
		ListSpotPrices(gomock.Any()).
		Return([]ec2Types.SpotPrice{}, nil).
		AnyTimes()

	mockClient.EXPECT().
		ListStoragePrices(gomock.Any(), gomock.Any()).
		Return([]string{}, nil).
		AnyTimes()
}

func Test_FetchVolumesData(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Run("sends EBS volumes data to channel", func(t *testing.T) {
		regionName := "af-south-1"
		region := ec2Types.Region{
			RegionName: aws.String(regionName),
		}

		c := mock_client.NewMockClient(ctrl)
		setupPricingExpectations(c)

		collector, err := New(context.Background(), &Config{
			Regions:        []ec2Types.Region{region},
			ScrapeInterval: time.Minute,
			AccountID:      "123456789012",
			RegionMap: map[string]client.Client{
				regionName: c,
			},
		}, logger)
		require.NoError(t, err)

		c.EXPECT().
			ListEBSVolumes(gomock.Any()).
			DoAndReturn(
				func(ctx context.Context) ([]ec2Types.Volume, error) {
					return []ec2Types.Volume{
						{
							VolumeId: aws.String("vol-111111111"),
						},
					}, nil
				},
			).
			Times(1)

		wg := sync.WaitGroup{}
		wg.Add(len(collector.regions))
		ch := make(chan []ec2Types.Volume)
		go collector.fetchVolumesData(t.Context(), c, regionName, ch)
		go func() {
			wg.Wait()
			close(ch)
		}()

		msg, ok := <-ch
		assert.True(t, ok)
		assert.NotNil(t, msg)
		assert.IsType(t, []ec2Types.Volume{}, msg)
	})
}

func Test_EmitMetricsFromVolumesChannel(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Run("reads from volumes channel and sends it over to prometheus channel", func(t *testing.T) {
		volumesCh := make(chan []ec2Types.Volume)
		promCh := make(chan prometheus.Metric)

		regionName := "af-south-1"
		region := ec2Types.Region{
			RegionName: aws.String(regionName),
		}
		volumeType := "gp3"

		c := mock_client.NewMockClient(ctrl)
		setupPricingExpectations(c)

		collector, err := New(context.Background(), &Config{
			Regions:        []ec2Types.Region{region},
			ScrapeInterval: time.Minute,
			AccountID:      "123456789012",
			RegionMap: map[string]client.Client{
				regionName: c,
			},
		}, logger)
		require.NoError(t, err)

		collector.storagePricingMap = &StoragePricingMap{
			Regions: map[string]*StoragePricing{
				regionName: {
					Storage: map[string]float64{
						volumeType: 0.1047,
					},
				},
			},
		}

		originMsg := []ec2Types.Volume{
			{
				AvailabilityZone: aws.String(fmt.Sprintf("%sa", regionName)),
				VolumeId:         aws.String("vol-111111111"),
				VolumeType:       ec2Types.VolumeType(volumeType),
				Size:             aws.Int32(100),
			},
		}

		go func() {
			collector.emitMetricsFromVolumesChannel(volumesCh, promCh)
		}()

		// fill volumes channel with data from the above volume
		volumesCh <- originMsg
		close(volumesCh)

		receivedMsg, ok := <-promCh
		close(promCh)

		assert.True(t, ok)
		assert.NotNil(t, receivedMsg)
		assert.Contains(t, receivedMsg.Desc().String(), "persistent_volume_usd_per_hour")
	})
}

func TestCollector_Collect_CapacityBlock(t *testing.T) {
	ctrl := gomock.NewController(t)
	regions := []ec2Types.Region{{RegionName: aws.String("us-east-1")}}

	start := time.Date(2026, 7, 10, 17, 16, 0, 0, time.UTC)
	end := start.Add(100 * time.Hour) // 100h block, count 1

	c := mock_client.NewMockClient(ctrl)
	// Compute/storage pricing so New() succeeds and InstanceDetails has m5.large
	// (needed to weight the capacity-block total into cpu/ram).
	c.EXPECT().ListSpotPrices(gomock.Any()).
		DoAndReturn(func(ctx context.Context) ([]ec2Types.SpotPrice, error) {
			return []ec2Types.SpotPrice{}, nil
		}).MinTimes(1)
	c.EXPECT().ListOnDemandPrices(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, region string) ([]string, error) {
			return []string{
				`{"product":{"productFamily":"Compute Instance","attributes":{"memory":"8 GiB","vcpu":"2","regionCode":"us-east-1","instanceFamily":"General purpose","operatingSystem":"Linux","instanceType":"m5.large","tenancy":"Shared","usagetype":"BoxUsage:m5.large","marketoption":"OnDemand"}},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"O.JRTCKXETXF":{"priceDimensions":{"O.JRTCKXETXF.6YS6EN2CT7":{"unit":"Hrs","pricePerUnit":{"USD":"0.0960000000"}}}}}}}`,
			}, nil
		}).MinTimes(1)
	c.EXPECT().ListStoragePrices(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, region string) ([]string, error) {
			return []string{}, nil
		}).MinTimes(1)
	// Capacity block inputs.
	c.EXPECT().ListActiveCapacityReservations(gomock.Any()).
		DoAndReturn(func(ctx context.Context) ([]ec2Types.CapacityReservation, error) {
			return []ec2Types.CapacityReservation{{
				CapacityReservationId: aws.String("cr-test"),
				InstanceType:          aws.String("m5.large"),
				AvailabilityZone:      aws.String("us-east-1a"),
				TotalInstanceCount:    aws.Int32(1),
				StartDate:             &start,
				EndDate:               &end,
			}}, nil
		}).MinTimes(1)
	c.EXPECT().GetCapacityBlockCosts(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, s, e time.Time) (*client.CapacityBlockCosts, error) {
			return &client.CapacityBlockCosts{Regions: map[string]map[string]float64{
				"us-east-1": {"m5.large": 1000.0},
			}}, nil
		}).MinTimes(1)
	c.EXPECT().ListComputeInstances(gomock.Any()).
		DoAndReturn(func(ctx context.Context) ([]ec2Types.Reservation, error) {
			return []ec2Types.Reservation{{Instances: []ec2Types.Instance{{
				InstanceId:            aws.String("i-cap"),
				InstanceType:          ec2Types.InstanceTypeM5Large,
				PrivateDnsName:        aws.String("ip-10-0-0-1.ec2.internal"),
				Placement:             &ec2Types.Placement{AvailabilityZone: aws.String("us-east-1a")},
				InstanceLifecycle:     ec2Types.InstanceLifecycleTypeCapacityBlock,
				CapacityReservationId: aws.String("cr-test"),
			}}}}, nil
		}).Times(1)
	c.EXPECT().ListEBSVolumes(gomock.Any()).
		DoAndReturn(func(ctx context.Context) ([]ec2Types.Volume, error) { return nil, nil }).Times(1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector, err := New(ctx, &Config{
		Regions:               regions,
		ScrapeInterval:        time.Minute,
		AccountID:             "123456789012",
		RegionMap:             map[string]client.Client{"us-east-1": c},
		CapacityBlocksEnabled: true,
	}, logger)
	require.NoError(t, err)

	ch := make(chan prometheus.Metric)
	go func() {
		require.NoError(t, collector.Collect(t.Context(), ch))
		close(ch)
	}()

	var total, gpu *utils.MetricResult
	for metric := range ch {
		m := utils.ReadMetrics(metric)
		switch m.FqName {
		case "cloudcost_aws_ec2_instance_total_usd_per_hour":
			total = m
		case "cloudcost_aws_ec2_instance_gpu_usd_per_gpu_hour":
			gpu = m
		}
	}
	require.NotNil(t, total, "expected a total cost metric for the capacity block instance")
	assert.Nil(t, gpu, "m5.large has no GPUs, so no gpu metric should be emitted")
	assert.Equal(t, "capacityblock", total.Labels["price_tier"])
	// 1000 USD / (1 instance * 100h) = 10 USD/instance-hour.
	assert.InDelta(t, 10.0, total.Value, 0.0001)
}

func TestCollector_Collect_GPU(t *testing.T) {
	ctrl := gomock.NewController(t)
	regions := []ec2Types.Region{{RegionName: aws.String("us-east-1")}}

	c := mock_client.NewMockClient(ctrl)
	c.EXPECT().ListSpotPrices(gomock.Any()).
		DoAndReturn(func(ctx context.Context) ([]ec2Types.SpotPrice, error) {
			return []ec2Types.SpotPrice{}, nil
		}).MinTimes(1)
	// A GPU instance: the pricing product carries gpu="8" for the 8 accelerators.
	// On-demand price is 8.0 USD/hr for round numbers.
	c.EXPECT().ListOnDemandPrices(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, region string) ([]string, error) {
			return []string{
				`{"product":{"productFamily":"Compute Instance","attributes":{"memory":"2048 GiB","vcpu":"192","gpu":"8","regionCode":"us-east-1","instanceFamily":"GPU instance","operatingSystem":"Linux","instanceType":"p5.48xlarge","tenancy":"Shared","usagetype":"BoxUsage:p5.48xlarge","marketoption":"OnDemand"}},"serviceCode":"AmazonEC2","terms":{"OnDemand":{"O.JRTCKXETXF":{"priceDimensions":{"O.JRTCKXETXF.6YS6EN2CT7":{"unit":"Hrs","pricePerUnit":{"USD":"8.0000000000"}}}}}}}`,
			}, nil
		}).MinTimes(1)
	c.EXPECT().ListStoragePrices(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, region string) ([]string, error) {
			return []string{}, nil
		}).MinTimes(1)
	c.EXPECT().ListComputeInstances(gomock.Any()).
		DoAndReturn(func(ctx context.Context) ([]ec2Types.Reservation, error) {
			return []ec2Types.Reservation{{Instances: []ec2Types.Instance{{
				InstanceId:     aws.String("i-gpu"),
				InstanceType:   ec2Types.InstanceType("p5.48xlarge"),
				PrivateDnsName: aws.String("ip-10-0-0-2.ec2.internal"),
				Placement:      &ec2Types.Placement{AvailabilityZone: aws.String("us-east-1a")},
			}}}}, nil
		}).Times(1)
	c.EXPECT().ListEBSVolumes(gomock.Any()).
		DoAndReturn(func(ctx context.Context) ([]ec2Types.Volume, error) { return nil, nil }).Times(1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector, err := New(ctx, &Config{
		Regions:        regions,
		ScrapeInterval: time.Minute,
		AccountID:      "123456789012",
		RegionMap:      map[string]client.Client{"us-east-1": c},
	}, logger)
	require.NoError(t, err)

	ch := make(chan prometheus.Metric)
	go func() {
		require.NoError(t, collector.Collect(t.Context(), ch))
		close(ch)
	}()

	metrics := map[string]*utils.MetricResult{}
	for metric := range ch {
		m := utils.ReadMetrics(metric)
		metrics[m.FqName] = m
	}

	gpu := metrics["cloudcost_aws_ec2_instance_gpu_usd_per_gpu_hour"]
	require.NotNil(t, gpu, "expected a gpu cost metric for the GPU instance")
	assert.Equal(t, "ondemand", gpu.Labels["price_tier"])
	assert.Equal(t, "p5.48xlarge", gpu.Labels["machine_type"])
	// gpuCostRatio (0.88) of the 8.0 total, spread over 8 GPUs = 0.88 USD/gpu-hour.
	assert.InDelta(t, 8.0*gpuCostRatio/8, gpu.Value, 1e-9)
	// Total is preserved regardless of the split.
	require.NotNil(t, metrics["cloudcost_aws_ec2_instance_total_usd_per_hour"])
	assert.InDelta(t, 8.0, metrics["cloudcost_aws_ec2_instance_total_usd_per_hour"].Value, 1e-9)
}
