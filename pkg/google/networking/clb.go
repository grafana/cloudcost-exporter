package clb

import (
	"log"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/google/metrics"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	collectorName = "CLB"
)

type Collector struct {
	interval   time.Duration
	nextScrape time.Time
	metrics    *metrics.Metrics
	gcpClient  client.Client
}

func (c *Collector) Describe(_ chan<- *prometheus.Desc) error {
	return nil
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	c.CollectMetrics(ch)
	return nil
}

type Config struct {
	ScrapeInterval time.Duration
}

func New(config *Config, gcpClient client.Client) (*Collector, error) {
	return &Collector{
		interval:   config.ScrapeInterval,
		nextScrape: time.Now().Add(-config.ScrapeInterval),
		metrics:    metrics.NewMetrics(),
		gcpClient:  gcpClient,
	}, nil
}

func (c *Collector) Name() string {
	return collectorName
}

func (c *Collector) Register(registry provider.Registry) error {
	log.Printf("Registering CLB metrics")
	//registry.MustRegister(c.metrics.CLB)
	return nil
}

func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	log.Printf("Collecting CLB metrics")
	now := time.Now()

	// If the nextScrape time is in the future, return nil and do not scrape
	// Billing API calls are free in GCP, just use this logic so metrics are similar to AWS
	if c.nextScrape.After(now) {
		// TODO: We should stuff in logic here to update pricing data if it's been more than 24 hours
		return 1
	}
	c.nextScrape = time.Now().Add(c.interval)
	c.metrics.NextScrapeGauge.Set(float64(c.nextScrape.Unix()))

	serviceName, err := c.gcpClient.GetServiceName(c.ctx, "Cloud Load Balancer")
	if err != nil {
		log.Printf("Error getting service name: %v", err)
		return 0
	}
	return c.gcpClient.ExportGCPCostData(c.ctx, serviceName, c.metrics)
}
