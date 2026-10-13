package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	computev1 "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// flakyCatalog wraps the fake Cloud Catalog and fails calls on demand with DeadlineExceeded, which is what a
// stalled call returns once the client's deadline passes. It lets tests reproduce a slow billing API without
// waiting on real timeouts.
type flakyCatalog struct {
	billingpb.UnimplementedCloudCatalogServer
	inner billingpb.CloudCatalogServer

	mu                sync.Mutex
	failServicesFirst int            // number of ListServices calls to fail before succeeding
	failSkusFirst     int            // number of ListSkus calls to fail for each (parent, page token)
	outage            bool           // while true, every call fails
	servicesCalls     int            // ListServices calls seen
	skusCalls         map[string]int // ListSkus calls seen, by parent and page token
	retryGate         chan struct{}  // when non-nil, ListServices calls after the first block until it closes
}

func newFlakyCatalog() *flakyCatalog {
	return &flakyCatalog{inner: &client.FakeCloudCatalogServer{}, skusCalls: map[string]int{}}
}

// holdRetries blocks every ListServices call after the startup one until releaseRetries runs. It makes the
// state between a failed startup and a successful retry observable without racing the retry goroutine.
func (f *flakyCatalog) holdRetries() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retryGate = make(chan struct{})
}

func (f *flakyCatalog) releaseRetries() {
	f.mu.Lock()
	defer f.mu.Unlock()
	close(f.retryGate)
}

func (f *flakyCatalog) setOutage(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outage = on
}

func (f *flakyCatalog) serviceCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.servicesCalls
}

func (f *flakyCatalog) ListServices(ctx context.Context, _ *billingpb.ListServicesRequest) (*billingpb.ListServicesResponse, error) {
	f.mu.Lock()
	f.servicesCalls++
	call := f.servicesCalls
	fail := f.outage || call <= f.failServicesFirst
	gate := f.retryGate
	f.mu.Unlock()
	if gate != nil && call > 1 {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, status.Error(codes.Canceled, "test ended")
		}
	}
	if fail {
		return nil, status.Error(codes.DeadlineExceeded, "stalled")
	}
	// Every display name a collector resolves, so each one can be built here rather
	// than skipping for want of a service entry.
	return &billingpb.ListServicesResponse{Services: []*billingpb.Service{
		{DisplayName: "Compute Engine", Name: "services/compute-engine"},
		{DisplayName: "Networking", Name: "services/networking"},
		{DisplayName: "Cloud SQL", Name: "services/cloud-sql"},
		{DisplayName: "Cloud Storage", Name: "services/cloud-storage"},
		{DisplayName: "Managed Service for Apache Kafka", Name: "services/managed-kafka"},
		{DisplayName: "Vertex AI", Name: "services/vertex-ai"},
		{DisplayName: "Vertex AI Search", Name: "services/vertex-ai-search"},
	}}, nil
}

func (f *flakyCatalog) ListSkus(ctx context.Context, req *billingpb.ListSkusRequest) (*billingpb.ListSkusResponse, error) {
	key := req.Parent + "|" + req.PageToken
	f.mu.Lock()
	f.skusCalls[key]++
	fail := f.outage || f.skusCalls[key] <= f.failSkusFirst
	f.mu.Unlock()
	if fail {
		return nil, status.Error(codes.DeadlineExceeded, "stalled")
	}
	if req.Parent == managedKafkaService {
		return &billingpb.ListSkusResponse{Skus: []*billingpb.Sku{managedKafkaSku()}}, nil
	}
	return f.inner.ListSkus(ctx, req)
}

const managedKafkaService = "services/managed-kafka"

// managedKafkaSku is the minimum a Managed Kafka SKU needs to parse: a description
// classifySKU recognises, a region, and a tiered rate. The shared fake catalog has
// no Kafka SKUs, so without this the collector cannot be built here at all.
func managedKafkaSku() *billingpb.Sku {
	return &billingpb.Sku{
		Description:    "Kafka data compute units in Iowa",
		ServiceRegions: []string{"us-central1"},
		Category:       &billingpb.Category{ResourceGroup: "ManagedKafka"},
		PricingInfo: []*billingpb.PricingInfo{{
			PricingExpression: &billingpb.PricingExpression{
				UsageUnitDescription: "hour",
				TieredRates: []*billingpb.PricingExpression_TierRate{{
					UnitPrice: &money.Money{CurrencyCode: "USD", Nanos: 1_000_000},
				}},
			},
		}},
	}
}

