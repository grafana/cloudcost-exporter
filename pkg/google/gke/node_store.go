package gke

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
)

const nodeRefreshInterval = 5 * time.Minute

type NodeStore struct {
	logger      *slog.Logger
	gcpClient   client.Client
	projects    []string
	concurrency int

	mu    sync.RWMutex
	nodes map[string]map[string][]*client.MachineSpec // project → zone → instances

	populating atomic.Bool

	initialPopulationOnce sync.Once
	initialPopulation     chan struct{}
}

// NewNodeStore returns a NodeStore that refreshes node inventory in the
// background. concurrency caps in-flight zone-level calls per populate;
// zero or negative values fall back to DefaultZoneCollectConcurrency.
func NewNodeStore(ctx context.Context, logger *slog.Logger, gcpClient client.Client, projects []string, concurrency int) *NodeStore {
	if concurrency <= 0 {
		concurrency = DefaultZoneCollectConcurrency
	}
	ns := &NodeStore{
		logger:            logger.With("store", "nodes"),
		gcpClient:         gcpClient,
		projects:          projects,
		concurrency:       concurrency,
		nodes:             make(map[string]map[string][]*client.MachineSpec),
		initialPopulation: make(chan struct{}),
	}
	go ns.Populate(ctx)
	return ns
}

func (ns *NodeStore) Done() <-chan struct{} {
	return ns.initialPopulation
}

func (ns *NodeStore) GetNodes(project string) []*client.MachineSpec {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	var all []*client.MachineSpec
	for _, instances := range ns.nodes[project] {
		all = append(all, instances...)
	}
	return all
}

func (ns *NodeStore) Populate(ctx context.Context) {
	// Drop overlapping populates: if a tick fires while the previous one is
	// still running (slow GCP / many projects), avoid doubling API load and
	// the last-writer race on ns.nodes.
	if !ns.populating.CompareAndSwap(false, true) {
		ns.logger.LogAttrs(ctx, slog.LevelInfo, "populate already in progress, skipping tick")
		return
	}
	defer ns.populating.Store(false)

	defer ns.initialPopulationOnce.Do(func() {
		close(ns.initialPopulation)
	})

	for _, project := range ns.projects {
		zones, err := ns.gcpClient.GetZones(project)
		if err != nil {
			ns.logger.LogAttrs(ctx, slog.LevelError, "failed to get zones",
				slog.String("project", project),
				slog.String("error", err.Error()))
			continue
		}

		sem := make(chan struct{}, ns.concurrency)
		var wg sync.WaitGroup

	zoneLoop:
		for _, zone := range zones {
			select {
			case <-ctx.Done():
				break zoneLoop
			case sem <- struct{}{}:
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				// Re-check: sem<- may have raced with cancellation.
				if ctx.Err() != nil {
					return
				}
				results, err := ns.gcpClient.ListInstancesInZone(project, zone.Name)
				if err != nil {
					ns.logger.LogAttrs(ctx, slog.LevelError, "failed to list instances in zone",
						slog.String("project", project),
						slog.String("zone", zone.Name),
						slog.String("error", err.Error()))
					return
				}
				ns.mu.Lock()
				if ns.nodes[project] == nil {
					ns.nodes[project] = make(map[string][]*client.MachineSpec)
				}
				ns.nodes[project][zone.Name] = results
				ns.mu.Unlock()
			}()
		}
		wg.Wait()
	}
}
