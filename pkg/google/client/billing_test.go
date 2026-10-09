package client

import (
	"context"
	"sync"
	"testing"
	"time"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/type/money"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestStorageclassFromSkuDescription(t *testing.T) {
	tt := map[string]struct {
		exp string
	}{
		"Dual-Region Standard Class B Operation": {
			"MULTI_REGIONAL",
		},
		"Multi-Region Nearline Class A Operations": {
			"NEARLINE",
		},
		"Coldline Storage US Multi-region": {
			"COLDLINE",
		},
		"Durable Reduced Availability Multi-region": {
			"DRA",
		},
		"Standard Storage US Regional": {
			"REGIONAL",
		},
		"Standard Storage London": {
			"REGIONAL",
		},
		"Archive Storage Belgium Dual-region": {
			"ARCHIVE",
		},
	}

	for name, f := range tt {
		t.Run(name, func(t *testing.T) {
			got := storageClassFromSkuDescription(name, "any_regular_region")
			if got != f.exp {
				t.Errorf("expecting storageclass %s, got %s", f.exp, got)
			}
		})
	}
}

func TestPriceFromSku(t *testing.T) {
	sku := billingpb.Sku{
		PricingInfo: []*billingpb.PricingInfo{
			{PricingExpression: &billingpb.PricingExpression{
				TieredRates: []*billingpb.PricingExpression_TierRate{
					{UnitPrice: &money.Money{Nanos: 0}},
					{StartUsageAmount: 5, UnitPrice: &money.Money{Nanos: 4000000}},
				},
			}},
		},
	}
	got, err := getPriceFromSku(&sku)
	exp := 0.004
	if err != nil {
		t.Errorf("failed to parse sku")
	}
	if got != exp {
		t.Errorf("expect %f but got %f", exp, got)
	}
}

func TestMisformedPricingInfoFromSku(t *testing.T) {
	tt := []struct {
		sku   *billingpb.Sku
		descr string
	}{
		{
			sku: &billingpb.Sku{
				PricingInfo: []*billingpb.PricingInfo{},
			},
			descr: "should fail to parse sku with empty PricingInfo",
		},
		{
			sku: &billingpb.Sku{
				PricingInfo: []*billingpb.PricingInfo{
					{PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{},
					}},
				},
			},
			descr: "shoud fail to parse sku with empty TieredRates",
		},
	}

	for _, testcase := range tt {
		_, err := getPriceFromSku(testcase.sku)
		if err == nil {
			t.Error(testcase.descr)
		}
	}
}

func TestOpClassFromSkuDescription(t *testing.T) {
	tests := map[string]struct {
		str  string
		want string
	}{
		"OpsClass without class-a or class-b": {
			str:  "Standard Storage US Regional",
			want: "Standard Storage US Regional",
		},
		"OpsClass with class-a": {
			str:  "Standard Storage US Regional Class A Operations",
			want: "class-a",
		},
		"OpsClass with class-b": {
			str:  "Standard Storage US Regional Class B Operations",
			want: "class-b",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equalf(t, tt.want, opClassFromSkuDescription(tt.str), "OpClassFromSkuDescription(%v)", tt.want)
		})
	}
}

func Test_parseOpSku(t *testing.T) {
	tests := map[string]struct {
		sku *billingpb.Sku
		err error
	}{
		"should fail to parse sku with no pricing info": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Compute Engine",
				},
				ServiceRegions: []string{"us-east1"},
			},
			err: errInvalidSKU,
		},
		"should fail to parse sku with tagging": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Tagging Test",
				},
				Description: "Tagging",
			},
			err: errTaggingNotSupported,
		},
		"should parse a sku with pricing and description": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Compute Engine",
				},
				ServiceRegions: []string{"us-east1"},
				PricingInfo: []*billingpb.PricingInfo{
					{PricingExpression: &billingpb.PricingExpression{
						TieredRates: []*billingpb.PricingExpression_TierRate{
							{UnitPrice: &money.Money{Nanos: 0}},
							{StartUsageAmount: 5, UnitPrice: &money.Money{Nanos: 4000000}},
						},
					},
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := parseOpSku(tt.sku, metrics.NewMetrics())
			assert.ErrorIs(t, err, tt.err)
		})
	}
}

