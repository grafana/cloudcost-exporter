package gke

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

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

	var eg errgroup.Group
	eg.SetLimit(ds.concurrency)

	// Phase 1: resolve zones for every project in parallel.
	var mu sync.Mutex
	zonesByProject := make(map[string][]string)
	for _, project := range ds.projects {
		if ctx.Err() != nil {
			break
		}
		eg.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}
			zones, err := ds.gcpClient.GetZones(project)
			if err != nil {
				ds.logger.LogAttrs(ctx, slog.LevelError, "failed to get zones",
					slog.String("project", project),
					slog.String("error", err.Error()))
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
	eg.Wait()

	// Phase 2: list disks for every (project, zone) in parallel under the
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
				results, err := ds.gcpClient.ListDisks(ctx, project, zone)
				if err != nil {
					ds.logger.LogAttrs(ctx, slog.LevelError, "failed to list disks in zone",
						slog.String("project", project),
						slog.String("zone", zone),
						slog.String("error", err.Error()))
					return nil // log and continue; don't abort sibling zones
				}
				zonedDisks := make([]*Disk, 0, len(results))
				for _, raw := range results {
					zonedDisks = append(zonedDisks, NewDisk(raw, project))
				}
				ds.mu.Lock()
				if ds.disks[project] == nil {
					ds.disks[project] = make(map[string][]*Disk)
				}
				ds.disks[project][zone] = zonedDisks
				ds.mu.Unlock()
				return nil
			})
		}
	}
	eg.Wait()
}
