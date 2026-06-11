package gke

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	computev1 "google.golang.org/api/compute/v1"
)

// benchConfig is the simulated GCP environment for one benchmark case.
type benchConfig struct {
	projects        int
	zonesPerProject int
	getZonesLatency time.Duration // one round-trip per project
	listLatency     time.Duration // one round-trip per zone
}

// latencyGCPClient injects a fixed latency per call so wall-clock time reflects
// the call-scheduling strategy rather than CPU work. It returns no data; the
// benchmark measures orchestration, not unmarshalling.
type latencyGCPClient struct {
	client.Client

	zones          []*computev1.Zone
	getZonesDelay  time.Duration
	listZonesDelay time.Duration
}

func (c *latencyGCPClient) GetZones(_ string) ([]*computev1.Zone, error) {
	time.Sleep(c.getZonesDelay)
	return c.zones, nil
}

func (c *latencyGCPClient) ListInstancesInZone(_, _ string) ([]*client.MachineSpec, error) {
	time.Sleep(c.listZonesDelay)
	return nil, nil
}

func (c *latencyGCPClient) ListDisks(_ context.Context, _, _ string) ([]*computev1.Disk, error) {
	time.Sleep(c.listZonesDelay)
	return nil, nil
}

func newLatencyClient(cfg benchConfig) *latencyGCPClient {
	zones := make([]*computev1.Zone, cfg.zonesPerProject)
	for i := range zones {
		zones[i] = &computev1.Zone{Name: fmt.Sprintf("zone-%d", i)}
	}
	return &latencyGCPClient{
		zones:          zones,
		getZonesDelay:  cfg.getZonesLatency,
		listZonesDelay: cfg.listLatency,
	}
}

func newBenchNodeStore(cfg benchConfig) *NodeStore {
	projects := make([]string, cfg.projects)
	for i := range projects {
		projects[i] = fmt.Sprintf("project-%d", i)
	}
	return &NodeStore{
		logger:            logger.With("store", "nodes"),
		gcpClient:         newLatencyClient(cfg),
		projects:          projects,
		concurrency:       DefaultZoneCollectConcurrency,
		nodes:             make(map[string]map[string][]*client.MachineSpec),
		initialPopulation: make(chan struct{}),
	}
}

// BenchmarkNodeStore_Populate sweeps a full initial population across a matrix
// of project counts, zones per project, and per-call latencies. GetZones fans
// out across projects, then a single global concurrency limit covers every
// (project, zone) pair.
func BenchmarkNodeStore_Populate(b *testing.B) {
	projectCounts := []int{5, 10, 20}
	zoneCounts := []int{20, 50}
	getZonesLatencies := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond}
	listLatencies := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond}

	ctx := context.Background()
	for _, projects := range projectCounts {
		for _, zones := range zoneCounts {
			for _, getZones := range getZonesLatencies {
				for _, list := range listLatencies {
					cfg := benchConfig{
						projects:        projects,
						zonesPerProject: zones,
						getZonesLatency: getZones,
						listLatency:     list,
					}
					name := fmt.Sprintf("projects=%d/zones=%d/getzones=%s/list=%s",
						projects, zones, getZones, list)
					b.Run(name, func(b *testing.B) {
						for b.Loop() {
							ns := newBenchNodeStore(cfg)
							ns.Populate(ctx)
						}
					})
				}
			}
		}
	}
}
