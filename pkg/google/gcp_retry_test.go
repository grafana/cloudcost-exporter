package google

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCollector emits one metric from Collect and registers one gauge, like the real collectors do.
type fakeCollector struct {
	name          string
	registerErr   error
	registerPanic bool
}

func (f *fakeCollector) collectedName() string { return "fake_collected_" + f.name }

func (f *fakeCollector) Register(r provider.Registry) error {
	if f.registerPanic {
		panic("duplicate metrics collector registration attempted")
	}
	if f.registerErr != nil {
		return f.registerErr
	}
	r.MustRegister(prometheus.NewGauge(prometheus.GaugeOpts{Name: "fake_registered_" + f.name}))
	return nil
}

func (f *fakeCollector) Collect(_ context.Context, ch chan<- prometheus.Metric) error {
	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc(f.collectedName(), "help", nil, nil), prometheus.GaugeValue, 1)
	return nil
}

func (f *fakeCollector) Describe(chan<- *prometheus.Desc) error { return nil }
func (f *fakeCollector) Name() string                           { return f.name }

// testRetryInitial and testRetryMax shorten the collector retry backoff for tests. They are passed per
// provider, through Config or the GCP fields, so a retry goroutine that outlives its test cannot read a
// value another test is writing.
const (
	testRetryInitial = time.Millisecond
	testRetryMax     = 5 * time.Millisecond
)

func newRegisteredGCP(t *testing.T, ctx context.Context, pendingServices ...string) (*GCP, *prometheus.Registry) {
	t.Helper()
	g := &GCP{
		logger:           logger,
		ctx:              ctx,
		collectorTimeout: time.Second,
		retryInitial:     testRetryInitial,
		retryMax:         testRetryMax,
	}
	for _, s := range pendingServices {
		g.markPending(s)
	}
	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(g))
	require.NoError(t, g.RegisterCollectors(reg))
	return g, reg
}

func gatherByName(t *testing.T, reg *prometheus.Registry) map[string]*dto.MetricFamily {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	byName := make(map[string]*dto.MetricFamily, len(mfs))
	for _, mf := range mfs {
		byName[mf.GetName()] = mf
	}
	return byName
}

// lastScrapeError returns collector_last_scrape_error for a collector label, and whether the series exists.
func lastScrapeError(mfs map[string]*dto.MetricFamily, collector string) (float64, bool) {
	mf, ok := mfs["cloudcost_exporter_collector_last_scrape_error"]
	if !ok {
		return 0, false
	}
	for _, m := range mf.GetMetric() {
		for _, l := range m.GetLabel() {
			if l.GetName() == "collector" && l.GetValue() == collector {
				return m.GetCounter().GetValue(), true
			}
		}
	}
	return 0, false
}

func TestGCP_PendingCollectorIsReportedAsError(t *testing.T) {
	_, reg := newRegisteredGCP(t, context.Background(), serviceGKE)

	value, ok := lastScrapeError(gatherByName(t, reg), "gcp_gke")

	require.True(t, ok, "a collector that failed to create must still produce a series")
	assert.Equal(t, 1.0, value)
}

// All three collector_* metrics must carry the same label set whether the collector exists or not,
// so a query joining them cannot silently drop the one that is broken.
func TestGCP_PendingCollectorEmitsTheWholeMetricFamily(t *testing.T) {
	_, reg := newRegisteredGCP(t, context.Background(), serviceGKE)
	mfs := gatherByName(t, reg)

	for _, name := range []string{
		"cloudcost_exporter_collector_last_scrape_error",
		"cloudcost_exporter_collector_last_scrape_duration_seconds",
		"cloudcost_exporter_collector_last_scrape_time",
	} {
		mf, ok := mfs[name]
		require.True(t, ok, "%s is missing entirely", name)
		var found bool
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "collector" && l.GetValue() == "gcp_gke" {
					found = true
				}
			}
		}
		assert.True(t, found, "%s has no series for the pending collector", name)
	}
}

