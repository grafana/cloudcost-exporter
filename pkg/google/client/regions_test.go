package client

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"testing"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"google.golang.org/api/compute/v1"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
)

// regionsStubClient implements only GetRegions and GetZones for testing.
type regionsStubClient struct {
	regionsByProject map[string][]*compute.Region
	regionErrors     map[string]error
	zonesByProject   map[string][]*compute.Zone
	zoneErrors       map[string]error
}

func (c *regionsStubClient) GetRegions(project string) ([]*compute.Region, error) {
	if err, ok := c.regionErrors[project]; ok {
		return nil, err
	}
	return c.regionsByProject[project], nil
}

func (c *regionsStubClient) GetZones(project string) ([]*compute.Zone, error) {
	if err, ok := c.zoneErrors[project]; ok {
		return nil, err
	}
	return c.zonesByProject[project], nil
}

func (c *regionsStubClient) GetServiceName(_ context.Context, _ string) (string, error) {
	panic("not implemented")
}
func (c *regionsStubClient) ExportRegionalDiscounts(_ context.Context, _ *metrics.Metrics) error {
	panic("not implemented")
}
func (c *regionsStubClient) ExportGCPCostData(_ context.Context, _ string, _ *metrics.Metrics) float64 {
	panic("not implemented")
}
func (c *regionsStubClient) ExportBucketInfo(_ context.Context, _ []string, _ *metrics.Metrics) error {
	panic("not implemented")
}
func (c *regionsStubClient) GetPricing(_ context.Context, _ string) []*billingpb.Sku {
	panic("not implemented")
}
func (c *regionsStubClient) ListInstancesInZone(_, _ string) ([]*MachineSpec, error) {
	panic("not implemented")
}
func (c *regionsStubClient) ListDisks(_ context.Context, _, _ string) ([]*compute.Disk, error) {
	panic("not implemented")
}
func (c *regionsStubClient) ListForwardingRules(_ context.Context, _, _ string) ([]*compute.ForwardingRule, error) {
	panic("not implemented")
}
func (c *regionsStubClient) ListSQLInstances(_ context.Context, _ string) ([]*sqladmin.DatabaseInstance, error) {
	panic("not implemented")
}

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func TestRegionsForProjects(t *testing.T) {
	tests := map[string]struct {
		client   *regionsStubClient
		projects []string
		want     []string
	}{
		"no projects": {
			client:   &regionsStubClient{},
			projects: []string{},
			want:     []string{},
		},
		"single project with regions": {
			client: &regionsStubClient{
				regionsByProject: map[string][]*compute.Region{
					"proj-a": {{Name: "us-central1"}, {Name: "europe-west1"}},
				},
			},
			projects: []string{"proj-a"},
			want:     []string{"europe-west1", "us-central1"},
		},
		"multiple projects with overlapping regions": {
			client: &regionsStubClient{
				regionsByProject: map[string][]*compute.Region{
					"proj-a": {{Name: "us-central1"}, {Name: "europe-west1"}},
					"proj-b": {{Name: "us-central1"}, {Name: "asia-east1"}},
				},
			},
			projects: []string{"proj-a", "proj-b"},
			want:     []string{"asia-east1", "europe-west1", "us-central1"},
		},
		"project that errors is skipped": {
			client: &regionsStubClient{
				regionsByProject: map[string][]*compute.Region{
					"proj-b": {{Name: "us-central1"}},
				},
				regionErrors: map[string]error{
					"proj-a": fmt.Errorf("permission denied"),
				},
			},
			projects: []string{"proj-a", "proj-b"},
			want:     []string{"us-central1"},
		},
		"all projects error returns empty": {
			client: &regionsStubClient{
				regionErrors: map[string]error{
					"proj-a": fmt.Errorf("not found"),
					"proj-b": fmt.Errorf("not found"),
				},
			},
			projects: []string{"proj-a", "proj-b"},
			want:     []string{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := RegionsForProjects(tt.client, tt.projects, testLogger)
			slices.Sort(got)
			if len(got) != len(tt.want) {
				t.Fatalf("RegionsForProjects() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("RegionsForProjects()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRegionsFromZonesForProjects(t *testing.T) {
	tests := map[string]struct {
		client   *regionsStubClient
		projects []string
		want     []string
	}{
		"no projects": {
			client:   &regionsStubClient{},
			projects: []string{},
			want:     []string{},
		},
		"zones are stripped to region names": {
			client: &regionsStubClient{
				zonesByProject: map[string][]*compute.Zone{
					"proj-a": {{Name: "us-central1-a"}, {Name: "us-central1-b"}, {Name: "europe-west1-c"}},
				},
			},
			projects: []string{"proj-a"},
			want:     []string{"europe-west1", "us-central1"},
		},
		"zones across multiple projects are deduplicated": {
			client: &regionsStubClient{
				zonesByProject: map[string][]*compute.Zone{
					"proj-a": {{Name: "us-central1-a"}},
					"proj-b": {{Name: "us-central1-b"}, {Name: "asia-east1-a"}},
				},
			},
			projects: []string{"proj-a", "proj-b"},
			want:     []string{"asia-east1", "us-central1"},
		},
		"zones with fewer than 3 parts are skipped": {
			client: &regionsStubClient{
				zonesByProject: map[string][]*compute.Zone{
					"proj-a": {{Name: "us"}, {Name: "us-central1"}, {Name: "us-central1-a"}},
				},
			},
			projects: []string{"proj-a"},
			want:     []string{"us-central1"},
		},
		"project that errors is skipped": {
			client: &regionsStubClient{
				zonesByProject: map[string][]*compute.Zone{
					"proj-b": {{Name: "us-central1-a"}},
				},
				zoneErrors: map[string]error{
					"proj-a": fmt.Errorf("permission denied"),
				},
			},
			projects: []string{"proj-a", "proj-b"},
			want:     []string{"us-central1"},
		},
		"all projects error returns empty": {
			client: &regionsStubClient{
				zoneErrors: map[string]error{
					"proj-a": fmt.Errorf("not found"),
				},
			},
			projects: []string{"proj-a"},
			want:     []string{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := RegionsFromZonesForProjects(tt.client, tt.projects, testLogger)
			slices.Sort(got)
			if len(got) != len(tt.want) {
				t.Fatalf("RegionsFromZonesForProjects() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("RegionsFromZonesForProjects()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
