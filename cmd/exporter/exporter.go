package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/version"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/cmd/exporter/config"
	"github.com/grafana/cloudcost-exporter/cmd/exporter/web"
	"github.com/grafana/cloudcost-exporter/pkg/aws"
	"github.com/grafana/cloudcost-exporter/pkg/google"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

var (
	csp     provider.Provider
	cfg     config.Config
	timeout time.Duration = 30 * time.Second
)

func ProviderFlags(fs *flag.FlagSet, awsProfiles, gcpProjects, awsServices, gcpServices *config.StringSliceFlag) {
	fs.Var(awsProfiles, "aws.profile", "AWS profile(s).")
	// TODO: RENAME THIS TO JUST PROJECTS
	fs.Var(gcpProjects, "gcp.bucket-projects", "GCP project(s).")
	fs.Var(awsServices, "aws.services", "AWS service(s).")
	fs.Var(gcpServices, "gcp.services", "GCP service(s).")
}

func init() {
	ProviderFlags(flag.CommandLine, &cfg.Providers.AWS.Profiles, &cfg.Providers.GCP.Projects, &cfg.Providers.AWS.Services, &cfg.Providers.GCP.Services)
	targetProvider := flag.String("provider", "aws", "aws or gcp")
	flag.DurationVar(&cfg.Collector.ScrapeInterval, "scrape-interval", 1*time.Hour, "Scrape interval")
	flag.StringVar(&cfg.Providers.AWS.Region, "aws.region", "", "AWS region")
	flag.StringVar(&cfg.ProjectID, "project-id", "ops-tools-1203", "Project ID to target.")
	flag.StringVar(&cfg.Server.Address, "server.address", ":8080", "Default address for the server to listen on.")
	flag.StringVar(&cfg.Server.Path, "server.path", "/metrics", "Default path for the server to listen on.")
	flag.IntVar(&cfg.Providers.GCP.DefaultGCSDiscount, "gcp.default-discount", 19, "GCP default discount")
	flag.Parse()

	log.Print("Version ", version.Info())
	log.Print("Build Context ", version.BuildContext())

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
}

func main() {
	mux := http.NewServeMux()
	ctx, cancel := signal.NotifyContext(context.TODO(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	defer cancel()

	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewBuildInfoCollector(),
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		version.NewCollector(cloudcost_exporter.ExporterName),
		csp,
	)
	if err := csp.RegisterCollectors(registry); err != nil {
		log.Fatalf("Error registering collectors: %s", err)
	}

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})

	// landing page
	mux.HandleFunc("/", web.HomePageHandler(cfg.Server.Path))

	// CollectMetrics http server for prometheus
	mux.Handle(cfg.Server.Path, handler)

	server := &http.Server{Addr: cfg.Server.Address, Handler: mux}

	errorChannel := make(chan error)

	go func() {
		log.Printf("Listening on %s%s", cfg.Server.Address, cfg.Server.Path)
		errorChannel <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Print("shutting down server")
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Fatalf("error shutting down server: %v", err)
		}

	case err := <-errorChannel:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("error running server: %v", err)
		}
	}
}
