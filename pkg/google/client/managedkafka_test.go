package client

import (
	"context"
	"net"
	"testing"

	managedkafka "cloud.google.com/go/managedkafka/apiv1"
	managedkafkapb "cloud.google.com/go/managedkafka/apiv1/managedkafkapb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"
	locationpb "google.golang.org/genproto/googleapis/cloud/location"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type fakeManagedKafkaServer struct {
	managedkafkapb.UnimplementedManagedKafkaServer
	locationpb.UnimplementedLocationsServer

	clustersResponse  *managedkafkapb.ListClustersResponse
	clustersErr       error
	locationsResponse *locationpb.ListLocationsResponse
	locationsErr      error

	gotClustersParent string
	gotLocationsName  string
}

func (s *fakeManagedKafkaServer) ListClusters(_ context.Context, req *managedkafkapb.ListClustersRequest) (*managedkafkapb.ListClustersResponse, error) {
	s.gotClustersParent = req.GetParent()
	if s.clustersErr != nil {
		return nil, s.clustersErr
	}
	return s.clustersResponse, nil
}

func (s *fakeManagedKafkaServer) ListLocations(_ context.Context, req *locationpb.ListLocationsRequest) (*locationpb.ListLocationsResponse, error) {
	s.gotLocationsName = req.GetName()
	if s.locationsErr != nil {
		return nil, s.locationsErr
	}
	return s.locationsResponse, nil
}

func newTestManagedKafkaClient(t *testing.T, server *fakeManagedKafkaServer) *ManagedKafka {
	t.Helper()

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	managedkafkapb.RegisterManagedKafkaServer(grpcServer, server)
	locationpb.RegisterLocationsServer(grpcServer, server)

	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			t.Errorf("failed to serve managed kafka test server: %v", err)
		}
	}()

	t.Cleanup(func() {
		grpcServer.Stop()
	})

	client, err := managedkafka.NewClient(
		t.Context(),
		option.WithEndpoint(listener.Addr().String()),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})

	return newManagedKafka(client)
}

func TestManagedKafkaClientListLocations(t *testing.T) {
	tests := map[string]struct {
		server  *fakeManagedKafkaServer
		want    []string
		wantErr bool
	}{
		"filters global locations": {
			server: &fakeManagedKafkaServer{
				locationsResponse: &locationpb.ListLocationsResponse{
					Locations: []*locationpb.Location{
						{Name: "projects/test-project/locations/us-central1"},
						{Name: "projects/test-project/locations/global"},
						{Name: "projects/test-project/locations/europe-west1"},
					},
				},
			},
			want:    []string{"us-central1", "europe-west1"},
			wantErr: false,
		},
		"propagates api errors": {
			server: &fakeManagedKafkaServer{
				locationsErr: status.Error(codes.Internal, "boom"),
			},
			want:    nil,
			wantErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			client := newTestManagedKafkaClient(t, test.server)

			got, err := client.listLocations(t.Context(), "test-project")

			assert.Equal(t, test.wantErr, err != nil)
			assert.Equal(t, "projects/test-project", test.server.gotLocationsName)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestManagedKafkaClientListClusters(t *testing.T) {
	tests := map[string]struct {
		server  *fakeManagedKafkaServer
		want    []*managedkafkapb.Cluster
		wantErr bool
	}{
		"lists clusters for a location": {
			server: &fakeManagedKafkaServer{
				clustersResponse: &managedkafkapb.ListClustersResponse{
					Clusters: []*managedkafkapb.Cluster{
						{Name: "projects/test-project/locations/us-central1/clusters/cluster-a"},
						{Name: "projects/test-project/locations/us-central1/clusters/cluster-b"},
					},
				},
			},
			want: []*managedkafkapb.Cluster{
				{Name: "projects/test-project/locations/us-central1/clusters/cluster-a"},
				{Name: "projects/test-project/locations/us-central1/clusters/cluster-b"},
			},
			wantErr: false,
		},
		"propagates api errors": {
			server: &fakeManagedKafkaServer{
				clustersErr: status.Error(codes.Internal, "boom"),
			},
			want:    nil,
			wantErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			client := newTestManagedKafkaClient(t, test.server)

			got, err := client.listClusters(t.Context(), "test-project", "us-central1")

			assert.Equal(t, test.wantErr, err != nil)
			assert.Equal(t, "projects/test-project/locations/us-central1", test.server.gotClustersParent)
			assert.Equal(t, test.want, got)
		})
	}
}
