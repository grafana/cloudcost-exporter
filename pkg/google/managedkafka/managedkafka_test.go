package managedkafka

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"cloud.google.com/go/billing/apiv1/billingpb"
	managedkafkapb "cloud.google.com/go/managedkafka/apiv1/managedkafkapb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	gcmetrics "github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/compute/v1"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
	"google.golang.org/genproto/googleapis/type/money"
)

func TestBuildClusterPricingData(t *testing.T) {
	tests := []struct {
		name       string
		cluster    *managedkafkapb.Cluster
		want       clusterPricingData
		wantErr    bool
		wantErrMsg string
	}{
		{
			name:    "extracts supported cluster",
			cluster: newCluster("projects/test-project/locations/us-central1/clusters/test-cluster", 6, 24),
			want: clusterPricingData{
				project:     "test-project",
				region:      "us-central1",
				clusterName: "test-cluster",
				cluster:     "projects/test-project/locations/us-central1/clusters/test-cluster",
				vcpuCount:   6,
				memoryGiB:   24,
			},
		},
		{
			name:       "fails on nil cluster",
			cluster:    nil,
			wantErr:    true,
			wantErrMsg: "cluster is nil",
		},
		{
			name:       "fails on missing cluster resource name",
			cluster:    &managedkafkapb.Cluster{},
			wantErr:    true,
			wantErrMsg: "cluster name is missing",
		},
		{
			name: "fails on missing location",
			cluster: &managedkafkapb.Cluster{
				Name: "projects/test-project/clusters/test-cluster",
				CapacityConfig: &managedkafkapb.CapacityConfig{
					VcpuCount:   3,
					MemoryBytes: 3 * bytesPerGiB,
				},
			},
			wantErr:    true,
			wantErrMsg: "location missing",
		},
		{
			name: "fails on missing capacity config",
			cluster: &managedkafkapb.Cluster{
				Name: "projects/test-project/locations/us-central1/clusters/test-cluster",
			},
			wantErr:    true,
			wantErrMsg: "capacity config is missing",
		},
		{
			name: "fails on invalid vcpu count",
			cluster: &managedkafkapb.Cluster{
				Name: "projects/test-project/locations/us-central1/clusters/test-cluster",
				CapacityConfig: &managedkafkapb.CapacityConfig{
					MemoryBytes: 3 * bytesPerGiB,
				},
			},
			wantErr:    true,
			wantErrMsg: "vcpu count is missing",
		},
		{
			name: "fails on invalid memory bytes",
			cluster: &managedkafkapb.Cluster{
				Name: "projects/test-project/locations/us-central1/clusters/test-cluster",
				CapacityConfig: &managedkafkapb.CapacityConfig{
					VcpuCount: 3,
				},
			},
			wantErr:    true,
			wantErrMsg: "memory bytes are missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildClusterPricingData("test-project", tt.cluster)
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

func TestNewFailsIfInitialPricingFetchFails(t *testing.T) {
	_, err := New(t.Context(), &Config{
		Projects: "test-project",
	}, testLogger(), &stubClient{
		serviceNameErr: fmt.Errorf("billing API unavailable"),
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to initialise Managed Kafka pricing")
}

func TestNewFailsIfPricingContainsMultipleComputePricesForRegion(t *testing.T) {
	_, err := New(t.Context(), &Config{
		Projects: "test-project",
	}, testLogger(), &stubClient{
		serviceName: "services/managed-kafka",
		skus: []*billingpb.Sku{
			newSKU("Managed Service for Apache Kafka CPU+RAM", "us-central1", 0, 90000000, ""),
			newSKUWithUsage("Data Compute Units in us-central1", "us-central1", 0, 91000000, "h", "hour", ""),
		},
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "multiple compute prices found for region us-central1")
}

func TestNewAllowsDuplicateComputePriceForRegionWhenValuesMatch(t *testing.T) {
	_, err := New(t.Context(), &Config{
		Projects: "test-project",
	}, testLogger(), &stubClient{
		serviceName: "services/managed-kafka",
		skus: []*billingpb.Sku{
			newSKU("Managed Service for Apache Kafka CPU+RAM", "us-central1", 0, 90000000, ""),
			newSKUWithUsage("Data Compute Units in us-central1", "us-central1", 0, 90000000, "h", "hour", ""),
			newSKUWithUsage("Local Storage in us-central1", "us-central1", 0, 170000000, "GiBy.mo", "gibibyte month", ""),
		},
	})

	require.NoError(t, err)
}

func TestCollectorCollectEmitsHourlyRateMetrics(t *testing.T) {
	gcpClient := &stubClient{
		serviceName: "services/managed-kafka",
		skus: []*billingpb.Sku{
			newSKU("Managed Service for Apache Kafka CPU+RAM", "us-central1", 0, 90000000, ""),
			newSKU("Managed Service for Apache Kafka CPU+RAM", "us-central1", 0, 72000000, "Managed Service for Apache Kafka CUD - 1 Year"),
			newSKU("Managed Service for Apache Kafka Connect CPU+RAM", "us-central1", 0, 120000000, ""),
			newSKU("Managed Service for Apache Kafka Local Storage", "us-central1", 0, 232877, ""),
			newSKU("Managed Service for Apache Kafka Long term storage", "us-central1", 0, 136986, ""),
		},
		locations: map[string][]string{
			"test-project": {"us-central1"},
		},
		clusters: map[string][]*managedkafkapb.Cluster{
			clusterKey("test-project", "us-central1"): {
				newCluster("projects/test-project/locations/us-central1/clusters/test-cluster", 6, 24),
			},
		},
	}

	collector, err := New(t.Context(), &Config{
		Projects: "test-project",
	}, testLogger(), gcpClient)
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 2)

	computeMetric := metricByName(results, "cloudcost_gcp_managedkafka_compute_hourly_rate_usd_per_hour")
	require.NotNil(t, computeMetric)
	assert.Equal(t, "test-project", computeMetric.Labels["project"])
	assert.Equal(t, "us-central1", computeMetric.Labels["region"])
	assert.Equal(t, "test-cluster", computeMetric.Labels["cluster_name"])
	assert.Equal(t, "projects/test-project/locations/us-central1/clusters/test-cluster", computeMetric.Labels["cluster"])
	assert.InDelta(t, 0.54, computeMetric.Value, 0.000001)

	storageMetric := metricByName(results, "cloudcost_gcp_managedkafka_storage_hourly_rate_usd_per_hour")
	require.NotNil(t, storageMetric)
	assert.Equal(t, "test-project", storageMetric.Labels["project"])
	assert.Equal(t, "us-central1", storageMetric.Labels["region"])
	assert.Equal(t, "test-cluster", storageMetric.Labels["cluster_name"])
	assert.Equal(t, "projects/test-project/locations/us-central1/clusters/test-cluster", storageMetric.Labels["cluster"])
	assert.InDelta(t, 0.1397262, storageMetric.Value, 0.0000001)
}

func TestCollectorCollectEmitsHourlyRateMetricsFromCurrentBillingCatalogSKUs(t *testing.T) {
	gcpClient := &stubClient{
		serviceName: "services/managed-kafka",
		skus: []*billingpb.Sku{
			newSKUWithUsage("Data Compute Units in us-central1", "us-central1", 0, 90000000, "h", "hour", ""),
			newSKUWithUsage("Managed Kafka Connect Data Compute Units in us-central1", "us-central1", 0, 120000000, "h", "hour", ""),
			newSKUWithUsage("Local Storage in us-central1", "us-central1", 0, 170000000, "GiBy.mo", "gibibyte month", ""),
			newSKUWithUsage("Long Term Regional Storage in us-central1", "us-central1", 0, 100000000, "GiBy.mo", "gibibyte month", ""),
		},
		locations: map[string][]string{
			"test-project": {"us-central1"},
		},
		clusters: map[string][]*managedkafkapb.Cluster{
			clusterKey("test-project", "us-central1"): {
				newCluster("projects/test-project/locations/us-central1/clusters/test-cluster", 6, 24),
			},
		},
	}

	collector, err := New(t.Context(), &Config{
		Projects: "test-project",
	}, testLogger(), gcpClient)
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 2)

	computeMetric := metricByName(results, "cloudcost_gcp_managedkafka_compute_hourly_rate_usd_per_hour")
	require.NotNil(t, computeMetric)
	assert.InDelta(t, 0.54, computeMetric.Value, 0.000001)

	storageMetric := metricByName(results, "cloudcost_gcp_managedkafka_storage_hourly_rate_usd_per_hour")
	require.NotNil(t, storageMetric)
	assert.InDelta(t, (0.17/utils.HoursInMonth)*600, storageMetric.Value, 0.0000001)
}

func TestCollectorCollectContinuesWhenLocationListingFails(t *testing.T) {
	gcpClient := &stubClient{
		serviceName: "services/managed-kafka",
		skus: []*billingpb.Sku{
			newSKU("Managed Service for Apache Kafka CPU+RAM", "us-central1", 0, 90000000, ""),
			newSKU("Managed Service for Apache Kafka Local Storage", "us-central1", 0, 232877, ""),
		},
		locations: map[string][]string{
			"test-project": {"us-central1", "us-east1"},
		},
		clusterErrs: map[string]error{
			clusterKey("test-project", "us-east1"): fmt.Errorf("boom"),
		},
		clusters: map[string][]*managedkafkapb.Cluster{
			clusterKey("test-project", "us-central1"): {
				newCluster("projects/test-project/locations/us-central1/clusters/test-cluster", 3, 12),
			},
		},
	}

	collector, err := New(t.Context(), &Config{
		Projects: "test-project",
	}, testLogger(), gcpClient)
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

type stubClient struct {
	serviceName    string
	serviceNameErr error
	skus           []*billingpb.Sku
	locations      map[string][]string
	locationErrs   map[string]error
	clusters       map[string][]*managedkafkapb.Cluster
	clusterErrs    map[string]error
}

func (s *stubClient) GetServiceName(context.Context, string) (string, error) {
	if s.serviceNameErr != nil {
		return "", s.serviceNameErr
	}
	return s.serviceName, nil
}

func (s *stubClient) ExportRegionalDiscounts(context.Context, *gcmetrics.Metrics) error {
	return nil
}

func (s *stubClient) ExportGCPCostData(context.Context, string, *gcmetrics.Metrics) float64 {
	return 0
}

func (s *stubClient) ExportBucketInfo(context.Context, []string, *gcmetrics.Metrics) error {
	return nil
}

func (s *stubClient) GetPricing(context.Context, string) []*billingpb.Sku {
	return s.skus
}

func (s *stubClient) GetZones(string) ([]*compute.Zone, error) {
	return nil, nil
}

func (s *stubClient) GetRegions(string) ([]*compute.Region, error) {
	return nil, nil
}

func (s *stubClient) ListInstancesInZone(string, string) ([]*client.MachineSpec, error) {
	return nil, nil
}

func (s *stubClient) ListDisks(context.Context, string, string) ([]*compute.Disk, error) {
	return nil, nil
}

func (s *stubClient) ListForwardingRules(context.Context, string, string) ([]*compute.ForwardingRule, error) {
	return nil, nil
}

func (s *stubClient) ListSQLInstances(context.Context, string) ([]*sqladmin.DatabaseInstance, error) {
	return nil, nil
}

func (s *stubClient) ListManagedKafkaLocations(_ context.Context, project string) ([]string, error) {
	if err := s.locationErrs[project]; err != nil {
		return nil, err
	}
	return s.locations[project], nil
}

func (s *stubClient) ListManagedKafkaClusters(_ context.Context, project string, location string) ([]*managedkafkapb.Cluster, error) {
	key := clusterKey(project, location)
	if err := s.clusterErrs[key]; err != nil {
		return nil, err
	}
	return s.clusters[key], nil
}

func newCluster(name string, vcpuCount, memoryGiB int64) *managedkafkapb.Cluster {
	return &managedkafkapb.Cluster{
		Name: name,
		CapacityConfig: &managedkafkapb.CapacityConfig{
			VcpuCount:   vcpuCount,
			MemoryBytes: memoryGiB * bytesPerGiB,
		},
	}
}

func newSKU(description, region string, units int64, nanos int32, summary string) *billingpb.Sku {
	return newSKUWithUsage(description, region, units, nanos, "", "", summary)
}

func newSKUWithUsage(description, region string, units int64, nanos int32, usageUnit, usageUnitDescription, summary string) *billingpb.Sku {
	return &billingpb.Sku{
		Description:    description,
		ServiceRegions: []string{region},
		PricingInfo: []*billingpb.PricingInfo{
			{
				Summary: summary,
				PricingExpression: &billingpb.PricingExpression{
					UsageUnit:            usageUnit,
					UsageUnitDescription: usageUnitDescription,
					TieredRates: []*billingpb.PricingExpression_TierRate{
						{
							UnitPrice: &money.Money{
								Units: units,
								Nanos: nanos,
							},
						},
					},
				},
			},
		},
	}
}

func clusterKey(project, location string) string {
	return project + "|" + location
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func collectMetricResults(t *testing.T, collector *Collector) ([]*utils.MetricResult, error) {
	t.Helper()

	ch := make(chan prometheus.Metric, 10)
	if err := collector.Collect(t.Context(), ch); err != nil {
		close(ch)
		return nil, err
	}
	close(ch)

	results := make([]*utils.MetricResult, 0)
	for metric := range ch {
		result := utils.ReadMetrics(metric)
		if result != nil {
			results = append(results, result)
		}
	}

	return results, nil
}

func metricByName(metrics []*utils.MetricResult, fqName string) *utils.MetricResult {
	for _, metric := range metrics {
		if metric.FqName == fqName {
			return metric
		}
	}
	return nil
}
