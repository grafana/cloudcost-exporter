package aws

import (
	"fmt"
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/deployment_tools/docker/cloudcost-exporter/pkg/aws/s3"
	"github.com/grafana/deployment_tools/docker/cloudcost-exporter/pkg/collector"
)

type Config struct {
	Services       []string
	Region         string
	Profile        string
	ScrapeInterval time.Duration
}

type AWS struct {
	Config     *Config
	collectors []collector.Collector
}

var services = []string{"S3"}

func NewAWS(config *Config) (*AWS, error) {
	var collectors []collector.Collector
	for _, service := range services {
		switch service {
		case "S3":
			collector, err := s3.NewCollector(config.Region, config.Profile, config.ScrapeInterval)
			if err != nil {
				return nil, fmt.Errorf("error creating s3 collector: %w", err)
			}
			collectors = append(collectors, collector)
		default:
			log.Printf("Unknown service %s", service)
			continue
		}
	}
	return &AWS{
		Config:     config,
		collectors: collectors,
	}, nil
}

func (a *AWS) RegisterCollectors(registry *prometheus.Registry) error {
	log.Printf("Registering %d collectors for AWS", len(a.collectors))
	for _, c := range a.collectors {
		if err := c.Register(registry); err != nil {
			return err
		}
	}
	return nil
}

func (a *AWS) CollectMetrics() error {
	log.Printf("Collecting metrics for %d collectors for AWS", len(a.collectors))
	for _, c := range a.collectors {
		if err := c.Collect(); err != nil {
			return err
		}
	}
	return nil
}
