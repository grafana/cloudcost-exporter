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

	"github.com/grafana/cloudcost-exporter/cmd/exporter/config"
	"github.com/grafana/cloudcost-exporter/pkg/aws"
	"github.com/grafana/cloudcost-exporter/pkg/collector"
	"github.com/grafana/cloudcost-exporter/pkg/google"
)

func ProviderFlags(fs *flag.FlagSet, awsProfiles, gcpProjects, awsServices, gcpServices *config.StringSliceFlag) {
	fs.Var(awsProfiles, "aws.profile", "AWS profile(s).")
	// TODO: RENAME THIS TO JUST PROJECTS
	fs.Var(gcpProjects, "gcp.bucket-projects", "GCP project(s).")
	fs.Var(awsServices, "aws.services", "AWS service(s).")
	fs.Var(gcpServices, "gcp.services", "GCP service(s).")
}

func main() {
	var cfg config.Config
	ProviderFlags(flag.CommandLine, &cfg.Providers.AWS.Profiles, &cfg.Providers.GCP.Projects, &cfg.Providers.AWS.Services, &cfg.Providers.GCP.Services)
	provider := flag.String("provider", "aws", "AWS or GCP")
	flag.DurationVar(&cfg.Collector.ScrapeInterval, "scrape-interval", 1*time.Hour, "Scrape interval")
	flag.StringVar(&cfg.Providers.AWS.Region, "aws.region", "", "AWS region")
	flag.StringVar(&cfg.ProjectID, "project-id", "ops-tools-1203", "Project ID to target.")
	flag.IntVar(&cfg.Providers.GCP.DefaultDiscount, "gcp.default-discount", 19, "GCP default discount")
	flag.Parse()

	log.Print("Version ", version.Info())
	log.Print("Build Context ", version.BuildContext())
	var gatherer prometheus.GathererFunc

	var csp collector.CSP
	var err error
	switch *provider {
	case "aws":
		csp, err = aws.NewAWS(&aws.Config{
			Region:         cfg.Providers.AWS.Region,
			Profile:        cfg.Providers.AWS.Profiles.String(),
			ScrapeInterval: cfg.Collector.ScrapeInterval,
			Services:       strings.Split(cfg.Providers.AWS.Services.String(), ","),
		})

	case "gcp":
		csp, err = google.NewGCP(&google.Config{
			ProjectId:       cfg.ProjectID,
			Region:          cfg.Providers.GCP.Region,
			Projects:        cfg.Providers.GCP.Projects.String(),
			DefaultDiscount: cfg.Providers.GCP.DefaultDiscount,
			ScrapeInterval:  cfg.Collector.ScrapeInterval,
			Services:        strings.Split(cfg.Providers.GCP.Services.String(), ","),
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
