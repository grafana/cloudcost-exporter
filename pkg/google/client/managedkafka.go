package client

import (
	"context"
	"errors"
	"fmt"
	"path"

	managedkafka "cloud.google.com/go/managedkafka/apiv1"
	managedkafkapb "cloud.google.com/go/managedkafka/apiv1/managedkafkapb"
	"github.com/googleapis/gax-go/v2"
	"google.golang.org/api/iterator"
	locationpb "google.golang.org/genproto/googleapis/cloud/location"
)

type ManagedKafkaClient interface {
	ListClusters(context.Context, *managedkafkapb.ListClustersRequest, ...gax.CallOption) *managedkafka.ClusterIterator
	ListLocations(context.Context, *locationpb.ListLocationsRequest, ...gax.CallOption) *managedkafka.LocationIterator
}

type ManagedKafka struct {
	client ManagedKafkaClient
}

func newManagedKafka(client ManagedKafkaClient) *ManagedKafka {
	return &ManagedKafka{client: client}
}

func (m *ManagedKafka) listLocations(ctx context.Context, project string) ([]string, error) {
	if m == nil || m.client == nil {
		return nil, fmt.Errorf("managed kafka client not initialized")
	}

	req := &locationpb.ListLocationsRequest{
		Name: fmt.Sprintf("projects/%s", project),
	}
	it := m.client.ListLocations(ctx, req)

	var locations []string
	for {
		location, err := it.Next()
		if err != nil {
			if errors.Is(err, iterator.Done) {
				break
			}
			return nil, err
		}

		locations = append(locations, path.Base(location.GetName()))
	}

	return locations, nil
}

func (m *ManagedKafka) listClusters(ctx context.Context, project, location string) ([]*managedkafkapb.Cluster, error) {
	if m == nil || m.client == nil {
		return nil, fmt.Errorf("managed kafka client not initialized")
	}

	req := &managedkafkapb.ListClustersRequest{
		Parent: fmt.Sprintf("projects/%s/locations/%s", project, location),
	}
	it := m.client.ListClusters(ctx, req)

	var clusters []*managedkafkapb.Cluster
	for {
		cluster, err := it.Next()
		if err != nil {
			if errors.Is(err, iterator.Done) {
				break
			}
			return nil, err
		}
		clusters = append(clusters, cluster)
	}

	return clusters, nil
}
