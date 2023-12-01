package aws

import (
	"fmt"
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/cloudcost-exporter/pkg/aws/s3"
)

type Config struct {
	Services       []string
	Region         string
	Profile        string
	ScrapeInterval time.Duration
}

type AWS struct {
	Config     *Config
	collectors []prometheus.Collector
}

var services = []string{"S3"}

func New(config *Config) (*AWS, error) {
	var collectors []prometheus.Collector
	for _, service := range services {
		switch service {
		case "S3":
			collector, err := s3.New(config.Region, config.Profile, config.ScrapeInterval)
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
	for _, c := range a.collectors {
		if err := registry.Register(c); err != nil {
			return fmt.Errorf("error registering collector: %w", err)
		}
	}
	return nil
}

func (a *AWS) CollectMetrics() error {
	return nil
}
