package gce

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/google/gke"
)

// errNilMachineType guards against a client returning a nil *compute.MachineType
// alongside a nil error, which would otherwise panic on field access below.
var errNilMachineType = errors.New("machine type response was nil")

// machineTypeSpec is the subset of a GCE machine type's spec needed to turn
// per-core/per-GiB rates into a total hourly cost.
type machineTypeSpec struct {
	VCPU      int64
	MemoryGiB float64
}

// machineTypeCache resolves (project, zone, machine type) tuples to their vCPU/memory
// spec, warmed in the background so Collect never makes a live GCP call. A machine
// type's spec is immutable for a given zone, so entries are cached indefinitely; a
// fetch failure just leaves the key uncached for the next warm to retry.
type machineTypeCache struct {
	gcpClient      client.Client
	populateErrors *prometheus.CounterVec
	concurrency    int
	logger         *slog.Logger

	mu    sync.RWMutex
	specs map[string]machineTypeSpec

	initialWarmOnce sync.Once
	initialWarm     chan struct{}
}

func newMachineTypeCache(gcpClient client.Client, populateErrors *prometheus.CounterVec, concurrency int, logger *slog.Logger) *machineTypeCache {
	if concurrency <= 0 {
		concurrency = gke.DefaultZoneCollectConcurrency
	}
	return &machineTypeCache{
		gcpClient:      gcpClient,
		populateErrors: populateErrors,
		concurrency:    concurrency,
		logger:         logger.With("store", "machine_types"),
		specs:          make(map[string]machineTypeSpec),
		initialWarm:    make(chan struct{}),
	}
}

// Done closes once the first call to warm has finished, mirroring
// gke.NodeStore.Done(). It doesn't guarantee every key resolved, only that
// the initial attempt completed, same semantics as the node store.
func (c *machineTypeCache) Done() <-chan struct{} {
	return c.initialWarm
}

func machineTypeCacheKey(project, zone, machineType string) string {
	return project + "/" + zone + "/" + machineType
}

// get returns the cached spec for a machine type, if resolved.
func (c *machineTypeCache) get(project, zone, machineType string) (machineTypeSpec, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	spec, ok := c.specs[machineTypeCacheKey(project, zone, machineType)]
	return spec, ok
}

type machineTypeKey struct {
	project, zone, machineType string
}

// warm fetches specs for any (project, zone, machine type) tuple present in nodeStore
// that isn't already cached, capped at c.concurrency in-flight calls.
func (c *machineTypeCache) warm(ctx context.Context, nodeStore *gke.NodeStore, projects []string) {
	defer c.initialWarmOnce.Do(func() { close(c.initialWarm) })

	seen := make(map[machineTypeKey]struct{})
	var missing []machineTypeKey
	for _, project := range projects {
		for _, instance := range nodeStore.GetNodes(project) {
			k := machineTypeKey{project, instance.Zone, instance.MachineType}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			if _, cached := c.get(k.project, k.zone, k.machineType); !cached {
				missing = append(missing, k)
			}
		}
	}

	var eg errgroup.Group
	eg.SetLimit(c.concurrency)
	for _, k := range missing {
		if ctx.Err() != nil {
			break
		}
		eg.Go(func() error {
			if ctx.Err() != nil {
				return nil
			}
			mt, err := c.gcpClient.GetMachineType(k.project, k.zone, k.machineType)
			if err == nil && mt == nil {
				err = errNilMachineType
			}
			if err != nil {
				c.logger.LogAttrs(ctx, slog.LevelError, "failed to get machine type",
					slog.String("project", k.project),
					slog.String("zone", k.zone),
					slog.String("machine_type", k.machineType),
					slog.String("error", err.Error()))
				c.populateErrors.WithLabelValues("machine_types", k.project, "get_machine_type").Inc()
				return nil
			}
			c.mu.Lock()
			c.specs[machineTypeCacheKey(k.project, k.zone, k.machineType)] = machineTypeSpec{
				VCPU:      mt.GuestCpus,
				MemoryGiB: float64(mt.MemoryMb) / 1024,
			}
			c.mu.Unlock()
			return nil
		})
	}
	eg.Wait()
}
