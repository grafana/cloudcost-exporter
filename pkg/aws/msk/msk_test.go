package msk

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	msktypes "github.com/aws/aws-sdk-go-v2/service/kafka/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	mockclient "github.com/grafana/cloudcost-exporter/pkg/aws/client/mocks"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestBuildClusterPricingData(t *testing.T) {
	tests := []struct {
		name       string
		cluster    msktypes.Cluster
		want       clusterPricingData
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:    "extracts supported provisioned cluster",
			cluster: newProvisionedCluster("test-cluster", "arn:aws:kafka:us-east-1:123456789012:cluster/test-cluster", "kafka.m5.large", 3, 100),
			want: clusterPricingData{
				clusterName:   "test-cluster",
				clusterARN:    "arn:aws:kafka:us-east-1:123456789012:cluster/test-cluster",
				instanceType:  "kafka.m5.large",
				brokerCount:   3,
				volumeSizeGiB: 100,
			},
		},
		{
			name: "skips serverless clusters",
			cluster: msktypes.Cluster{
				ClusterType: msktypes.ClusterTypeServerless,
				ClusterArn:  aws.String("arn:aws:kafka:us-east-1:123456789012:cluster/serverless"),
				ClusterName: aws.String("serverless"),
			},
			wantErr:    true,
			wantErrMsg: "not supported",
		},
		{
			name: "skips missing provisioned data",
			cluster: msktypes.Cluster{
				ClusterType: msktypes.ClusterTypeProvisioned,
				ClusterArn:  aws.String("arn:aws:kafka:us-east-1:123456789012:cluster/missing"),
				ClusterName: aws.String("missing"),
			},
			wantErr:    true,
			wantErrMsg: "missing provisioned data",
		},
		{
			name: "skips tiered storage",
			cluster: func() msktypes.Cluster {
				cluster := newProvisionedCluster("tiered", "arn:aws:kafka:us-east-1:123456789012:cluster/tiered", "kafka.m5.large", 3, 100)
				cluster.Provisioned.StorageMode = msktypes.StorageModeTiered
				return cluster
			}(),
			wantErr:    true,
			wantErrMsg: "tiered storage",
		},
		{
			name: "skips provisioned throughput",
			cluster: func() msktypes.Cluster {
				cluster := newProvisionedCluster("throughput", "arn:aws:kafka:us-east-1:123456789012:cluster/throughput", "kafka.m5.large", 3, 100)
				cluster.Provisioned.BrokerNodeGroupInfo.StorageInfo.EbsStorageInfo.ProvisionedThroughput = &msktypes.ProvisionedThroughput{
					Enabled: aws.Bool(true),
				}
				return cluster
			}(),
			wantErr:    true,
			wantErrMsg: "provisioned throughput",
		},
		{
			name:       "skips express brokers",
			cluster:    newProvisionedCluster("express", "arn:aws:kafka:us-east-1:123456789012:cluster/express", "express.m7g.large", 3, 100),
			wantErr:    true,
			wantErrMsg: "express brokers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildClusterPricingData(tt.cluster)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCollectorCollectEmitsHourlyRateMetrics(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	regionClient := mockclient.NewMockClient(ctrl)
	pricingClient := mockclient.NewMockClient(ctrl)

	cluster := newProvisionedCluster(
		"test-cluster",
		"arn:aws:kafka:us-east-1:123456789012:cluster/test-cluster",
		"kafka.m5.large",
		3,
		100,
	)

	regionClient.EXPECT().ListMSKClusters(gomock.Any()).Return([]msktypes.Cluster{cluster}, nil).Times(1)
	expectPricingLoad(pricingClient, "us-east-1", "USE1", "0.2100000000", "0.1000000000")

	collector := New(t.Context(), &Config{
		Regions:   []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		RegionMap: map[string]client.Client{"us-east-1": regionClient},
		Client:    pricingClient,
		Logger:    testLogger(),
	})

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 2)

	computeMetric := metricByName(results, "cloudcost_aws_msk_compute_hourly_rate_usd_per_hour")
	require.NotNil(t, computeMetric)
	assert.Equal(t, "us-east-1", computeMetric.Labels["region"])
	assert.Equal(t, "test-cluster", computeMetric.Labels["cluster_name"])
	assert.Equal(t, "arn:aws:kafka:us-east-1:123456789012:cluster/test-cluster", computeMetric.Labels["cluster_arn"])
	assert.Equal(t, "kafka.m5.large", computeMetric.Labels["instance_type"])
	assert.InDelta(t, 0.63, computeMetric.Value, 0.000001)

	storageMetric := metricByName(results, "cloudcost_aws_msk_storage_hourly_rate_usd_per_hour")
	require.NotNil(t, storageMetric)
	assert.Equal(t, "us-east-1", storageMetric.Labels["region"])
	assert.Equal(t, "test-cluster", storageMetric.Labels["cluster_name"])
	assert.Equal(t, "arn:aws:kafka:us-east-1:123456789012:cluster/test-cluster", storageMetric.Labels["cluster_arn"])
	assert.InDelta(t, (0.1/730.5)*300, storageMetric.Value, 0.000001)
}