func Test_parseStorageSku(t *testing.T) {
	tests := map[string]struct {
		sku *billingpb.Sku
		err error
	}{
		"should fail to parse sku with no pricing info": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Compute Engine",
				},
				ServiceRegions: []string{"us-east1"},
			},
			err: errInvalidSKU,
		},
		"should fail to parse sku with unknown pricing unit": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Compute Engine",
				},
				ServiceRegions: []string{"us-east1"},
				PricingInfo: []*billingpb.PricingInfo{
					{PricingExpression: &billingpb.PricingExpression{
						UsageUnitDescription: "unknown",
						TieredRates: []*billingpb.PricingExpression_TierRate{
							{UnitPrice: &money.Money{Nanos: 0}},
							{StartUsageAmount: 5, UnitPrice: &money.Money{Nanos: 4000000}},
						},
					},
					},
				},
			},
			err: errUnknownPricingUnit,
		},
		"should parse a sku with one pricing unit with gibDaily": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Compute Engine",
				},
				ServiceRegions: []string{"us-east1"},
				PricingInfo: []*billingpb.PricingInfo{
					{PricingExpression: &billingpb.PricingExpression{
						UsageUnitDescription: gibDay,
						TieredRates: []*billingpb.PricingExpression_TierRate{
							{UnitPrice: &money.Money{Nanos: 0}},
							{StartUsageAmount: 5, UnitPrice: &money.Money{Nanos: 4000000}},
						},
					},
					},
				},
			},
			err: nil,
		},
		"should parse a sku with one pricing unit with gibMonthly": {
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Compute Engine",
				},
				ServiceRegions: []string{"us-east1"},
				PricingInfo: []*billingpb.PricingInfo{
					{PricingExpression: &billingpb.PricingExpression{
						UsageUnitDescription: gibMonthly,
						TieredRates: []*billingpb.PricingExpression_TierRate{
							{UnitPrice: &money.Money{Nanos: 0}},
							{StartUsageAmount: 5, UnitPrice: &money.Money{Nanos: 4000000}},
						},
					},
					},
				},
			},
			err: nil,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := parseStorageSku(tt.sku, metrics.NewMetrics())
			assert.ErrorIs(t, err, tt.err)
		})
	}
}

type failingListSkusServer struct {
	billingpb.UnimplementedCloudCatalogServer
}

func (s *failingListSkusServer) ListSkus(_ context.Context, _ *billingpb.ListSkusRequest) (*billingpb.ListSkusResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "boom")
}

// stallingListSkusServer blocks the first stallFirst calls for each page token until the client gives up.
// It serves two pages: the empty token returns page one and token "p2" returns page two.
type stallingListSkusServer struct {
	billingpb.UnimplementedCloudCatalogServer
	mu         sync.Mutex
	calls      map[string]int
	stallFirst int
}

func (s *stallingListSkusServer) ListSkus(ctx context.Context, req *billingpb.ListSkusRequest) (*billingpb.ListSkusResponse, error) {
	s.mu.Lock()
	if s.calls == nil {
		s.calls = map[string]int{}
	}
	s.calls[req.PageToken]++
	n := s.calls[req.PageToken]
	s.mu.Unlock()

	if n <= s.stallFirst {
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	if req.PageToken == "" {
		return &billingpb.ListSkusResponse{Skus: []*billingpb.Sku{{Name: "a"}, {Name: "b"}}, NextPageToken: "p2"}, nil
	}
	return &billingpb.ListSkusResponse{Skus: []*billingpb.Sku{{Name: "c"}}}, nil
}

func (s *stallingListSkusServer) totalCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for _, n := range s.calls {
		total += n
	}
	return total
}

func shortenSkuRetryTimings(t *testing.T) {
	t.Helper()
	oldTimeout, oldBackoff := skuPageTimeout, skuRetryBackoff
	skuPageTimeout, skuRetryBackoff = 100*time.Millisecond, time.Millisecond
	t.Cleanup(func() { skuPageTimeout, skuRetryBackoff = oldTimeout, oldBackoff })
}

// slowListSkusServer answers every call after a fixed delay, modelling an API that is slow but not stalled.
type slowListSkusServer struct {
	billingpb.UnimplementedCloudCatalogServer
	delay time.Duration
	mu    sync.Mutex
	calls int
}

func (s *slowListSkusServer) ListSkus(ctx context.Context, _ *billingpb.ListSkusRequest) (*billingpb.ListSkusResponse, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	select {
	case <-time.After(s.delay):
		return &billingpb.ListSkusResponse{Skus: []*billingpb.Sku{{Name: "a"}}}, nil
	case <-ctx.Done():
		return nil, status.FromContextError(ctx.Err()).Err()
	}
}

