package gke

import (
	"context"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
)

const (
	nodeRefreshInterval          = 5 * time.Minute
	nodePopulateConcurrencyLimit = 10
)

type NodeStore struct {
	logger    *slog.Logger
	gcpClient client.Client
	projects  []string

	mu    sync.RWMutex
	nodes map[string][]*client.MachineSpec

	initialPopulationOnce sync.Once
	initialPopulation     chan struct{}
}

func NewNodeStore(ctx context.Context, logger *slog.Logger, gcpClient client.Client, projects []string) *NodeStore {
	ns := &NodeStore{
		logger:            logger.With("store", "nodes"),
		gcpClient:         gcpClient,
		projects:          projects,
		nodes:             make(map[string][]*client.MachineSpec),
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
	return ns.nodes[project]
}

func (ns *NodeStore) Populate(ctx context.Context) {
	defer ns.initialPopulationOnce.Do(func() {
		close(ns.initialPopulation)
	})

	updates := make(map[string][]*client.MachineSpec)
	for _, project := range ns.projects {
		zones, err := ns.gcpClient.GetZones(project)
		if err != nil {
			ns.logger.LogAttrs(ctx, slog.LevelError, "failed to get zones",
				slog.String("project", project),
				slog.String("error", err.Error()))
			continue
		}

		sem := make(chan struct{}, nodePopulateConcurrencyLimit)
		var wg sync.WaitGroup
		var mu sync.Mutex
		var instances []*client.MachineSpec

		for _, zone := range zones {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				results, err := ns.gcpClient.ListInstancesInZone(project, zone.Name)
				if err != nil {
					ns.logger.LogAttrs(ctx, slog.LevelError, "failed to list instances in zone",
						slog.String("project", project),
						slog.String("zone", zone.Name),
						slog.String("error", err.Error()))
					return
				}
				mu.Lock()
				instances = append(instances, results...)
				mu.Unlock()
			}()
		}
		wg.Wait()
		updates[project] = instances
	}

	ns.mu.Lock()
	maps.Copy(ns.nodes, updates)
	ns.mu.Unlock()
}