// newFaultInjectedProvider runs the real provider startup, with the real Billing code and the real collector
// constructors, against a catalog that fails on demand and a compute API that returns empty responses.
func newFaultInjectedProvider(t *testing.T, catalog *flakyCatalog, services ...string) *GCP {
	t.Helper()

	computeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(struct{}{})
	}))
	t.Cleanup(computeServer.Close)
	computeService, err := computev1.NewService(t.Context(), option.WithoutAuthentication(), option.WithEndpoint(computeServer.URL))
	require.NoError(t, err)

	gcpClient := client.NewMock("testing", 0, nil, nil, client.NewTestBillingClient(t, catalog), computeService, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return newWithClient(ctx, &Config{
		ProjectId:             "testing",
		Projects:              "testing",
		Services:              services,
		ScrapeInterval:        time.Hour,
		CollectorTimeout:      time.Minute,
		GKEZoneConcurrency:    1,
		CollectorRetryInitial: testRetryInitial,
		CollectorRetryMax:     testRetryMax,
		Logger:                logger,
	}, gcpClient)
}

func collectorNamesOf(g *GCP) []string {
	collectors, _ := g.snapshot()
	names := make([]string, 0, len(collectors))
	for _, c := range collectors {
		names = append(names, c.Name())
	}
	slices.Sort(names)
	return names
}

func TestFaultInjection_CollectorFailedByServiceLookupIsCreatedByBackgroundRetry(t *testing.T) {
	catalog := newFlakyCatalog()
	catalog.failServicesFirst = 2 // the service lookup stalls twice, and it has no call-level retry
	catalog.holdRetries()         // no retry can land before the startup state is asserted

	g := newFaultInjectedProvider(t, catalog, serviceGKE)

	require.Equal(t, []string{serviceGKE}, g.pendingServices(), "the collector is skipped at startup, as it was in production")
	require.Empty(t, collectorNamesOf(g), "startup must not produce the collector on its own")

	catalog.releaseRetries()

	require.Eventually(t, func() bool {
		return slices.Equal(collectorNamesOf(g), []string{"gcp_gke"})
	}, 5*time.Second, 5*time.Millisecond, "the background retry must create the collector without a restart")
	assert.Empty(t, g.pendingServices())
	assert.GreaterOrEqual(t, catalog.serviceCalls(), 3)
}

func TestFaultInjection_CollectorRecoversAfterALongOutage(t *testing.T) {
	catalog := newFlakyCatalog()
	catalog.setOutage(true)

	g := newFaultInjectedProvider(t, catalog, serviceGKE)

	require.Equal(t, []string{serviceGKE}, g.pendingServices())
	callsAtStart := catalog.serviceCalls()
	require.Eventually(t, func() bool { return catalog.serviceCalls() >= callsAtStart+3 }, 5*time.Second, 5*time.Millisecond,
		"the retry must keep trying for as long as the outage lasts")
	assert.Empty(t, collectorNamesOf(g), "no collector can exist while the catalog is down")

	catalog.setOutage(false)

	require.Eventually(t, func() bool {
		return slices.Equal(collectorNamesOf(g), []string{"gcp_gke"})
	}, 5*time.Second, 5*time.Millisecond, "the collector must appear once the catalog recovers")
	assert.Empty(t, g.pendingServices())
}

// collectorNames is hand-maintained, and a stale entry would be silent: the error metric would report a
// missing collector under one label while the healthy collector reports under another, so the two would
// never line up as one series and a recovery would never look like it cleared. Build each collector for
// real and assert the map matches the name it actually reports.
func TestCollectorNamesMatchTheNameEachCollectorReports(t *testing.T) {
	for _, svc := range Services() {
		t.Run(svc.Name, func(t *testing.T) {
			g := newFaultInjectedProvider(t, newFlakyCatalog(), svc.Name)
			collectors, notCreated := g.snapshot()
			// Deliberately not a skip. The provider recovers a constructor panic, so a
			// skip here would turn any future crash in a constructor into a quiet pass.
			require.Len(t, collectors, 1, "%s must be constructible in this harness (pending: %v)", svc.Name, notCreated)
			assert.Equal(t, collectorNames[svc.Name], collectors[0].Name(),
				"collectorNames[%q] is stale: the collector reports %q", svc.Name, collectors[0].Name())
		})
	}
}
