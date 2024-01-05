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
	"github.com/grafana/cloudcost-exporter/pkg/google"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

func ProviderFlags(fs *flag.FlagSet, awsProfiles, gcpProjects, awsServices, gcpServices *config.StringSliceFlag) {
	fs.Var(awsProfiles, "aws.profile", "AWS profile(s).")
	// TODO: RENAME THIS TO JUST PROJECTS
	fs.Var(gcpProjects, "gcp.bucket-projects", "GCP project(s).")
	fs.Var(awsServices, "aws.services", "AWS service(s).")
	fs.Var(gcpServices, "gcp.services", "GCP service(s).")
}

var (
	UseInstrumentMetrics bool = false
)

func main() {
	var cfg config.Config
	ProviderFlags(flag.CommandLine, &cfg.Providers.AWS.Profiles, &cfg.Providers.GCP.Projects, &cfg.Providers.AWS.Services, &cfg.Providers.GCP.Services)
	targetProvider := flag.String("provider", "aws", "aws or gcp")
	flag.DurationVar(&cfg.Collector.ScrapeInterval, "scrape-interval", 1*time.Hour, "Scrape interval")
	flag.StringVar(&cfg.Providers.AWS.Region, "aws.region", "", "AWS region")
	flag.StringVar(&cfg.ProjectID, "project-id", "ops-tools-1203", "Project ID to target.")
	flag.StringVar(&cfg.Server.Address, "server.address", ":8080", "Default address for the server to listen on.")
	flag.StringVar(&cfg.Server.Path, "server.path", "/metrics", "Default path for the server to listen on.")
	flag.IntVar(&cfg.Providers.GCP.DefaultGCSDiscount, "gcp.default-discount", 19, "GCP default discount")
	flag.BoolVar(&UseInstrumentMetrics, "use-instrument-metrics-feature", false, "Use prometheus collector to collect metrics")
	flag.Parse()

	log.Print("Version ", version.Info())
	log.Print("Build Context ", version.BuildContext())
	var gatherer prometheus.GathererFunc

	var csp provider.Provider
	var err error
	switch *targetProvider {
	case "aws":
		csp, err = aws.New(&aws.Config{
			Region:         cfg.Providers.AWS.Region,
			Profile:        cfg.Providers.AWS.Profiles.String(),
			ScrapeInterval: cfg.Collector.ScrapeInterval,
			Services:       strings.Split(cfg.Providers.AWS.Services.String(), ","),
		})

	case "gcp":
		csp, err = google.New(&google.Config{
			ProjectId:       cfg.ProjectID,
			Region:          cfg.Providers.GCP.Region,
			Projects:        cfg.Providers.GCP.Projects.String(),
			DefaultDiscount: cfg.Providers.GCP.DefaultGCSDiscount,
			ScrapeInterval:  cfg.Collector.ScrapeInterval,
			Services:        strings.Split(cfg.Providers.GCP.Services.String(), ","),
		})
	default:
		err = fmt.Errorf("unknown provider")
	}

	if err != nil {
		log.Printf("Error setting up provider %s: %s", *targetProvider, err)
		os.Exit(1)
	}

	var handler http.Handler
	if UseInstrumentMetrics {
		registry := prometheus.NewRegistry()
		registry.MustRegister(
			collectors.NewBuildInfoCollector(),
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
			version.NewCollector("cloudcost_exporter"),
			csp,
		)
		if err := csp.RegisterCollectors(registry); err != nil {
			log.Printf("Error registering collectors: %s", err)
			os.Exit(1)
		}

		handler = promhttp.HandlerFor(registry, promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		})
	} else {
		gatherer, err = gathererFunc(csp)
		if err != nil {
			log.Printf("Error setting up gatherer: %s", err)
			os.Exit(1)
		}
		handler = promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{
			EnableOpenMetrics: true,
		})
	}

	// CollectMetrics http server for prometheus
	http.Handle(cfg.Server.Path, handler)

	log.Printf("Listening on %s:%s", cfg.Server.Address, cfg.Server.Path)
	if err = http.ListenAndServe(cfg.Server.Address, nil); err != nil {
		log.Printf("Error listening and serving: %s", err)
		os.Exit(1)
	}
}

func gathererFunc(csp provider.Provider) (prometheus.GathererFunc, error) {
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
