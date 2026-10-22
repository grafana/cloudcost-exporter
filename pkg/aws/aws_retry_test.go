package aws

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

const (
	testRetryInitial = time.Millisecond
	testRetryMax     = 5 * time.Millisecond
)

// fakeCollector stands in for a real collector. Its metrics are named after it
// so tests can tell which collector reached the registry.
type fakeCollector struct {
	name          string
	registerPanic bool
}

func (f *fakeCollector) Name() string { return f.name }

func (f *fakeCollector) Register(r provider.Registry) error {
	if f.registerPanic {
		panic("register exploded")
	}
	return r.Register(prometheus.NewGauge(prometheus.GaugeOpts{Name: "fake_registered_" + f.name}))
}

func (f *fakeCollector) Describe(ch chan<- *prometheus.Desc) error { return nil }

func (f *fakeCollector) Collect(_ context.Context, ch chan<- prometheus.Metric) error {
	ch <- prometheus.MustNewConstMetric(
		prometheus.NewDesc("fake_collected_"+f.name, "", nil, nil), prometheus.GaugeValue, 1)
	return nil
}

// newRegisteredAWS builds a provider whose registry is already serving, which is
// the state a background retry has to register into.
func newRegisteredAWS(t *testing.T, ctx context.Context, pendingServices ...string) (*AWS, *prometheus.Registry) {
	t.Helper()
	a := &AWS{
		Config:           &Config{},
		logger:           logger,
		ctx:              ctx,
		collectorTimeout: time.Second,
		retryInitial:     testRetryInitial,
		retryMax:         testRetryMax,
	}
	for _, s := range pendingServices {
		a.markPending(s)
	}
	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(a))
	require.NoError(t, a.RegisterCollectors(reg))
	return a, reg
}

func gatherByName(t *testing.T, reg *prometheus.Registry) map[string]*dto.MetricFamily {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	out := make(map[string]*dto.MetricFamily, len(mfs))
	for _, mf := range mfs {
		out[mf.GetName()] = mf
	}
	return out
}

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

func TestAWS_PendingCollectorIsReportedAsError(t *testing.T) {
	_, reg := newRegisteredAWS(t, context.Background(), serviceVPC)

	v, ok := lastScrapeError(gatherByName(t, reg), "VPC")

	require.True(t, ok, "a collector that failed to create must still be reported")
	assert.Equal(t, 1.0, v)
}

func TestAWS_PendingCollectorEmitsTheWholeMetricFamily(t *testing.T) {
	_, reg := newRegisteredAWS(t, context.Background(), serviceVPC)

	mfs := gatherByName(t, reg)
	for _, name := range []string{
		"cloudcost_exporter_collector_last_scrape_error",
		"cloudcost_exporter_collector_last_scrape_duration_seconds",
		"cloudcost_exporter_collector_last_scrape_time",
	} {
		assert.Contains(t, mfs, name, "%s must be emitted for a pending collector", name)
	}
}

func TestAWS_RetryPendingCreatesAndRegistersCollector(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, reg := newRegisteredAWS(t, ctx, serviceVPC)

	var attempts atomic.Int32
	create := func(context.Context, string) (provider.Collector, error) {
		if attempts.Add(1) < 3 {
			return nil, errors.New("pricing api is slow")
		}
		return &fakeCollector{name: "VPC"}, nil
	}

	done := make(chan struct{})
	go func() { a.retryPending(ctx, create); close(done) }()

	require.Eventually(t, func() bool {
		_, ok := gatherByName(t, reg)["fake_collected_VPC"]
		return ok
	}, 2*time.Second, 5*time.Millisecond, "the late collector must emit into the live registry")

	mfs := gatherByName(t, reg)
	assert.Contains(t, mfs, "fake_registered_VPC", "it must register its own metrics")
	v, ok := lastScrapeError(mfs, "VPC")
	require.True(t, ok)
	assert.Equal(t, 0.0, v, "the error must clear once the collector exists")
	assert.EqualValues(t, 3, attempts.Load())
	assert.Empty(t, a.pendingServices())

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retryPending must return once nothing is pending")
	}
}