func (s *slowListSkusServer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// A page slower than the first attempt's deadline must still be fetched, because the deadline escalates to the
// client's 60s default. A flat first-attempt cap would drop a catalog that the old single 60s attempt fetched.
func TestGetPricingFetchesASlowButNotStalledPage(t *testing.T) {
	shortenSkuRetryTimings(t) // first attempt 100ms, so attempts are 100ms, 200ms, 300ms
	srv := &slowListSkusServer{delay: 250 * time.Millisecond}
	b := newBilling(NewTestBillingClient(t, srv), nil, nil)

	skus := b.getPricing(context.Background(), "services/networking")

	require.Len(t, skus, 1, "a page slower than the first deadline must still be fetched on a later attempt")
	assert.Equal(t, 3, srv.callCount(), "the first two attempts time out, the third has enough time")
}

func TestSkuAttemptTimeoutEscalatesToTheClientDefault(t *testing.T) {
	assert.Equal(t, 20*time.Second, skuAttemptTimeout(1))
	assert.Equal(t, 40*time.Second, skuAttemptTimeout(2))
	assert.Equal(t, 60*time.Second, skuAttemptTimeout(skuPageAttempts),
		"the last attempt must get the 60s the Google client allows by default, so we never lose a page it would fetch")
}

func TestGetPricingRetriesStalledPages(t *testing.T) {
	shortenSkuRetryTimings(t)

	t.Run("recovers when each page stalls once", func(t *testing.T) {
		srv := &stallingListSkusServer{stallFirst: 1}
		b := newBilling(NewTestBillingClient(t, srv), nil, nil)

		skus := b.getPricing(context.Background(), "services/networking")

		assert.Len(t, skus, 3)
		assert.Equal(t, 4, srv.totalCalls(), "two pages, each fetched twice")
	})

	t.Run("gives up and returns nil after the last attempt", func(t *testing.T) {
		srv := &stallingListSkusServer{stallFirst: 100}
		b := newBilling(NewTestBillingClient(t, srv), nil, nil)

		assert.Nil(t, b.getPricing(context.Background(), "services/networking"))
		assert.Equal(t, skuPageAttempts, srv.totalCalls())
	})

	t.Run("does not retry a permanent error", func(t *testing.T) {
		srv := &countingFailingServer{}
		b := newBilling(NewTestBillingClient(t, srv), nil, nil)

		assert.Nil(t, b.getPricing(context.Background(), "services/networking"))
		assert.Equal(t, 1, srv.calls)
	})
}

type countingFailingServer struct {
	billingpb.UnimplementedCloudCatalogServer
	calls int
}

func (s *countingFailingServer) ListSkus(_ context.Context, _ *billingpb.ListSkusRequest) (*billingpb.ListSkusResponse, error) {
	s.calls++
	return nil, status.Error(codes.PermissionDenied, "boom")
}

func TestGetPricing(t *testing.T) {
	t.Run("returns all skus when iteration succeeds", func(t *testing.T) {
		b := newBilling(NewTestBillingClient(t, &FakeCloudCatalogServerSlimResults{}), nil, nil)
		assert.NotEmpty(t, b.getPricing(context.Background(), "services/compute-engine"))
	})

	t.Run("returns nil and terminates when iteration fails", func(t *testing.T) {
		b := newBilling(NewTestBillingClient(t, &failingListSkusServer{}), nil, nil)

		done := make(chan []*billingpb.Sku, 1)
		go func() { done <- b.getPricing(context.Background(), "services/compute-engine") }()

		select {
		case skus := <-done:
			assert.Nil(t, skus)
		case <-time.After(5 * time.Second):
			t.Fatal("getPricing did not return after an iteration error")
		}
	})
}

func TestRegionNameSameAsStackdriver(t *testing.T) {
	tests := map[string]struct {
		region string
		want   string
	}{
		"region collectorName is same as stackdriver": {
			region: "us-east1",
			want:   "us-east1",
		},
		"region collectorName is not same as stackdriver": {
			region: "europe",
			want:   "eu",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equalf(t, tt.want, regionNameSameAsStackdriver(tt.region), "RegionNameSameAsStackdriver(%v)", tt.region)
		})
	}
}
