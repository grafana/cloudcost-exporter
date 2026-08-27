package gce

import (
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	computev1 "google.golang.org/api/compute/v1"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/google/gke"
)

var cacheTestLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))

// fakeNodeStoreClient provides a fixed set of zones/instances so a real
// gke.NodeStore can be populated without hitting a live GCP API.
type fakeNodeStoreClient struct {
	client.Client
	zones     []*computev1.Zone
	instances []*computev1.Instance
}

func (f *fakeNodeStoreClient) GetZones(string) ([]*computev1.Zone, error) {
	return f.zones, nil
}

func (f *fakeNodeStoreClient) ListInstancesInZone(string, string) ([]*client.MachineSpec, error) {
	specs := make([]*client.MachineSpec, len(f.instances))
	for i, inst := range f.instances {
		specs[i] = client.NewMachineSpec(inst)
	}
	return specs, nil
}

func newTestNodeStore(t *testing.T, instances []*computev1.Instance) *gke.NodeStore {
	t.Helper()
	fakeClient := &fakeNodeStoreClient{
		zones:     []*computev1.Zone{{Name: "us-central1-a"}},
		instances: instances,
	}
	populateErrors := newPopulateErrorsCounter()
	ns := gke.NewNodeStore(t.Context(), cacheTestLogger, fakeClient, []string{"testing"}, 5, populateErrors)
	<-ns.Done()
	return ns
}

// fakeMachineTypeClient controls GetMachineType behavior per (project, zone,
// machineType) key: configurable failure count before success, plus
// concurrency tracking.
type fakeMachineTypeClient struct {
	client.Client

	mu                 sync.Mutex
	failuresRemaining  map[string]int
	calls              map[string]int
	currentConcurrency int
	peakConcurrency    int
}

func newFakeMachineTypeClient(failuresRemaining map[string]int) *fakeMachineTypeClient {
	return &fakeMachineTypeClient{
		failuresRemaining: failuresRemaining,
		calls:             map[string]int{},
	}
}

func (f *fakeMachineTypeClient) GetMachineType(project, zone, machineType string) (*computev1.MachineType, error) {
	key := machineTypeCacheKey(project, zone, machineType)

	f.mu.Lock()
	f.calls[key]++
	f.currentConcurrency++
	if f.currentConcurrency > f.peakConcurrency {
		f.peakConcurrency = f.currentConcurrency
	}
	f.mu.Unlock()

	time.Sleep(5 * time.Millisecond)

	f.mu.Lock()
	f.currentConcurrency--
	remaining := f.failuresRemaining[key]
	if remaining > 0 {
		f.failuresRemaining[key] = remaining - 1
	}
	f.mu.Unlock()

	if remaining > 0 {
		return nil, errors.New("transient error")
	}
	return &computev1.MachineType{GuestCpus: 4, MemoryMb: 8192}, nil
}

func newTestCache(gcpClient client.Client, concurrency int) *machineTypeCache {
	return newMachineTypeCache(gcpClient, newPopulateErrorsCounter(), concurrency, cacheTestLogger)
}

func TestMachineTypeCache_GetHitMiss(t *testing.T) {
	cache := newTestCache(newFakeMachineTypeClient(nil), 5)

	_, ok := cache.get("proj", "us-central1-a", "n1-standard-1")
	assert.False(t, ok, "expected a miss before warming")

	cache.mu.Lock()
	cache.specs[machineTypeCacheKey("proj", "us-central1-a", "n1-standard-1")] = machineTypeSpec{VCPU: 1, MemoryGiB: 3.75}
	cache.mu.Unlock()

	spec, ok := cache.get("proj", "us-central1-a", "n1-standard-1")
	require.True(t, ok)
	assert.Equal(t, machineTypeSpec{VCPU: 1, MemoryGiB: 3.75}, spec)
}

func TestMachineTypeCache_Warm_PopulatesFromNodeStore(t *testing.T) {
	instances := []*computev1.Instance{
		{Name: "a", MachineType: "abc/n1-slim", Zone: "testing/us-central1-a", Scheduling: &computev1.Scheduling{}},
		{Name: "b", MachineType: "abc/n2-slim", Zone: "testing/us-central1-a", Scheduling: &computev1.Scheduling{}},
	}
	nodeStore := newTestNodeStore(t, instances)
	gcpClient := newFakeMachineTypeClient(nil)
	cache := newTestCache(gcpClient, 5)

	cache.warm(t.Context(), nodeStore, []string{"testing"})

	spec, ok := cache.get("testing", "us-central1-a", "n1-slim")
	require.True(t, ok)
	assert.Equal(t, machineTypeSpec{VCPU: 4, MemoryGiB: 8}, spec)

	spec, ok = cache.get("testing", "us-central1-a", "n2-slim")
	require.True(t, ok)
	assert.Equal(t, machineTypeSpec{VCPU: 4, MemoryGiB: 8}, spec)

	select {
	case <-cache.Done():
	default:
		t.Fatal("expected Done() to be closed after the first warm")
	}
}

func TestMachineTypeCache_Warm_RetriesPreviouslyFailedKeys(t *testing.T) {
	instances := []*computev1.Instance{
		{Name: "a", MachineType: "abc/n1-slim", Zone: "testing/us-central1-a", Scheduling: &computev1.Scheduling{}},
	}
	nodeStore := newTestNodeStore(t, instances)
	key := machineTypeCacheKey("testing", "us-central1-a", "n1-slim")
	gcpClient := newFakeMachineTypeClient(map[string]int{key: 1}) // fails once, then succeeds
	cache := newTestCache(gcpClient, 5)

	cache.warm(t.Context(), nodeStore, []string{"testing"})
	_, ok := cache.get("testing", "us-central1-a", "n1-slim")
	assert.False(t, ok, "expected the key to remain uncached after a failed attempt")

	cache.warm(t.Context(), nodeStore, []string{"testing"})
	spec, ok := cache.get("testing", "us-central1-a", "n1-slim")
	require.True(t, ok, "expected the retried key to resolve on the next warm")
	assert.Equal(t, machineTypeSpec{VCPU: 4, MemoryGiB: 8}, spec)
}

func TestMachineTypeCache_Warm_RespectsConcurrencyLimit(t *testing.T) {
	var instances []*computev1.Instance
	for i := 0; i < 10; i++ {
		instances = append(instances, &computev1.Instance{
			Name:        "instance",
			MachineType: "abc/machine-" + string(rune('a'+i)),
			Zone:        "testing/us-central1-a",
			Scheduling:  &computev1.Scheduling{},
		})
	}
	nodeStore := newTestNodeStore(t, instances)
	gcpClient := newFakeMachineTypeClient(nil)
	const concurrency = 3
	cache := newTestCache(gcpClient, concurrency)

	cache.warm(t.Context(), nodeStore, []string{"testing"})

	gcpClient.mu.Lock()
	peak := gcpClient.peakConcurrency
	gcpClient.mu.Unlock()
	assert.LessOrEqual(t, peak, concurrency)
}

func TestCollector_Register(t *testing.T) {
	c := &Collector{populateErrors: newPopulateErrorsCounter()}
	registry := prometheus.NewRegistry()
	require.NoError(t, c.Register(registry))
}
