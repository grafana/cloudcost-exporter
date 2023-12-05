package aws

import (
	"context"
	"fmt"
	"log"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"

	"github.com/grafana/cloudcost-exporter/pkg/aws/s3"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

type Config struct {
	Services       []string
	Region         string
	Profile        string
	ScrapeInterval time.Duration
}

type AWS struct {
	Config     *Config
	collectors []provider.Collector
}

var services = []string{"S3"}

func New(config *Config) (*AWS, error) {
	var collectors []provider.Collector
	for _, service := range services {
		switch service {
		case "S3":
			// There are two scenarios:
			// 1. Running locally, the user must pass in a region and profile to use
			// 2. Running within an EC2 instance and the region and profile can be derived
			// I'm going to use the AWS SDK to handle this for me. If the user has provided a region and profile, it will use that.
			// If not, it will use the EC2 instance metadata service to determine the region and credentials.
			// This is the same logic that the AWS CLI uses, so it should be fine.
			options := []func(*awsconfig.LoadOptions) error{awsconfig.WithEC2IMDSRegion()}
			if config.Region != "" {
				options = append(options, awsconfig.WithRegion(config.Region))
			}
			if config.Profile != "" {
				options = append(options, awsconfig.WithSharedConfigProfile(config.Profile))
			}
			ac, err := awsconfig.LoadDefaultConfig(context.Background(), options...)
			if err != nil {
				return nil, err
			}

			client := costexplorer.NewFromConfig(ac)
			collector, err := s3.New(config.ScrapeInterval, client)
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

func (a *AWS) RegisterCollectors(registry provider.Registry) error {
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
