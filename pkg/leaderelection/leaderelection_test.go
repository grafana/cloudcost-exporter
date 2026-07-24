package leaderelection

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"k8s.io/client-go/kubernetes/fake"
)

func Test_NewIsLeaderGauge(t *testing.T) {
	gauge := NewIsLeaderGauge()
	want := "cloudcost_exporter_leader_election_is_leader"
	if got := testutil.CollectAndCount(gauge, want); got != 1 {
		t.Errorf("expected metric %q to be present once, counted %d", want, got)
	}
	if got := testutil.ToFloat64(gauge); got != 0 {
		t.Errorf("expected gauge to default to 0, got %v", got)
	}
}

func Test_ResolveNamespace(t *testing.T) {
	tests := map[string]struct {
		configured string
		want       string
	}{
		"uses configured value when set":    {configured: "monitoring", want: "monitoring"},
		"falls back to default off-cluster": {configured: "", want: "default"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := ResolveNamespace(tc.configured); got != tc.want {
				t.Errorf("ResolveNamespace(%q) = %q, want %q", tc.configured, got, tc.want)
			}
		})
	}
}

func Test_ResolveIdentity(t *testing.T) {
	t.Run("uses configured value when set", func(t *testing.T) {
		got, err := ResolveIdentity("pod-a")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "pod-a" {
			t.Errorf("ResolveIdentity(\"pod-a\") = %q, want \"pod-a\"", got)
		}
	})

	t.Run("falls back to hostname", func(t *testing.T) {
		got, err := ResolveIdentity("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		host, _ := os.Hostname()
		if got != host {
			t.Errorf("ResolveIdentity(\"\") = %q, want hostname %q", got, host)
		}
	})
}

func Test_Run_acquiresLeadershipAndStartsLeading(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := fake.NewSimpleClientset()
	gauge := NewIsLeaderGauge()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	opts := Options{
		LeaseName:     "cloudcost-exporter",
		Namespace:     "default",
		Identity:      "pod-a",
		LeaseDuration: 100 * time.Millisecond,
		RenewDeadline: 80 * time.Millisecond,
		RetryPeriod:   20 * time.Millisecond,
	}

	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(ctx, client, opts, log, gauge, func(context.Context) {
			close(started)
		})
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting to acquire leadership")
	}

	if got := testutil.ToFloat64(gauge); got != 1 {
		t.Errorf("expected is_leader gauge to be 1 after acquiring leadership, got %v", got)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Run to return after context cancel")
	}

	if got := testutil.ToFloat64(gauge); got != 0 {
		t.Errorf("expected is_leader gauge to be 0 after losing leadership, got %v", got)
	}
}
