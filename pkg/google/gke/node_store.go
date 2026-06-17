package gke

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
)

const nodeRefreshInterval = 5 * time.Minute

type NodeStore struct {
	logger         *slog.Logger
	gcpClient      client.Client
	projects       []string
	concurrency    int
	populateErrors *prometheus.CounterVec

	mu    sync.RWMutex
	nodes map[string]map[string][]*client.MachineSpec // project → zone → instances

	populating atomic.Bool

	initialPopulationOnce sync.Once
	initialPopulation     chan struct{}
}

// NewNodeStore returns a NodeStore that refreshes node inventory in the
// background. concurrency caps in-flight zone-level calls per populate;
// zero or negative values fall back to DefaultZoneCollectConcurrency.
func NewNodeStore(ctx context.Context, logger *slog.Logger, gcpClient client.Client, projects []string, concurrency int, populateErrors *prometheus.CounterVec) *NodeStore {
	if concurrency <= 0 {
		concurrency = DefaultZoneCollectConcurrency
	}
	ns := &NodeStore{
		logger:            logger.With("store", "nodes"),
		gcpClient:         gcpClient,
		projects:          projects,
		concurrency:       concurrency,
		populateErrors:    populateErrors,
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

	var eg errgroup.Group
	eg.SetLimit(ns.concurrency)

	// Phase 1: resolve zones for every project in parallel.
	var mu sync.Mutex
	zonesByProject := make(map[string][]string)
	for _, project := range ns.projects {
		if ctx.Err() != nil {
			break
		}
		eg.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}
			zones, err := ns.gcpClient.GetZones(project)
			if err != nil {
				ns.logger.LogAttrs(ctx, slog.LevelError, "failed to get zones",
					slog.String("project", project),
					slog.String("error", err.Error()))
				ns.populateErrors.WithLabelValues("nodes", project, "get_zones").Inc()
				return nil // log and continue; don't drop sibling projects
			}
			names := make([]string, len(zones))
			for i, zone := range zones {
				names[i] = zone.Name
			}
			mu.Lock()
			zonesByProject[project] = names
			mu.Unlock()
			return nil
		})
	}
	eg.Wait() // errors are swallowed by design

	// Phase 2: list instances for every (project, zone) in parallel under the
	// same global concurrency limit.
	for project, zones := range zonesByProject {
		for _, zone := range zones {
			if ctx.Err() != nil {
				break
			}
			eg.Go(func() error {
				if ctx.Err() != nil {
					return nil
				}
				results, err := ns.gcpClient.ListInstancesInZone(project, zone)
				if err != nil {
					ns.logger.LogAttrs(ctx, slog.LevelError, "failed to list instances in zone",
						slog.String("project", project),
						slog.String("zone", zone),
						slog.String("error", err.Error()))
					ns.populateErrors.WithLabelValues("nodes", project, "list_instances").Inc()
					return nil // log and continue; don't abort sibling zones
				}
				ns.mu.Lock()
				if ns.nodes[project] == nil {
					ns.nodes[project] = make(map[string][]*client.MachineSpec)
				}
				ns.nodes[project][zone] = results
				ns.mu.Unlock()
				return nil
			})
		}
	}
	eg.Wait()
}