func TestAWS_RetryPendingStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a, _ := newRegisteredAWS(t, ctx, serviceVPC)

	create := func(context.Context, string) (provider.Collector, error) {
		return nil, errors.New("still broken")
	}
	done := make(chan struct{})
	go func() { a.retryPending(ctx, create); close(done) }()
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("retryPending must return when ctx is cancelled")
	}
	assert.Equal(t, []string{serviceVPC}, a.pendingServices(), "an uncreated collector stays pending")
}

func TestAWS_RetryPendingSurvivesAPanickingConstructor(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, _ := newRegisteredAWS(t, ctx, serviceVPC)

	var attempts atomic.Int32
	create := func(context.Context, string) (provider.Collector, error) {
		if attempts.Add(1) < 2 {
			panic("constructor exploded")
		}
		return &fakeCollector{name: "VPC"}, nil
	}
	go a.retryPending(ctx, create)

	require.Eventually(t, func() bool { return len(a.pendingServices()) == 0 }, 2*time.Second, 5*time.Millisecond,
		"a panicking constructor must be recovered and retried, not crash the exporter")
}

func TestAWS_AddCollectorRecoversFromRegisterPanic(t *testing.T) {
	a, _ := newRegisteredAWS(t, context.Background(), serviceVPC)

	err := a.addCollector(serviceVPC, &fakeCollector{name: "VPC", registerPanic: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "panic registering collector VPC")
	collectors, _ := a.snapshot()
	assert.Empty(t, collectors, "a collector that failed to register must not be added")
	assert.Equal(t, []string{serviceVPC}, a.pendingServices(), "it must stay pending")
}

func TestAWS_AddCollectorClearsPendingAtomically(t *testing.T) {
	a, reg := newRegisteredAWS(t, context.Background(), serviceVPC)

	require.NoError(t, a.addCollector(serviceVPC, &fakeCollector{name: "VPC"}))

	_, notCreated := a.snapshot()
	assert.Empty(t, notCreated, "a created collector must not also be reported as missing")
	assert.Empty(t, a.pendingServices())

	// Collect reports pending services under the collector's own name, so a
	// collector that is both present and pending emits one label set twice and
	// Gather rejects the whole scrape.
	mfs, err := reg.Gather()
	require.NoError(t, err, "the scrape must not fail on a duplicate metric")
	for _, mf := range mfs {
		if name := mf.GetName(); name == "cloudcost_exporter_collector_last_scrape_error" ||
			name == "cloudcost_exporter_collector_last_scrape_duration_seconds" ||
			name == "cloudcost_exporter_collector_last_scrape_time" {
			assert.Len(t, mf.GetMetric(), 1, "%s must have exactly one series", name)
		}
	}
}

func TestAWS_ConcurrentCollectAndLateCollectors(t *testing.T) {
	a, reg := newRegisteredAWS(t, context.Background())

	var wg sync.WaitGroup
	var gatherErr atomic.Pointer[error]
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
					// A scrape must never fail while collectors are being added.
					if _, err := reg.Gather(); err != nil {
						gatherErr.Store(&err)
						return
					}
				}
			}
		}()
	}
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		a.markPending(name)
		require.NoError(t, a.addCollector(name, &fakeCollector{name: name}))
	}
	close(stop)
	wg.Wait()
	if err := gatherErr.Load(); err != nil {
		t.Fatalf("a scrape failed while collectors were being added: %v", *err)
	}

	collectors, notCreated := a.snapshot()
	assert.Len(t, collectors, 5)
	assert.Empty(t, notCreated)
}

// Every service must have an entry, otherwise a pending collector would be
// reported under a lowercased service name that no healthy collector uses.
func TestAWS_EveryServiceHasACollectorName(t *testing.T) {
	for _, s := range Services() {
		assert.Contains(t, collectorNames, s.Name,
			"service %s needs an entry so it is reported under its collector name", s.Name)
	}
}

func TestAWS_JitterStaysWithinBounds(t *testing.T) {
	const d = time.Minute
	low, high := d-d/retryJitterFraction, d+d/retryJitterFraction
	for range 200 {
		got := jittered(d)
		assert.GreaterOrEqual(t, got, low)
		assert.LessOrEqual(t, got, high)
	}
	assert.Zero(t, jittered(0), "zero must stay zero")
}
