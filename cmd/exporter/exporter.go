package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
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
	timeout time.Duration
)

func ProviderFlags(fs *flag.FlagSet, cfg *config.Config) {
	fs.Var(&cfg.Providers.AWS.Profiles, "aws.profile", "AWS profile(s).")
	// TODO: RENAME THIS TO JUST PROJECTS
	fs.Var(&cfg.Providers.GCP.Projects, "gcp.bucket-projects", "GCP project(s).")
	fs.Var(&cfg.Providers.AWS.Services, "aws.services", "AWS service(s).")
	fs.Var(&cfg.Providers.GCP.Services, "gcp.services", "GCP service(s).")
	flag.StringVar(&cfg.Providers.AWS.Region, "aws.region", "", "AWS region")
	flag.StringVar(&cfg.ProjectID, "project-id", "ops-tools-1203", "Project ID to target.")
	flag.IntVar(&cfg.Providers.GCP.DefaultGCSDiscount, "gcp.default-discount", 19, "GCP default discount")
}

func init() {
	targetProvider := *flag.String("provider", "aws", "aws or gcp")
	ProviderFlags(flag.CommandLine, &cfg)
	flag.DurationVar(&cfg.Collector.ScrapeInterval, "scrape-interval", 1*time.Hour, "Scrape interval")
	flag.DurationVar(&timeout, "server-timeout", 30*time.Second, "Server timeout")
	flag.StringVar(&cfg.Server.Address, "server.address", ":8080", "Default address for the server to listen on.")
	flag.StringVar(&cfg.Server.Path, "server.path", "/metrics", "Default path for the server to listen on.")
	flag.Parse()

	log.Printf("Version %s", version.Info())
	log.Printf("Build Context %s", version.BuildContext())

	var err error
	switch targetProvider {
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
		log.Fatalf("Error setting up provider %s: %s", targetProvider, err)
	}
}

func createPromRegistryHandler() http.Handler {
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewBuildInfoCollector(),
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		version.NewCollector(cloudcost_exporter.ExporterName),
		csp,
	)
	err := csp.RegisterCollectors(registry)
	if err != nil {
		log.Fatalf("Error registering collectors: %s", err)
	}
	// CollectMetrics http server for prometheus
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

func main() {
	mux := http.NewServeMux()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
	defer cancel()

	mux.HandleFunc("/", web.HomePageHandler(cfg.Server.Path)) // landing page
	mux.Handle(cfg.Server.Path, createPromRegistryHandler())  // prom metrics handler

	server := &http.Server{Addr: cfg.Server.Address, Handler: mux}
	errorChan := make(chan error)

	go func() {
		log.Printf("Listening on %s%s", cfg.Server.Address, cfg.Server.Path)
		errorChan <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Print("shutting down server")
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		err := server.Shutdown(ctx)
		if err != nil {
			log.Fatalf("error shutting down server: %v", err)
		}
	case err := <-errorChan:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("error running server: %v", err)
		}
	default:
		log.Fatalf("unknown error occurred while running server")
	}
}
