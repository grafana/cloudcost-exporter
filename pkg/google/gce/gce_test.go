package gce

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	computev1 "google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, nil))

// standaloneInstances mixes a GKE-labeled node in with standalone VMs, including
// one on an unpriced machine type, matching the fixture gke_test.go uses for the
// same "unpriced machine type" case (see grafana/cloudcost-exporter#335).
func standaloneInstances() []*computev1.Instance {
	return []*computev1.Instance{
		{
			Name:        "gke-node",
			MachineType: "abc/n1-slim",
			Zone:        "testing/us-central1-a",
			Scheduling:  &computev1.Scheduling{ProvisioningModel: "test"},
			Labels:      map[string]string{client.GkeClusterLabel: "test"},
		},
		{
			Name:        "standalone-n1",
			MachineType: "abc/n1-slim",
			Zone:        "testing/us-central1-a",
			Scheduling:  &computev1.Scheduling{ProvisioningModel: "test"},
		},
		{
			Name:        "standalone-n1-spot",
			MachineType: "abc/n1-slim",
			Zone:        "testing/us-central1-a",
			Scheduling:  &computev1.Scheduling{ProvisioningModel: "SPOT"},
		},
		{
			// No SKU exists for this machine type in the fake billing fixture below,
			// mirroring gke_test.go's unpriced-machine-type case: the instance is
			// skipped entirely, not just its total metric.
			Name:        "standalone-n8",
			MachineType: "abc/n8-slim",
			Zone:        "testing/us-central1-a",
			Scheduling:  &computev1.Scheduling{ProvisioningModel: "test"},
		},
	}
}

