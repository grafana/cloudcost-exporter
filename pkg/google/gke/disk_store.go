package gke

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
)

const diskRefreshInterval = 15 * time.Minute

type DiskStore struct {
	logger      *slog.Logger
	gcpClient   client.Client
	projects    []string
	concurrency int

	mu    sync.RWMutex
	disks map[string]map[string][]*Disk // project → zone → disks

	populating atomic.Bool

	initialPopulationOnce sync.Once
	initialPopulation     chan struct{}
}

// NewDiskStore returns a DiskStore that refreshes disk inventory in the
// background. concurrency caps in-flight zone-level calls per populate;
// zero or negative values fall back to DefaultZoneCollectConcurrency.
func NewDiskStore(ctx context.Context, logger *slog.Logger, gcpClient client.Client, projects []string, concurrency int) *DiskStore {
	if concurrency <= 0 {
		concurrency = DefaultZoneCollectConcurrency
	}
	ds := &DiskStore{
		logger:            logger.With("store", "disks"),
		gcpClient:         gcpClient,
		projects:          projects,
		concurrency:       concurrency,
		disks:             make(map[string]map[string][]*Disk),
		initialPopulation: make(chan struct{}),
	}
	go ds.Populate(ctx)
	return ds
}

func (ds *DiskStore) Done() <-chan struct{} {
	return ds.initialPopulation
}

// GetDisks returns all cached disks for a project, deduplicated by name across zones.
func (ds *DiskStore) GetDisks(project string) []*Disk {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	seen := make(map[string]bool)
	var all []*Disk
	for _, zonedDisks := range ds.disks[project] {
		for _, d := range zonedDisks {
			if seen[d.Name()] {
				continue
			}
			seen[d.Name()] = true
			all = append(all, d)
		}
	}
	return all
}

func (ds *DiskStore) Populate(ctx context.Context) {
	// Drop overlapping populates: if a tick fires while the previous one is
	// still running (slow GCP / many projects), avoid doubling API load and
	// the last-writer race on ds.disks.
	if !ds.populating.CompareAndSwap(false, true) {
		ds.logger.LogAttrs(ctx, slog.LevelInfo, "populate already in progress, skipping tick")
		return
	}
	defer ds.populating.Store(false)

	defer ds.initialPopulationOnce.Do(func() {
		close(ds.initialPopulation)
	})

	for _, project := range ds.projects {
		zones, err := ds.gcpClient.GetZones(project)
		if err != nil {
			ds.logger.LogAttrs(ctx, slog.LevelError, "failed to get zones",
				slog.String("project", project),
				slog.String("error", err.Error()))
			continue
		}

		sem := make(chan struct{}, ds.concurrency)
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
				results, err := ds.gcpClient.ListDisks(ctx, project, zone.Name)
				if err != nil {
					ds.logger.LogAttrs(ctx, slog.LevelError, "failed to list disks in zone",
						slog.String("project", project),
						slog.String("zone", zone.Name),
						slog.String("error", err.Error()))
					return
				}
				zonedDisks := make([]*Disk, 0, len(results))
				for _, raw := range results {
					zonedDisks = append(zonedDisks, NewDisk(raw, project))
				}
				ds.mu.Lock()
				if ds.disks[project] == nil {
					ds.disks[project] = make(map[string][]*Disk)
				}
				ds.disks[project][zone.Name] = zonedDisks
				ds.mu.Unlock()
			}()
		}
		wg.Wait()
	}
}