func TestGCP_RetryPendingCreatesAndRegistersCollector(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, reg := newRegisteredGCP(t, ctx, serviceGKE)

	var attempts atomic.Int32
	create := func(context.Context, string) (provider.Collector, error) {
		if attempts.Add(1) < 3 {
			return nil, errors.New("stalled")
		}
		return &fakeCollector{name: "gcp_gke"}, nil
	}

	done := make(chan struct{})
	go func() { g.retryPending(ctx, create); close(done) }()

	require.Eventually(t, func() bool {
		_, ok := gatherByName(t, reg)["fake_collected_gcp_gke"]
		return ok
	}, 2*time.Second, 5*time.Millisecond, "the late collector's metrics must appear in the live registry")

	mfs := gatherByName(t, reg)
	assert.Contains(t, mfs, "fake_registered_gcp_gke", "the late collector must register its own metrics")
	value, ok := lastScrapeError(mfs, "gcp_gke")
	require.True(t, ok)
	assert.Equal(t, 0.0, value, "the error must clear once the collector exists")
	assert.EqualValues(t, 3, attempts.Load())
	assert.Empty(t, g.pendingServices())

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retryPending did not return after every collector was created")
	}
}

func TestGCP_RetryPendingStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	g, _ := newRegisteredGCP(t, ctx, serviceGKE)

	var attempts atomic.Int32
	create := func(context.Context, string) (provider.Collector, error) {
		attempts.Add(1)
		return nil, errors.New("still failing")
	}

	done := make(chan struct{})
	go func() { g.retryPending(ctx, create); close(done) }()

	require.Eventually(t, func() bool { return attempts.Load() >= 2 }, 2*time.Second, time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retryPending did not stop after the context was cancelled")
	}
	assert.Equal(t, []string{serviceGKE}, g.pendingServices(), "an uncreated collector stays reported as pending")
}

func TestGCP_AddCollectorRecoversFromRegisterPanic(t *testing.T) {
	g, _ := newRegisteredGCP(t, context.Background(), serviceGKE)

	err := g.addCollector(&fakeCollector{name: "gcp_gke", registerPanic: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "panic registering collector gcp_gke")
	collectors, _ := g.snapshot()
	assert.Empty(t, collectors, "a collector that failed to register must not be added")
	assert.Equal(t, []string{serviceGKE}, g.pendingServices(), "it must stay pending")
}

func TestGCP_AddCollectorBeforeRegisterCollectorsIsRegisteredLater(t *testing.T) {
	g := &GCP{logger: logger, ctx: context.Background(), collectorTimeout: time.Second}
	g.markPending(serviceGKE)

	require.NoError(t, g.addCollector(&fakeCollector{name: "gcp_gke"}))
	g.clearPending(serviceGKE)

	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(g))
	require.NoError(t, g.RegisterCollectors(reg))
	mfs := gatherByName(t, reg)

	assert.Contains(t, mfs, "fake_registered_gcp_gke")
	assert.Contains(t, mfs, "fake_collected_gcp_gke")
}

func TestGCP_ConcurrentCollectAndLateCollectors(t *testing.T) {
	g, reg := newRegisteredGCP(t, context.Background())

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = reg.Gather()
				}
			}
		}()
	}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		g.markPending(name)
		require.NoError(t, g.addCollector(&fakeCollector{name: name}))
		g.clearPending(name)
	}
	close(stop)
	wg.Wait()

	collectors, notCreated := g.snapshot()
	assert.Len(t, collectors, 5)
	assert.Empty(t, notCreated)
}

func TestCollectorNamesCoverEveryService(t *testing.T) {
	for _, s := range Services() {
		assert.Contains(t, collectorNames, s.Name, "service %s needs an entry so it is reported under its collector name", s.Name)
		for _, alias := range s.Aliases {
			assert.Contains(t, collectorNames, alias)
		}
	}
}

// A panicking constructor must not kill the exporter. The retry runs on a background goroutine of an
// already-serving pod, so an unrecovered panic would take every healthy collector down with it.
func TestGCP_RetryPendingSurvivesAPanickingConstructor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, _ := newRegisteredGCP(t, ctx, serviceGKE)

	var attempts atomic.Int32
	create := func(context.Context, string) (provider.Collector, error) {
		if attempts.Add(1) == 1 {
			panic("constructor blew up")
		}
		return &fakeCollector{name: "gcp_gke"}, nil
	}

	done := make(chan struct{})
	require.NotPanics(t, func() {
		go func() { g.retryPending(ctx, create); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("retryPending did not finish after recovering the panic")
		}
	})
	collectors, _ := g.snapshot()
	require.Len(t, collectors, 1, "the collector must still be created on the attempt after the panic")
	assert.Empty(t, g.pendingServices())
}
