package aws

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	mock_client "github.com/grafana/cloudcost-exporter/pkg/aws/client/mocks"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

// countingHandler counts log records whose message matches, so a test can
// observe the background retry without reaching into it.
type countingHandler struct {
	slog.Handler
	match string
	n     atomic.Int32
}

func (h *countingHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == h.match {
		h.n.Add(1)
	}
	return nil
}
func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *countingHandler) WithAttrs(_ []slog.Attr) slog.Handler     { return h }
func (h *countingHandler) WithGroup(_ string) slog.Handler          { return h }

// newFaultInjected runs the real provider startup with the real collector
// constructors. The pricing-dependent collectors cannot reach a pricing API
// here, so they fail to construct, which is the same shape as the pricing API
// being briefly unavailable during a rollout.
func newFaultInjected(t *testing.T, log *slog.Logger, services ...string) *AWS {
	t.Helper()
	ctrl := gomock.NewController(t)
	mockClient := mock_client.NewMockClient(ctrl)
	mockClient.EXPECT().DescribeRegions(gomock.Any(), false).Return(nil, nil).AnyTimes()

	if log == nil {
		log = logger
	}
	config := &Config{
		Services:              services,
		Region:                "us-east-1",
		AccountID:             "123456789012",
		ScrapeInterval:        time.Hour,
		CollectorTimeout:      time.Minute,
		Logger:                log,
		CollectorRetryInitial: testRetryInitial,
		CollectorRetryMax:     testRetryMax,
	}
	regions := []types.Region{{RegionName: utils.StringPtr("us-east-1")}}
	a, err := newWithDependencies(
		t.Context(), config, mockClient, map[string]client.Client{}, regions,
		aws.Config{Region: "us-east-1"}, aws.Config{Region: "us-east-1"},
	)
	require.NoError(t, err, "a collector that fails to create must not fail startup")
	return a
}

func TestFaultInjection_FailedCollectorIsRecordedNotDropped(t *testing.T) {
	a := newFaultInjected(t, nil, serviceVPC)

	require.Equal(t, []string{serviceVPC}, a.pendingServices(),
		"the collector must be recorded as pending, not silently dropped")
	collectors, notCreated := a.snapshot()
	assert.Empty(t, collectors, "the collector genuinely failed to build")
	assert.Equal(t, []string{"VPC"}, notCreated,
		"it must be reported under the name the healthy collector uses, so the series lines up")
}

func TestFaultInjection_NewStartsTheBackgroundRetry(t *testing.T) {
	h := &countingHandler{match: "Retrying collector creation failed"}
	a := newFaultInjected(t, slog.New(h), serviceVPC)

	require.NotEmpty(t, a.pendingServices(), "precondition: something must be pending")
	require.Eventually(t, func() bool { return h.n.Load() >= 2 }, 5*time.Second, 10*time.Millisecond,
		"New must start a goroutine that keeps retrying a collector that failed at startup")
}

func TestFaultInjection_HealthyStartupNeedsNoRetry(t *testing.T) {
	h := &countingHandler{match: "Retrying collector creation failed"}
	a := newFaultInjected(t, slog.New(h), serviceS3)

	collectors, notCreated := a.snapshot()
	require.Len(t, collectors, 1, "a healthy collector is built at startup")
	assert.Empty(t, notCreated)
	assert.Empty(t, a.pendingServices())

	time.Sleep(20 * testRetryMax)
	assert.Zero(t, h.n.Load(), "nothing pending means no retry goroutine and no retry logging")
}

// collectorNames is hand-maintained, and a stale entry would be silent: the
// error metric would report a missing collector under one label while the
// healthy collector reports under another, so the two would never line up as
// one series and a recovery would never look like it cleared.
//
// Only the collectors that can be constructed without a live pricing API are
// covered. EC2, MSK, NATGATEWAY, VPC and BEDROCK all build a pricing client in
// their constructor, so they cannot be built here; their entries were taken
// from the names the deployed collectors report.
func TestCollectorNamesMatchTheNameEachCollectorReports(t *testing.T) {
	for _, svc := range []string{serviceS3, serviceRDS, serviceELB} {
		t.Run(svc, func(t *testing.T) {
			a := newFaultInjected(t, nil, svc)
			collectors, notCreated := a.snapshot()
			require.Len(t, collectors, 1, "%s must be constructible here (pending: %v)", svc, notCreated)
			assert.Equal(t, collectorNames[svc], collectors[0].Name(),
				"collectorNames[%q] is stale: the collector reports %q", svc, collectors[0].Name())
		})
	}
}
