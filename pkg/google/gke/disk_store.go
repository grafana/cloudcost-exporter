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
	diskRefreshInterval          = 15 * time.Minute
	diskPopulateConcurrencyLimit = 10
)

type DiskStore struct {
	logger    *slog.Logger
	gcpClient client.Client
	projects  []string

	mu    sync.RWMutex
	disks map[string][]*Disk

	initialPopulationOnce sync.Once
	initialPopulation     chan struct{}
}

func NewDiskStore(ctx context.Context, logger *slog.Logger, gcpClient client.Client, projects []string) *DiskStore {
	ds := &DiskStore{
		logger:            logger.With("store", "disks"),
		gcpClient:         gcpClient,
		projects:          projects,
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

		sem := make(chan struct{}, diskPopulateConcurrencyLimit)
		var wg sync.WaitGroup
		var mu sync.Mutex
		seen := make(map[string]bool)
		var disks []*Disk
		successCount := 0

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
				successCount++
				mu.Unlock()
			}()
		}
		wg.Wait()

		if successCount == 0 && len(zones) > 0 {
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