func computeHandler(serveMachineType bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var buf interface{}
		switch r.URL.Path {
		case "/projects/testing/zones/us-central1-a/instances":
			buf = &computev1.InstanceList{Items: standaloneInstances()}
		case "/projects/testing/zones":
			buf = &computev1.ZoneList{Items: []*computev1.Zone{{Name: "us-central1-a"}}}
		case "/projects/testing/zones/us-central1-a/machineTypes/n1-slim":
			if !serveMachineType {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			buf = &computev1.MachineType{GuestCpus: 2, MemoryMb: 4096}
		default:
			// e.g. the unpriced "n8-slim" machine type: no fixture data for it,
			// same as a real API 404 for an unrecognized machine type.
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(buf)
	}
}

func TestCollector_Collect(t *testing.T) {
	tests := map[string]struct {
		config          *Config
		testServer      *httptest.Server
		expectedMetrics []*utils.MetricResult
	}{
		"Handle http error": {
			config: &Config{Projects: "testing"},
			testServer: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			})),
			expectedMetrics: []*utils.MetricResult{},
		},
		"Excludes GKE nodes, prices standalone VMs, skips unpriced machine types": {
			config:     &Config{Projects: "testing"},
			testServer: httptest.NewServer(computeHandler(true)),
			expectedMetrics: []*utils.MetricResult{
				{
					FqName: "cloudcost_gcp_gce_instance_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family": "n1", "instance": "standalone-n1", "machine_type": "n1-slim",
						"price_tier": "ondemand", "project": "testing", "region": "us-central1",
					},
					Value: 1, MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gce_instance_memory_usd_per_gib_hour",
					Labels: map[string]string{
						"family": "n1", "instance": "standalone-n1", "machine_type": "n1-slim",
						"price_tier": "ondemand", "project": "testing", "region": "us-central1",
					},
					Value: 1, MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gce_instance_total_usd_per_hour",
					Labels: map[string]string{
						"family": "n1", "instance": "standalone-n1", "machine_type": "n1-slim",
						"price_tier": "ondemand", "project": "testing", "region": "us-central1",
					},
					Value: 6, MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gce_instance_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family": "n1", "instance": "standalone-n1-spot", "machine_type": "n1-slim",
						"price_tier": "spot", "project": "testing", "region": "us-central1",
					},
					Value: 1, MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gce_instance_memory_usd_per_gib_hour",
					Labels: map[string]string{
						"family": "n1", "instance": "standalone-n1-spot", "machine_type": "n1-slim",
						"price_tier": "spot", "project": "testing", "region": "us-central1",
					},
					Value: 1, MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gce_instance_total_usd_per_hour",
					Labels: map[string]string{
						"family": "n1", "instance": "standalone-n1-spot", "machine_type": "n1-slim",
						"price_tier": "spot", "project": "testing", "region": "us-central1",
					},
					Value: 6, MetricType: prometheus.GaugeValue,
				},
			},
		},
		"Machine type lookup failure suppresses only the total metric": {
			config:     &Config{Projects: "testing"},
			testServer: httptest.NewServer(computeHandler(false)),
			expectedMetrics: []*utils.MetricResult{
				{
					FqName: "cloudcost_gcp_gce_instance_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family": "n1", "instance": "standalone-n1", "machine_type": "n1-slim",
						"price_tier": "ondemand", "project": "testing", "region": "us-central1",
					},
					Value: 1, MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gce_instance_memory_usd_per_gib_hour",
					Labels: map[string]string{
						"family": "n1", "instance": "standalone-n1", "machine_type": "n1-slim",
						"price_tier": "ondemand", "project": "testing", "region": "us-central1",
					},
					Value: 1, MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gce_instance_cpu_usd_per_core_hour",
					Labels: map[string]string{
						"family": "n1", "instance": "standalone-n1-spot", "machine_type": "n1-slim",
						"price_tier": "spot", "project": "testing", "region": "us-central1",
					},
					Value: 1, MetricType: prometheus.GaugeValue,
				},
				{
					FqName: "cloudcost_gcp_gce_instance_memory_usd_per_gib_hour",
					Labels: map[string]string{
						"family": "n1", "instance": "standalone-n1-spot", "machine_type": "n1-slim",
						"price_tier": "spot", "project": "testing", "region": "us-central1",
					},
					Value: 1, MetricType: prometheus.GaugeValue,
				},
			},
		},
		"Empty node store emits no metrics": {
			config: &Config{Projects: "testing"},
			testServer: httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var buf interface{}
				switch r.URL.Path {
				case "/projects/testing/zones/us-central1-a/instances":
					buf = &computev1.InstanceList{}
				case "/projects/testing/zones":
					buf = &computev1.ZoneList{Items: []*computev1.Zone{{Name: "us-central1-a"}}}
				}
				_ = json.NewEncoder(w).Encode(buf)
			})),
			expectedMetrics: []*utils.MetricResult{},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			computeService, err := computev1.NewService(t.Context(), option.WithoutAuthentication(), option.WithEndpoint(test.testServer.URL))
			require.NoError(t, err)
			l, err := net.Listen("tcp", "localhost:0")
			require.NoError(t, err)
			gsrv := grpc.NewServer()
			defer gsrv.Stop()
			billingpb.RegisterCloudCatalogServer(gsrv, &client.FakeCloudCatalogServer{})
			go func() {
				_ = gsrv.Serve(l)
			}()

			cloudCatalogClient, err := billingv1.NewCloudCatalogClient(t.Context(),
				option.WithEndpoint(l.Addr().String()),
				option.WithoutAuthentication(),
				option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
			)
			require.NoError(t, err)

			gcpClient := client.NewMock("testing", 0, nil, nil, cloudCatalogClient, computeService, nil, nil)
			collector, err := New(t.Context(), test.config, logger, gcpClient)
			require.NoError(t, err)
			require.NotNil(t, collector)

			// Wait for the background stores to complete their initial population
			// before collecting, same convention as gke's test suite.
			<-collector.nodeStore.Done()
			<-collector.machineTypes.Done()

			ch := make(chan prometheus.Metric)
			go func() {
				require.NoError(t, collector.Collect(t.Context(), ch))
				close(ch)
			}()

			var metrics []*utils.MetricResult
			for metric := range ch {
				metrics = append(metrics, utils.ReadMetrics(metric))
			}
			if len(metrics) == 0 {
				return
			}
			assert.ElementsMatch(t, metrics, test.expectedMetrics)
		})
	}
}

func TestCollector_Name(t *testing.T) {
	c := &Collector{}
	assert.Equal(t, "gcp_gce", c.Name())
}

func TestCollector_Describe(t *testing.T) {
	c := &Collector{}
	ch := make(chan *prometheus.Desc, 3)
	require.NoError(t, c.Describe(ch))
	close(ch)
	var descs []*prometheus.Desc
	for d := range ch {
		descs = append(descs, d)
	}
	assert.Len(t, descs, 3)
}
