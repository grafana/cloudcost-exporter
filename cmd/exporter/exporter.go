package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"

	dto "github.com/prometheus/client_model/go"

	"github.com/grafana/cloudcost-exporter/pkg/aws"
	"github.com/grafana/cloudcost-exporter/pkg/collector"
	"github.com/grafana/cloudcost-exporter/pkg/google"
)

func main() {
	provider := flag.String("provider", "aws", "AWS or GCP")
	scrapeInterval := flag.Duration("scrape-interval", 1*time.Hour, "Scrape interval")
	awsRegion := flag.String("aws.region", "", "AWS region")
	awsProfile := flag.String("aws.profile", "", "AWS profile")
	projectId := flag.String("project-id", "ops-tools-1203", "Project ID to target.")
	gcpDefaultDiscount := flag.Int("gcp.default-discount", 19, "GCP default discount")
	// TODO: Deprecate this flag in favor of `gcp.projects`
	gcpProjects := flag.String("gcp.bucket-projects", "", "GCP projects to fetch resources from. Must be a list of comma-separated project IDs. If no value is passed it, defaults to the project ID passed via --project-id.")
	gcpServices := flag.String("gcp.services", "GCS", "GCP services to scrape. Must be a list of comma-separated service names.")
	awsServices := flag.String("aws.services", "S3", "AWS services to scrape. Must be a list of comma-separated service names.")
	flag.Parse()

	log.Print("Version ", version.Info())
	log.Print("Build Context ", version.BuildContext())
	var gatherer prometheus.GathererFunc

	var csp collector.CSP
	var err error
	switch *provider {
	case "aws":
		csp, err = aws.NewAWS(&aws.Config{
			Region:         *awsRegion,
			Profile:        *awsProfile,
			ScrapeInterval: *scrapeInterval,
			Services:       strings.Split(*awsServices, ","),
		})

	case "gcp":
		csp, err = google.NewGCP(&google.Config{
			ProjectId:       *projectId,
			Region:          *awsRegion,
			Projects:        *gcpProjects,
			DefaultDiscount: *gcpDefaultDiscount,
			ScrapeInterval:  *scrapeInterval,
			Services:        strings.Split(*gcpServices, ","),
		})
	default:
		err = fmt.Errorf("unknown provider")
	}

	if err != nil {
		log.Printf("Error setting up provider %s: %s", *provider, err)
		os.Exit(1)
	}

	gatherer, err = gathererFunc(csp)
	if err != nil {
		log.Printf("Error setting up gatherer: %s", err)
		os.Exit(1)
	}

	// Collect http server for prometheus
	http.Handle("/metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	}))

	// TODO: Add proper shutdown sequence here. IE, listen for sigint and sigterm and shutdown gracefully.
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Printf("Error listening and serving: %s", err)
		os.Exit(1)
	}
}

func gathererFunc(csp collector.CSP) (prometheus.GathererFunc, error) {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewBuildInfoCollector(),
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		version.NewCollector("cloudcost_exporter"),
	)
	if err := csp.RegisterCollectors(reg); err != nil {
		return nil, err
	}

	return func() ([]*dto.MetricFamily, error) {
		if err := csp.CollectMetrics(); err != nil {
			return nil, err
		}
		return reg.Gather()
	}, nil
}
