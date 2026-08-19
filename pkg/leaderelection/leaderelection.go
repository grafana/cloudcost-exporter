// Package leaderelection provides opt-in, lease-based leader election so that
// only one cloudcost-exporter replica collects from the cloud provider APIs at
// a time. Non-leader replicas keep serving up/down metrics but do not query the
// provider, which keeps load on the upstream pricing APIs flat as replicas scale.
package leaderelection

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
)

const subsystem = "leader_election"

// namespaceFile exposes the pod's service account namespace inside a cluster.
const namespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// Options configures lease-based leader election.
type Options struct {
	LeaseName     string
	Namespace     string
	Identity      string
	LeaseDuration time.Duration
	RenewDeadline time.Duration
	RetryPeriod   time.Duration
}

// NewIsLeaderGauge returns a gauge that reports whether this replica holds the
// leader lease: 1 when leading, 0 otherwise.
func NewIsLeaderGauge() prometheus.Gauge {
	return prometheus.NewGauge(prometheus.GaugeOpts{
		Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "is_leader"),
		Help: "Whether this replica holds the leader lease: 1 if leader, 0 otherwise.",
	})
}

// ResolveNamespace returns the configured namespace, falling back to the pod's
// service account namespace, then to "default".
func ResolveNamespace(configured string) string {
	if configured != "" {
		return configured
	}
	if data, err := os.ReadFile(namespaceFile); err == nil {
		if ns := strings.TrimSpace(string(data)); ns != "" {
			return ns
		}
	}
	return "default"
}

// ResolveIdentity returns the configured identity, falling back to the hostname.
func ResolveIdentity(configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	host, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("resolving leader-election identity from hostname: %w", err)
	}
	return host, nil
}

// NewInClusterClient builds a Kubernetes client from the in-cluster service
// account. Leader election requires running inside a cluster.
func NewInClusterClient() (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("loading in-cluster config: %w", err)
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("building Kubernetes client: %w", err)
	}
	return client, nil
}

// Run participates in lease-based leader election until ctx is cancelled or
// leadership is lost. onStartedLeading runs when this replica acquires the
// lease; its context is cancelled when leadership is lost, which the caller
// uses to stop provider collection. Run returns when ctx is cancelled or the
// lease is lost, letting the caller shut down and rejoin as a candidate.
func Run(ctx context.Context, client kubernetes.Interface, opts Options, log *slog.Logger, isLeader prometheus.Gauge, onStartedLeading func(context.Context)) {
	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      opts.LeaseName,
			Namespace: opts.Namespace,
		},
		Client:     client.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{Identity: opts.Identity},
	}

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   opts.LeaseDuration,
		RenewDeadline:   opts.RenewDeadline,
		RetryPeriod:     opts.RetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(leaderCtx context.Context) {
				isLeader.Set(1)
				log.LogAttrs(leaderCtx, slog.LevelInfo, "Acquired leader lease", slog.String("identity", opts.Identity))
				onStartedLeading(leaderCtx)
			},
			OnStoppedLeading: func() {
				isLeader.Set(0)
				log.LogAttrs(ctx, slog.LevelInfo, "Lost leader lease", slog.String("identity", opts.Identity))
			},
			OnNewLeader: func(leader string) {
				if leader == opts.Identity {
					return
				}
				log.LogAttrs(ctx, slog.LevelInfo, "Observed new leader", slog.String("leader", leader))
			},
		},
	})
}
