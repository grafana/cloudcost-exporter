package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	cversion "github.com/prometheus/common/version"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/cmd/exporter/config"
	"github.com/grafana/cloudcost-exporter/cmd/exporter/web"
	"github.com/grafana/cloudcost-exporter/pkg/aws"
	"github.com/grafana/cloudcost-exporter/pkg/google"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

func providerFlags(fs *flag.FlagSet, cfg *config.Config) {
	flag.StringVar(&cfg.Provider, "provider", "aws", "aws or gcp")
	fs.StringVar(&cfg.Providers.AWS.Profile, "aws.profile", "", "AWS Profile to authenticate with.")
	// TODO: RENAME THIS TO JUST PROJECTS
	fs.Var(&cfg.Providers.GCP.Projects, "gcp.bucket-projects", "GCP project(s).")
	fs.Var(&cfg.Providers.AWS.Services, "aws.services", "AWS service(s).")
	fs.Var(&cfg.Providers.GCP.Services, "gcp.services", "GCP service(s).")
	flag.StringVar(&cfg.Providers.AWS.Region, "aws.region", "", "AWS region")
	// TODO - PUT PROJECT-ID UNDER GCP
	flag.StringVar(&cfg.ProjectID, "project-id", "ops-tools-1203", "Project ID to target.")
	flag.IntVar(&cfg.Providers.GCP.DefaultGCSDiscount, "gcp.default-discount", 19, "GCP default discount")
}

func operationalFlags(cfg *config.Config) {
	flag.DurationVar(&cfg.Collector.ScrapeInterval, "scrape-interval", 1*time.Hour, "Scrape interval")
	flag.DurationVar(&cfg.Server.Timeout, "server-timeout", 30*time.Second, "Server timeout")
	flag.StringVar(&cfg.Server.Address, "server.address", ":8080", "Default address for the server to listen on.")
	flag.StringVar(&cfg.Server.Path, "server.path", "/metrics", "Default path for the server to listen on.")
	flag.StringVar(&cfg.Logger.Level, "log.level", "info", "Log level(debug, info, warn, error)")
	flag.StringVar(&cfg.Logger.Output, "log.output", "stdout", "Log output(stdout, stderr)")
	flag.StringVar(&cfg.Logger.Type, "log.type", "text", "Log type(json, text)")
}

func selectProvider(cfg *config.Config, logger *slog.Logger) (provider.Provider, error) {
	switch cfg.Provider {
	case "aws":
		return aws.New(&aws.Config{
			Region:         cfg.Providers.AWS.Region,
			Profile:        cfg.Providers.AWS.Profile,
			ScrapeInterval: cfg.Collector.ScrapeInterval,
			Services:       strings.Split(cfg.Providers.AWS.Services.String(), ","),
		}, logger)

	case "gcp":
		return google.New(&google.Config{
			ProjectId:       cfg.ProjectID,
			Region:          cfg.Providers.GCP.Region,
			Projects:        cfg.Providers.GCP.Projects.String(),
			DefaultDiscount: cfg.Providers.GCP.DefaultGCSDiscount,
			ScrapeInterval:  cfg.Collector.ScrapeInterval,
			Services:        strings.Split(cfg.Providers.GCP.Services.String(), ","),
		}, logger)

	default:
		return nil, fmt.Errorf("unknown provider")
	}
}

func createPromRegistryHandler(csp provider.Provider) http.Handler {
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

func runServer(ctx context.Context, cfg *config.Config, csp provider.Provider, logger *slog.Logger) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/", web.HomePageHandler(cfg.Server.Path))   // landing page
	mux.Handle(cfg.Server.Path, createPromRegistryHandler(csp)) // prom metrics handler

	server := &http.Server{Addr: cfg.Server.Address, Handler: mux}
	errChan := make(chan error)

	go func() {
		logger.Info("Serving requests", "address", cfg.Server.Address, "path", cfg.Server.Path)
		errChan <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Print("shutting down server")
		ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.Timeout)
		defer cancel()

		err := server.Shutdown(ctx)
		if err != nil {
			return fmt.Errorf("error shutting down server: %w", err)
		}
	case err := <-errChan:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("error running server: %w", err)
		}
	}

	return nil
}

func main() {
	var cfg config.Config
	providerFlags(flag.CommandLine, &cfg)
	operationalFlags(&cfg)
	flag.Parse()

	logger := SetupLogger(cfg.Logger.Level, cfg.Logger.Output, cfg.Logger.Type)

	logger.Info("Starting CloudCost Exporter", "version", cversion.Info(), "build_context", cversion.BuildContext())

	csp, err := selectProvider(&cfg, logger)
	if err != nil {
		log.Fatalf("Error setting up provider %s: %s", cfg.Provider, err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	err = runServer(ctx, &cfg, csp, logger)
	if err != nil {
		log.Fatal(err)
	}
}

func SetupLogger(level, output, logtype string) *slog.Logger {
	handler := utils.NewLevelHandler(utils.GetLogLevel(level), utils.HandlerForOutput(logtype, utils.WriterForOutput(output)))
	logger := slog.New(handler)
	return logger
}
