package cloudsql

import (
	"fmt"
	"log"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"
)

type Config struct {
	ProjectId      string
	ScrapeInterval time.Duration
}

type Collector struct {
	gcpClient client.Client
	config    *Config
}

func New(config *Config, gcpClient client.Client) (*Collector, error) {
	return &Collector{
		gcpClient: gcpClient,
		config:    config,
	}, nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	return nil
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	instances, err := c.gcpClient.ListSQLInstances(c.config.ProjectId)
	if err != nil {
		return err
	}
	for _, instance := range instances {
		fmt.Println(instance)
	}
	return nil
}

func (c *Collector) Name() string {
	return "cloudsql"
}

func (c *Collector) Register(registry provider.Registry) error {
	log.Printf("Registering CloudSQL metrics")
	return nil
}

func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	return 0
}
