package vpc

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"google.golang.org/api/compute/v1"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
)

func TestNew(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Mock GCP client for testing
	mockClient := &mockGCPClient{}

	config := &Config{
		Projects:       "test-project-1,test-project-2",
		ScrapeInterval: 5 * time.Minute,
		Logger:         logger,
	}

	collector, err := New(t.Context(), config, mockClient)
	if err != nil {
		t.Fatalf("Failed to create VPC collector: %v", err)
	}

	if collector == nil {
		t.Fatal("Expected collector to be created, got nil")
	}

	if collector.Name() != "VPC" {
		t.Errorf("Expected collector name to be 'VPC', got '%s'", collector.Name())
	}

	if len(collector.projects) != 2 {
		t.Errorf("Expected 2 projects, got %d", len(collector.projects))
	}

	expectedProjects := []string{"test-project-1", "test-project-2"}
	for i, project := range collector.projects {
		if project != expectedProjects[i] {
			t.Errorf("Expected project %s, got %s", expectedProjects[i], project)
		}
	}
}

func TestVPCPricingMapErrors(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mockClient := &mockGCPClient{}

	pricingMap := NewVPCPricingMap(logger, mockClient)

	// Test that errors are returned when no pricing data is available
	testCases := []struct {
		name     string
		testFunc func(string) (float64, error)
		region   string
	}{
		{"CloudNATGatewayHourly", pricingMap.GetCloudNATGatewayHourlyRate, "us-central1"},
		{"CloudNATDataProcessing", pricingMap.GetCloudNATDataProcessingRate, "us-central1"},
		{"VPNGateway", pricingMap.GetVPNGatewayHourlyRate, "us-central1"},
		{"PrivateServiceConnectDataProcessing", pricingMap.GetPrivateServiceConnectDataProcessingRate, "us-central1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.testFunc(tc.region)
			if err == nil {
				t.Errorf("Expected error for %s when no pricing data available, got nil", tc.name)
			}
		})
	}

	// Test Private Service Connect endpoint rates separately (returns map)
	t.Run("PrivateServiceConnectEndpoints", func(t *testing.T) {
		_, err := pricingMap.GetPrivateServiceConnectEndpointRates("us-central1")
		if err == nil {
			t.Error("Expected error for Private Service Connect endpoints when no pricing data available, got nil")
		}
	})
}

// mockGCPClient implements the client.Client interface for testing
type mockGCPClient struct{}

func (m *mockGCPClient) GetServiceName(ctx context.Context, serviceName string) (string, error) {
	return "services/" + serviceName, nil
}

func (m *mockGCPClient) GetPricing(ctx context.Context, serviceName string) []*billingpb.Sku {
	return []*billingpb.Sku{} // Return empty SKUs for testing
}

func (m *mockGCPClient) ExportRegionalDiscounts(ctx context.Context, m2 *metrics.Metrics) error {
	return nil
}

func (m *mockGCPClient) ExportGCPCostData(ctx context.Context, serviceName string, m2 *metrics.Metrics) float64 {
	return 1.0
}

func (m *mockGCPClient) ExportBucketInfo(ctx context.Context, projects []string, m2 *metrics.Metrics) error {
	return nil
}

func (m *mockGCPClient) GetZones(projectId string) ([]*compute.Zone, error) {
	return nil, nil
}

func (m *mockGCPClient) GetRegions(projectId string) ([]*compute.Region, error) {
	// Return mock regions for testing
	return []*compute.Region{
		{Name: "us-central1"},
		{Name: "us-east1"},
		{Name: "europe-west1"},
	}, nil
}

func (m *mockGCPClient) ListInstancesInZone(projectId, zone string) ([]*client.MachineSpec, error) {
	return nil, nil
}

func (m *mockGCPClient) ListDisks(ctx context.Context, projectId string, zone string) ([]*compute.Disk, error) {
	return nil, nil
}

func (m *mockGCPClient) ListForwardingRules(ctx context.Context, projectId string, region string) ([]*compute.ForwardingRule, error) {
	return nil, nil
}

func (m *mockGCPClient) ListSQLInstances(ctx context.Context, projectId string) ([]*sqladmin.DatabaseInstance, error) {
	return nil, nil
}
