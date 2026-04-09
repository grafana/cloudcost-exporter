package eventhubs

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azmetrics"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v7"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v9"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/eventhub/armeventhub"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
)

func TestBuildNamespacePricingData(t *testing.T) {
	tests := []struct {
		name       string
		namespace   *armeventhub.EHNamespace
		want       namespacePricingData
		wantErrMsg string
	}{
		{
			name: "extracts supported namespace",
			namespace: newNamespace(
				"/subscriptions/test-sub/resourceGroups/test-rg/providers/Microsoft.EventHub/namespaces/test-namespace",
				"test-namespace",
				"eastus",
				1,
				nil,
			),
			want: namespacePricingData{
				region:        "eastus",
				namespaceName: "test-namespace",
				namespace:     "/subscriptions/test-sub/resourceGroups/test-rg/providers/Microsoft.EventHub/namespaces/test-namespace",
				sku:           "Standard",
				capacity:      1,
			},
		},
		{
			name:       "fails on nil namespace",
			wantErrMsg: "namespace is nil",
		},
		{
			name: "fails on premium namespace",
			namespace: &armeventhub.EHNamespace{
				ID:       to.Ptr("/subscriptions/test-sub/resourceGroups/test-rg/providers/Microsoft.EventHub/namespaces/test-namespace"),
				Name:     to.Ptr("test-namespace"),
				Location: to.Ptr("eastus"),
				SKU: &armeventhub.SKU{
					Name:     to.Ptr(armeventhub.SKUNamePremium),
					Capacity: to.Ptr(int32(1)),
				},
			},
			wantErrMsg: `SKU "Premium" is not supported`,
		},
		{
			name: "fails on auto-inflate namespace",
			namespace: newNamespace(
				"/subscriptions/test-sub/resourceGroups/test-rg/providers/Microsoft.EventHub/namespaces/test-namespace",
				"test-namespace",
				"eastus",
				1,
				&armeventhub.EHNamespaceProperties{IsAutoInflateEnabled: to.Ptr(true)},
			),
			wantErrMsg: "auto-inflate namespaces are not supported",
		},
		{
			name: "fails on explicit non-kafka namespace",
			namespace: newNamespace(
				"/subscriptions/test-sub/resourceGroups/test-rg/providers/Microsoft.EventHub/namespaces/test-namespace",
				"test-namespace",
				"eastus",
				1,
				&armeventhub.EHNamespaceProperties{KafkaEnabled: to.Ptr(false)},
			),
			wantErrMsg: "namespace is not Kafka-enabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildNamespacePricingData(tt.namespace)
			if tt.wantErrMsg != "" {
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
	namespaceID := "/subscriptions/test-sub/resourceGroups/test-rg/providers/Microsoft.EventHub/namespaces/test-namespace"
	ts1 := time.Date(2026, 4, 9, 10, 0, 0, 0, time.UTC)
	ts2 := ts1.Add(time.Minute)

	collector, err := New(t.Context(), &Config{
		Logger: testLogger(),
	}, &stubClient{
		namespaces: []*armeventhub.EHNamespace{
			newNamespace(namespaceID, "test-namespace", "eastus", 1, nil),
		},
		prices: []*retailPriceSdk.ResourceSKU{
			newPrice("Event Hubs", "Standard", "Standard Throughput Unit", "1 Hour", "eastus", 0.03),
			newPrice("Event Hubs", "Standard", "Standard Kafka Endpoint", "1 Hour", "eastus", 0.09),
			newPrice("Event Hubs", "Standard", "Standard Ingress Events", "1M", "eastus", 0.028),
			newPrice("Blob Storage", "Hot LRS", "Hot LRS Data Stored", "1 GB/Month", "eastus", 0.02),
		},
		metricsResponses: map[string]azmetrics.QueryResourcesResponse{
			metricsKey("eastus", []string{incomingBytesMetricName, incomingMessagesMetric}): incomingMetricsResponse(
				namespaceID,
				[]metricPoint{
					{timestamp: ts1, total: 500000},
					{timestamp: ts2, total: 0},
				},
				[]metricPoint{
					{timestamp: ts1, total: 25000000000},
					{timestamp: ts2, total: 16000000000},
				},
			),
			metricsKey("eastus", []string{sizeMetricName}): sizeMetricsResponse(
				namespaceID,
				[]metricPoint{
					{timestamp: ts1, average: 100 * bytesPerGB},
					{timestamp: ts2, average: 100 * bytesPerGB},
				},
			),
		},
	})
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 2)

	computeMetric := metricByName(results, "cloudcost_azure_eventhubs_compute_hourly_rate_usd_per_hour")
	require.NotNil(t, computeMetric)
	assert.Equal(t, "eastus", computeMetric.Labels["region"])
	assert.Equal(t, "test-namespace", computeMetric.Labels["namespace_name"])
	assert.Equal(t, namespaceID, computeMetric.Labels["namespace"])
	assert.Equal(t, "Standard", computeMetric.Labels["sku"])
	assert.InDelta(t, 0.141, computeMetric.Value, 0.0000001)

	storageMetric := metricByName(results, "cloudcost_azure_eventhubs_storage_hourly_rate_usd_per_hour")
	require.NotNil(t, storageMetric)
	assert.InDelta(t, (16*0.02)/utils.HoursInMonth, storageMetric.Value, 0.0000001)
}

func TestCollectorUsesGlobalEventHubsPricingFallback(t *testing.T) {
	namespaceID := "/subscriptions/test-sub/resourceGroups/test-rg/providers/Microsoft.EventHub/namespaces/test-namespace"

	collector, err := New(t.Context(), &Config{
		Logger: testLogger(),
	}, &stubClient{
		namespaces: []*armeventhub.EHNamespace{
			newNamespace(namespaceID, "test-namespace", "eastus", 1, nil),
		},
		prices: []*retailPriceSdk.ResourceSKU{
			newPrice("Event Hubs", "Standard", "Standard Throughput Unit", "1 Hour", "Global", 0.03),
			newPrice("Event Hubs", "Standard", "Standard Kafka Endpoint", "1 Hour", "Global", 0.09),
			newPrice("Event Hubs", "Standard", "Standard Ingress Events", "1M", "Global", 0.028),
			newPrice("Blob Storage", "Hot LRS", "Hot LRS Data Stored", "1 GB/Month", "eastus", 0.02),
		},
		metricsResponses: map[string]azmetrics.QueryResourcesResponse{
			metricsKey("eastus", []string{incomingBytesMetricName, incomingMessagesMetric}): incomingMetricsResponse(namespaceID, nil, nil),
			metricsKey("eastus", []string{sizeMetricName}):                             sizeMetricsResponse(namespaceID, nil),
		},
	})
	require.NoError(t, err)

	results, err := collectMetricResults(t, collector)
	require.NoError(t, err)
	require.Len(t, results, 2)

	computeMetric := metricByName(results, "cloudcost_azure_eventhubs_compute_hourly_rate_usd_per_hour")
	require.NotNil(t, computeMetric)
	assert.InDelta(t, 0.12, computeMetric.Value, 0.0000001)
}

type stubClient struct {
	namespaces      []*armeventhub.EHNamespace
	namespacesErr   error
	prices          []*retailPriceSdk.ResourceSKU
	pricesErr       error
	metricsResponses map[string]azmetrics.QueryResourcesResponse
	metricsErrs     map[string]error
}

func (s *stubClient) ListClustersInSubscription(context.Context) ([]*armcontainerservice.ManagedCluster, error) {
	return nil, nil
}

func (s *stubClient) ListVirtualMachineScaleSetsOwnedVms(context.Context, string, string) ([]*armcompute.VirtualMachineScaleSetVM, error) {
	return nil, nil
}

func (s *stubClient) ListVirtualMachineScaleSetsFromResourceGroup(context.Context, string) ([]*armcompute.VirtualMachineScaleSet, error) {
	return nil, nil
}

func (s *stubClient) ListMachineTypesByLocation(context.Context, string) ([]*armcompute.VirtualMachineSize, error) {
	return nil, nil
}

func (s *stubClient) ListEventHubNamespaces(context.Context) ([]*armeventhub.EHNamespace, error) {
	if s.namespacesErr != nil {
		return nil, s.namespacesErr
	}
	return s.namespaces, nil
}

func (s *stubClient) QueryResourceMetrics(_ context.Context, region string, _ string, metricNames []string, _ []string, _ *azmetrics.QueryResourcesOptions) (azmetrics.QueryResourcesResponse, error) {
	key := metricsKey(region, metricNames)
	if err := s.metricsErrs[key]; err != nil {
		return azmetrics.QueryResourcesResponse{}, err
	}
	return s.metricsResponses[key], nil
}

func (s *stubClient) ListDisksInSubscription(context.Context) ([]*armcompute.Disk, error) {
	return nil, nil
}

func (s *stubClient) ListPrices(context.Context, *retailPriceSdk.RetailPricesClientListOptions) ([]*retailPriceSdk.ResourceSKU, error) {
	if s.pricesErr != nil {
		return nil, s.pricesErr
	}
	return s.prices, nil
}

func newNamespace(id, name, location string, capacity int32, properties *armeventhub.EHNamespaceProperties) *armeventhub.EHNamespace {
	if properties == nil {
		properties = &armeventhub.EHNamespaceProperties{}
	}

	return &armeventhub.EHNamespace{
		ID:         to.Ptr(id),
		Name:       to.Ptr(name),
		Location:   to.Ptr(location),
		Properties: properties,
		SKU: &armeventhub.SKU{
			Name:     to.Ptr(armeventhub.SKUNameStandard),
			Capacity: to.Ptr(capacity),
		},
	}
}

func newPrice(productName, skuName, meterName, unit, region string, retailPrice float64) *retailPriceSdk.ResourceSKU {
	return &retailPriceSdk.ResourceSKU{
		ProductName:   productName,
		SkuName:       skuName,
		MeterName:     meterName,
		UnitOfMeasure: unit,
		ArmRegionName: region,
		RetailPrice:   retailPrice,
	}
}

type metricPoint struct {
	timestamp time.Time
	total     float64
	average   float64
}

func incomingMetricsResponse(resourceID string, messages []metricPoint, bytes []metricPoint) azmetrics.QueryResourcesResponse {
	return azmetrics.QueryResourcesResponse{
		MetricResults: azmetrics.MetricResults{
			Values: []azmetrics.MetricData{
				{
					ResourceID: to.Ptr(resourceID),
					Values: []azmetrics.Metric{
						{
							Name:       &azmetrics.LocalizableString{Value: to.Ptr(incomingMessagesMetric)},
							TimeSeries: []azmetrics.TimeSeriesElement{{Data: metricTotals(messages)}},
						},
						{
							Name:       &azmetrics.LocalizableString{Value: to.Ptr(incomingBytesMetricName)},
							TimeSeries: []azmetrics.TimeSeriesElement{{Data: metricTotals(bytes)}},
						},
					},
				},
			},
		},
	}
}

func sizeMetricsResponse(resourceID string, points []metricPoint) azmetrics.QueryResourcesResponse {
	return azmetrics.QueryResourcesResponse{
		MetricResults: azmetrics.MetricResults{
			Values: []azmetrics.MetricData{
				{
					ResourceID: to.Ptr(resourceID),
					Values: []azmetrics.Metric{
						{
							Name:       &azmetrics.LocalizableString{Value: to.Ptr(sizeMetricName)},
							TimeSeries: []azmetrics.TimeSeriesElement{{Data: metricAverages(points)}},
						},
					},
				},
			},
		},
	}
}

func metricTotals(points []metricPoint) []azmetrics.MetricValue {
	values := make([]azmetrics.MetricValue, 0, len(points))
	for _, point := range points {
		values = append(values, azmetrics.MetricValue{
			TimeStamp: to.Ptr(point.timestamp),
			Total:     to.Ptr(point.total),
		})
	}
	return values
}

func metricAverages(points []metricPoint) []azmetrics.MetricValue {
	values := make([]azmetrics.MetricValue, 0, len(points))
	for _, point := range points {
		values = append(values, azmetrics.MetricValue{
			TimeStamp: to.Ptr(point.timestamp),
			Average:   to.Ptr(point.average),
		})
	}
	return values
}

func metricsKey(region string, metricNames []string) string {
	return strings.ToLower(region) + "|" + strings.Join(metricNames, ",")
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

func TestCollectorCollectPropagatesRegionalUsageErrors(t *testing.T) {
	namespaceID := "/subscriptions/test-sub/resourceGroups/test-rg/providers/Microsoft.EventHub/namespaces/test-namespace"

	collector, err := New(t.Context(), &Config{
		Logger: testLogger(),
	}, &stubClient{
		namespaces: []*armeventhub.EHNamespace{
			newNamespace(namespaceID, "test-namespace", "eastus", 1, nil),
		},
		prices: []*retailPriceSdk.ResourceSKU{
			newPrice("Event Hubs", "Standard", "Standard Throughput Unit", "1 Hour", "eastus", 0.03),
			newPrice("Event Hubs", "Standard", "Standard Kafka Endpoint", "1 Hour", "eastus", 0.09),
			newPrice("Event Hubs", "Standard", "Standard Ingress Events", "1M", "eastus", 0.028),
			newPrice("Blob Storage", "Hot LRS", "Hot LRS Data Stored", "1 GB/Month", "eastus", 0.02),
		},
		metricsErrs: map[string]error{
			metricsKey("eastus", []string{incomingBytesMetricName, incomingMessagesMetric}): fmt.Errorf("boom"),
		},
	})
	require.NoError(t, err)

	_, err = collectMetricResults(t, collector)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to collect Event Hubs metrics for region eastus")
}
