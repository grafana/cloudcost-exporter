package client

import (
	"context"
	"errors"
	"fmt"
	"strings"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/googleapis/gax-go/v2"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"google.golang.org/api/iterator"
)

var (
	storageClasses = []string{"Standard", "Regional", "Nearline", "Coldline", "Archive"}
	baseRegions    = []string{"asia", "eu", "us", "asia1", "eur4", "nam4"}
)

//go:generate mockgen -source=region.go -destination mocks/region.go

type RegionsClient interface {
	List(ctx context.Context, req *computepb.ListRegionsRequest, opts ...gax.CallOption) *compute.RegionIterator
}

type Region struct {
	projectId    string
	discount     int
	regionClient RegionsClient
}

func newRegion(projectId string, discount int, regionClient RegionsClient) *Region {
	return &Region{
		projectId:    projectId,
		discount:     discount,
		regionClient: regionClient,
	}
}

func (r *Region) exportRegionalDiscounts(ctx context.Context, m *metrics.Metrics) error {
	req := &computepb.ListRegionsRequest{
		Project: r.projectId,
	}
	it := r.regionClient.List(ctx, req)
	regions := make([]string, 0)
	for {
		resp, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return fmt.Errorf("error getting regions: %w", err)
		}
		regions = append(regions, *resp.Name)
	}
	percentDiscount := float64(r.discount) / 100.0
	for _, storageClass := range storageClasses {
		for _, region := range regions {
			m.StorageDiscountGauge.WithLabelValues(region, strings.ToUpper(storageClass)).Set(percentDiscount)
		}
		// Base Regions are specific to `MULTI_REGION` buckets that do not have a specific region
		// Breakdown for buckets with these regions: https://ops.grafana-ops.net/explore?panes=%7B%229oU%22:%7B%22datasource%22:%22000000134%22,%22queries%22:%5B%7B%22refId%22:%22A%22,%22expr%22:%22sum%28count%20by%20%28bucket_name%29%20%28stackdriver_gcs_bucket_storage_googleapis_com_storage_total_bytes%7Blocation%3D~%5C%22asia%7Ceu%7Cus%5C%22%7D%29%29%22,%22range%22:true,%22instant%22:true,%22datasource%22:%7B%22type%22:%22prometheus%22,%22uid%22:%22000000134%22%7D,%22editorMode%22:%22code%22,%22legendFormat%22:%22__auto%22%7D,%7B%22refId%22:%22B%22,%22expr%22:%22sum%28count%20by%20%28bucket_name%29%20%28stackdriver_gcs_bucket_storage_googleapis_com_storage_total_bytes%7Blocation%21~%5C%22asia%7Ceu%7Cus%5C%22%7D%29%29%22,%22range%22:true,%22instant%22:true,%22datasource%22:%7B%22type%22:%22prometheus%22,%22uid%22:%22000000134%22%7D,%22editorMode%22:%22code%22,%22legendFormat%22:%22__auto%22%7D%5D,%22range%22:%7B%22from%22:%22now-6h%22,%22to%22:%22now%22%7D%7D%7D&schemaVersion=1&orgId=1
		for _, region := range baseRegions {
			if storageClass == "Regional" {
				// This is a hack to align storage classes with stackdriver_exporter
				storageClass = "MULTI_REGIONAL"
			}
			m.StorageDiscountGauge.WithLabelValues(region, strings.ToUpper(storageClass)).Set(percentDiscount)
		}
	}

	return nil
}
