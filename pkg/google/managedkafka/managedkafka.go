package managedkafka

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync"
	"time"

	managedkafkapb "cloud.google.com/go/managedkafka/apiv1/managedkafkapb"
	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"
)

const (
	subsystem                  = "gcp_managedkafka"
	collectorName              = "managedkafka"
	priceRefreshInterval       = 24 * time.Hour
	locationCollectLimit       = 10
	bytesPerGiB          int64 = 1024 * 1024 * 1024
)

var (
	ComputeHourlyGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"compute_hourly_rate_usd_per_hour",
		"Hourly compute cost of a GCP Managed Kafka cluster. Cost represented in USD/hour",
		[]string{"project", "region", "cluster_name", "cluster"},
	)
	StorageHourlyGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"storage_hourly_rate_usd_per_hour",
		"Hourly local storage cost of a GCP Managed Kafka cluster. Cost represented in USD/hour",
		[]string{"project", "region", "cluster_name", "cluster"},
	)
)

type Config struct {
	Projects       string
	ScrapeInterval time.Duration
}

type Collector struct {
	projects   []string
	gcpClient  client.Client
	pricingMap *pricingMap
	logger     *slog.Logger
}

type clusterPricingData struct {
	project     string
	region      string
	clusterName string
	cluster     string
	vcpuCount   int64
	memoryGiB   float64
}

func New(ctx context.Context, config *Config, logger *slog.Logger, gcpClient client.Client) (*Collector, error) {
	logger = logger.With("collector", collectorName)

	pm, err := newPricingMap(ctx, logger, gcpClient)
	if err != nil {
		return nil, fmt.Errorf("failed to initialise Managed Kafka pricing: %w", err)
	}

	go func() {
		ticker := time.NewTicker(priceRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := pm.populate(ctx); err != nil {
					logger.Error("failed to refresh Managed Kafka pricing SKUs", "error", err)
				}
			}
		}
	}()

	return &Collector{
		projects:   splitProjects(config.Projects),
		gcpClient:  gcpClient,
		pricingMap: pm,
		logger:     logger,
	}, nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- ComputeHourlyGaugeDesc
	ch <- StorageHourlyGaugeDesc
	return nil
}

func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	clusters, err := c.listClusters(ctx)
	if err != nil {
		return err
	}

	snap := c.pricingMap.Snapshot()

	for _, cluster := range clusters {
		computePrice, ok := snap.Price(cluster.region, computeComponent)
		if !ok {
			c.logger.Warn("skipping Managed Kafka cluster with unpriceable compute",
				"project", cluster.project,
				"region", cluster.region,
				"cluster", cluster.cluster)
			continue
		}

		storagePrice, ok := snap.Price(cluster.region, localStorageComponent)
		if !ok {
			c.logger.Warn("skipping Managed Kafka cluster with unpriceable storage",
				"project", cluster.project,
				"region", cluster.region,
				"cluster", cluster.cluster)
			continue
		}

		// Managed Kafka prices compute in DCUs where 1 vCPU = 0.6 DCU and 1 GiB RAM = 0.1 DCU.
		computeHourlyRate := computePrice * ((0.6 * float64(cluster.vcpuCount)) + (0.1 * cluster.memoryGiB))
		storageHourlyRate := storagePrice * (100 * float64(cluster.vcpuCount))

		ch <- prometheus.MustNewConstMetric(
			ComputeHourlyGaugeDesc,
			prometheus.GaugeValue,
			computeHourlyRate,
			cluster.project,
			cluster.region,
			cluster.clusterName,
			cluster.cluster,
		)
		ch <- prometheus.MustNewConstMetric(
			StorageHourlyGaugeDesc,
			prometheus.GaugeValue,
			storageHourlyRate,
			cluster.project,
			cluster.region,
			cluster.clusterName,
			cluster.cluster,
		)
	}

	return nil
}

func (c *Collector) Name() string {
	return collectorName
}

func (c *Collector) Register(provider.Registry) error {
	return nil
}

func (c *Collector) listClusters(ctx context.Context) ([]clusterPricingData, error) {
	var allClusters []clusterPricingData

	for _, project := range c.projects {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		locations, err := c.gcpClient.ListManagedKafkaLocations(ctx, project)
		if err != nil {
			c.logger.Error("error listing Managed Kafka locations for project", "project", project, "error", err)
			continue
		}

		projectClusters := make([]clusterPricingData, 0)
		var mu sync.Mutex

		eg, egCtx := errgroup.WithContext(ctx)
		eg.SetLimit(locationCollectLimit)
		for _, location := range locations {
			location := location
			eg.Go(func() error {
				clusters, err := c.gcpClient.ListManagedKafkaClusters(egCtx, project, location)
				if err != nil {
					c.logger.Error("error listing Managed Kafka clusters for location",
						"project", project,
						"location", location,
						"error", err)
					return nil
				}

				parsedClusters := make([]clusterPricingData, 0, len(clusters))
				for _, cluster := range clusters {
					clusterData, err := buildClusterPricingData(project, cluster)
					if err != nil {
						c.logger.Warn("skipping unsupported or incomplete Managed Kafka cluster",
							"project", project,
							"location", location,
							"cluster", cluster.GetName(),
							"error", err)
						continue
					}
					parsedClusters = append(parsedClusters, clusterData)
				}

				mu.Lock()
				projectClusters = append(projectClusters, parsedClusters...)
				mu.Unlock()
				return nil
			})
		}
		_ = eg.Wait()

		allClusters = append(allClusters, projectClusters...)
	}

	return allClusters, nil
}

func buildClusterPricingData(project string, cluster *managedkafkapb.Cluster) (clusterPricingData, error) {
	if cluster == nil {
		return clusterPricingData{}, fmt.Errorf("cluster is nil")
	}

	clusterResourceName := cluster.GetName()
	if clusterResourceName == "" {
		return clusterPricingData{}, fmt.Errorf("cluster name is missing")
	}

	clusterName := path.Base(clusterResourceName)
	if clusterName == "." || clusterName == "" {
		return clusterPricingData{}, fmt.Errorf("cluster name is invalid")
	}

	region, err := regionFromResourceName(clusterResourceName)
	if err != nil {
		return clusterPricingData{}, err
	}

	capacity := cluster.GetCapacityConfig()
	if capacity == nil {
		return clusterPricingData{}, fmt.Errorf("capacity config is missing")
	}
	if capacity.GetVcpuCount() <= 0 {
		return clusterPricingData{}, fmt.Errorf("vcpu count is missing")
	}
	if capacity.GetMemoryBytes() <= 0 {
		return clusterPricingData{}, fmt.Errorf("memory bytes are missing")
	}

	return clusterPricingData{
		project:     project,
		region:      region,
		clusterName: clusterName,
		cluster:     clusterResourceName,
		vcpuCount:   capacity.GetVcpuCount(),
		memoryGiB:   float64(capacity.GetMemoryBytes()) / float64(bytesPerGiB),
	}, nil
}

func regionFromResourceName(resourceName string) (string, error) {
	parts := strings.Split(resourceName, "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "locations" && parts[i+1] != "" {
			return parts[i+1], nil
		}
	}
	return "", fmt.Errorf("location missing from resource name")
}

func splitProjects(projects string) []string {
	rawProjects := strings.Split(projects, ",")
	trimmedProjects := make([]string, 0, len(rawProjects))
	for _, project := range rawProjects {
		project = strings.TrimSpace(project)
		if project == "" {
			continue
		}
		trimmedProjects = append(trimmedProjects, project)
	}
	return trimmedProjects
}