func TestCollectorCollectCachesPricingLookups(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	regionClient := mockclient.NewMockClient(ctrl)
	pricingClient := mockclient.NewMockClient(ctrl)

	clusters := []msktypes.Cluster{
		newProvisionedCluster("cluster-a", "arn:aws:kafka:us-east-1:123456789012:cluster/cluster-a", "kafka.m5.large", 3, 100),
		newProvisionedCluster("cluster-b", "arn:aws:kafka:us-east-1:123456789012:cluster/cluster-b", "kafka.m5.large", 6, 200),
	}

	regionClient.EXPECT().ListMSKClusters(gomock.Any()).Return(clusters, nil).Times(1)
	expectPricingLoad(pricingClient, "us-east-1", "USE1", "0.2100000000", "0.1000000000")

	collector := New(t.Context(), &Config{
		Regions:   []ec2types.Region{{RegionName: aws.String("us-east-1")}},
		RegionMap: map[string]client.Client{"us-east-1": regionClient},
		Client:    pricingClient,
		Logger:    testLogger(),
	})

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	assert.Len(t, results, 4)
}

func TestCollectorCollectContinuesWhenRegionClientMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	regionClient := mockclient.NewMockClient(ctrl)
	pricingClient := mockclient.NewMockClient(ctrl)
	cluster := newProvisionedCluster("test-cluster", "arn:aws:kafka:us-west-2:123456789012:cluster/test-cluster", "kafka.m5.large", 3, 100)

	regionClient.EXPECT().ListMSKClusters(gomock.Any()).Return([]msktypes.Cluster{cluster}, nil).Times(1)
	expectPricingLoad(pricingClient, "us-east-1", "USE1", "0.2100000000", "0.1000000000")
	expectPricingLoad(pricingClient, "us-west-2", "USW2", "0.2100000000", "0.1000000000")

	collector := New(t.Context(), &Config{
		Regions: []ec2types.Region{
			{RegionName: aws.String("us-east-1")},
			{RegionName: aws.String("us-west-2")},
		},
		RegionMap: map[string]client.Client{
			"us-west-2": regionClient,
		},
		Client: pricingClient,
		Logger: testLogger(),
	})

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestCollectorCollectContinuesWhenRegionListingFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	failingRegionClient := mockclient.NewMockClient(ctrl)
	healthyRegionClient := mockclient.NewMockClient(ctrl)
	pricingClient := mockclient.NewMockClient(ctrl)
	cluster := newProvisionedCluster("test-cluster", "arn:aws:kafka:us-west-2:123456789012:cluster/test-cluster", "kafka.m5.large", 3, 100)

	failingRegionClient.EXPECT().ListMSKClusters(gomock.Any()).Return(nil, fmt.Errorf("boom")).Times(1)
	healthyRegionClient.EXPECT().ListMSKClusters(gomock.Any()).Return([]msktypes.Cluster{cluster}, nil).Times(1)
	expectPricingLoad(pricingClient, "us-east-1", "USE1", "0.2100000000", "0.1000000000")
	expectPricingLoad(pricingClient, "us-west-2", "USW2", "0.2100000000", "0.1000000000")

	collector := New(t.Context(), &Config{
		Regions: []ec2types.Region{
			{RegionName: aws.String("us-east-1")},
			{RegionName: aws.String("us-west-2")},
		},
		RegionMap: map[string]client.Client{
			"us-east-1": failingRegionClient,
			"us-west-2": healthyRegionClient,
		},
		Client: pricingClient,
		Logger: testLogger(),
	})

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func newProvisionedCluster(name, arn, instanceType string, brokerCount, volumeSizeGiB int32) msktypes.Cluster {
	return msktypes.Cluster{
		ClusterArn:  aws.String(arn),
		ClusterName: aws.String(name),
		ClusterType: msktypes.ClusterTypeProvisioned,
		Provisioned: &msktypes.Provisioned{
			NumberOfBrokerNodes: aws.Int32(brokerCount),
			StorageMode:         msktypes.StorageModeLocal,
			BrokerNodeGroupInfo: &msktypes.BrokerNodeGroupInfo{
				InstanceType: aws.String(instanceType),
				StorageInfo: &msktypes.StorageInfo{
					EbsStorageInfo: &msktypes.EBSStorageInfo{
						VolumeSize: aws.Int32(volumeSizeGiB),
					},
				},
			},
		},
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func expectPricingLoad(pricingClient *mockclient.MockClient, region, usageTypePrefix, brokerPrice, storagePrice string) {
	pricingClient.EXPECT().
		ListMSKServicePrices(gomock.Any(), region, gomock.Any()).
		Return([]string{priceJSON(region, usageTypePrefix+"-Kafka.m5.large", brokerPrice)}, nil).
		Times(1)
	pricingClient.EXPECT().
		ListMSKServicePrices(gomock.Any(), region, gomock.Any()).
		Return([]string{priceJSON(region, usageTypePrefix+"-Kafka.Storage.GP2", storagePrice)}, nil).
		Times(1)
}

func priceJSON(region, usageType, price string) string {
	return fmt.Sprintf(`{"product":{"attributes":{"usagetype":"%s","regionCode":"%s"}},"terms":{"OnDemand":{"term1":{"priceDimensions":{"dim1":{"pricePerUnit":{"USD":"%s"}}}}}}}`, usageType, region, price)
}

func collectMetricResults(t *testing.T, collector *Collector) ([]*utils.MetricResult, error) {
	t.Helper()

	ch := make(chan prometheus.Metric, 10)
	err := collector.Collect(t.Context(), ch)
	close(ch)

	var results []*utils.MetricResult
	for metric := range ch {
		results = append(results, utils.ReadMetrics(metric))
	}

	return results, err
}

func metricByName(results []*utils.MetricResult, fqName string) *utils.MetricResult {
	for _, result := range results {
		if result.FqName == fqName {
			return result
		}
	}

	return nil
}
