package gke

import (
	"context"
	"log/slog"
	"maps"
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
	disks map[string][]*Disk

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
		disks:             make(map[string][]*Disk),
		initialPopulation: make(chan struct{}),
	}
	go ds.Populate(ctx)
	return ds
}

func (ds *DiskStore) Done() <-chan struct{} {
	return ds.initialPopulation
}

func (ds *DiskStore) GetDisks(project string) []*Disk {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.disks[project]
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

	updates := make(map[string][]*Disk)
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
		var mu sync.Mutex
		seen := make(map[string]bool)
		var disks []*Disk
		successfulZones := 0

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
				mu.Lock()
				for _, raw := range results {
					d := NewDisk(raw, project)
					if seen[d.Name()] {
						continue
					}
					seen[d.Name()] = true
					disks = append(disks, d)
				}
				successfulZones++
				mu.Unlock()
			}()
		}
		wg.Wait()

		// ctx.Err() != nil means we bailed for shutdown, not a real total-failure.
		if ctx.Err() == nil && successfulZones == 0 && len(zones) > 0 {
			ds.logger.LogAttrs(ctx, slog.LevelError, "all zone listings failed, wiping cached data",
				slog.String("project", project),
				slog.Int("zones", len(zones)))
		}
		updates[project] = disks
	}

	ds.mu.Lock()
	maps.Copy(ds.disks, updates)
	ds.mu.Unlock()
}
