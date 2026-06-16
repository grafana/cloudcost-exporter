package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
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
	"github.com/grafana/cloudcost-exporter/pkg/azure"
	"github.com/grafana/cloudcost-exporter/pkg/google"
	"github.com/grafana/cloudcost-exporter/pkg/leaderelection"
	"github.com/grafana/cloudcost-exporter/pkg/logger"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

func main() {
	var cfg config.Config
	providerFlags(flag.CommandLine, &cfg)
	operationalFlags(&cfg)
	flag.Parse()

	if cfg.ListServices {
		printAvailableServices(os.Stdout)
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logs := setupLogger(cfg.LoggerOpts.Level, cfg.LoggerOpts.Output, cfg.LoggerOpts.Type)
	logs.LogAttrs(ctx, slog.LevelInfo, "Starting cloudcost-exporter",
		slog.String("version", cversion.Info()),
		slog.String("build_context", cversion.BuildContext()),
	)
	cfg.Logger = logs

	if cfg.Providers.GCP.BucketProjectsDeprecated {
		logs.LogAttrs(ctx, slog.LevelWarn, "'gcp.bucket-projects' is deprecated and will be removed in a future version. Use '--gcp.projects' instead.")
	}

	var err error
	if cfg.LeaderElection.Enabled {
		err = runWithLeaderElection(ctx, &cfg, logs)
	} else {
		err = run(ctx, &cfg, logs)
	}
	if err != nil {
		logs.LogAttrs(ctx, slog.LevelError, "Error running server", slog.String("message", err.Error()))
		os.Exit(1)
	}
}

// run selects the provider, registers its collectors, and serves metrics. This
// is the default path: every replica collects independently.
func run(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	csp, err := selectProvider(ctx, cfg)
	if err != nil {
		return fmt.Errorf("selecting provider %q: %w", cfg.Provider, err)
	}

	registry, handler := createPromRegistryHandler(regionFromConfig(cfg))
	if err := registerProvider(registry, csp); err != nil {
		return err
	}

	return runServer(ctx, cfg, handler, log)
}

// runWithLeaderElection serves up/down metrics on every replica but only
// registers the provider's collectors on the replica holding the leader lease,
// so a single set of cloud provider API calls is made regardless of replica
// count. Losing leadership stops collection and shuts the replica down so it
// rejoins as a candidate.
func runWithLeaderElection(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	registry, handler := createPromRegistryHandler(regionFromConfig(cfg))

	isLeader := leaderelection.NewIsLeaderGauge()
	registry.MustRegister(isLeader)

	client, err := leaderelection.NewInClusterClient()
	if err != nil {
		return fmt.Errorf("initializing leader election: %w", err)
	}

	identity, err := leaderelection.ResolveIdentity(cfg.LeaderElection.Identity)
	if err != nil {
		return err
	}

	opts := leaderelection.Options{
		LeaseName:     cfg.LeaderElection.LeaseName,
		Namespace:     leaderelection.ResolveNamespace(cfg.LeaderElection.Namespace),
		Identity:      identity,
		LeaseDuration: cfg.LeaderElection.LeaseDuration,
		RenewDeadline: cfg.LeaderElection.RenewDeadline,
		RetryPeriod:   cfg.LeaderElection.RetryPeriod,
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	serverErr := make(chan error, 1)
	go func() {
		// A server exit (bind failure or shutdown) also stops the election.
		defer cancel()
		serverErr <- runServer(runCtx, cfg, handler, log)
	}()

	leaderelection.Run(runCtx, client, opts, log, isLeader, func(leaderCtx context.Context) {
		csp, err := selectProvider(leaderCtx, cfg)
		if err != nil {
			log.LogAttrs(leaderCtx, slog.LevelError, "Error selecting provider after acquiring leadership",
				slog.String("message", err.Error()),
				slog.String("provider", cfg.Provider),
			)
			return
		}
		if err := registerProvider(registry, csp); err != nil {
			log.LogAttrs(leaderCtx, slog.LevelError, "Error registering collectors after acquiring leadership",
				slog.String("message", err.Error()),
			)
			return
		}
		log.LogAttrs(leaderCtx, slog.LevelInfo, "Collecting cloud provider metrics as leader")
	})

	// Run returns when ctx is cancelled or leadership is lost. Either way, stop
	// the server so the process exits; the orchestrator restarts the replica,
	// which rejoins as a candidate.
	if ctx.Err() == nil {
		log.LogAttrs(ctx, slog.LevelInfo, "Lost leadership, shutting down to rejoin as a candidate")
	}
	cancel()
	return <-serverErr
}

// providerFlags is a helper method that is responsible for setting up the flags that are used to configure the provider.
// TODO: This should probably be moved over to the config package.
func providerFlags(fs *flag.FlagSet, cfg *config.Config) {
	flag.StringVar(&cfg.Provider, "provider", "aws", "aws, gcp, or azure")
	fs.StringVar(&cfg.Providers.AWS.Profile, "aws.profile", "", "AWS Profile to authenticate with.")
	fs.Var(&cfg.Providers.GCP.Projects, "gcp.projects", "GCP project(s).")
	fs.Var(config.NewDeprecatedStringSliceFlag(&cfg.Providers.GCP.Projects, &cfg.Providers.GCP.BucketProjectsDeprecated), "gcp.bucket-projects", "GCP project(s). (deprecated: use --gcp.projects instead)")
	fs.Var(&cfg.Providers.AWS.Services, "aws.services", "AWS service(s). Run with -list-services to see available values.")
	fs.Var(&cfg.Providers.AWS.ExperimentalServices, "aws.experimental.services", "Experimental AWS service(s); their metrics are not covered by the backward-compatibility contract and may change. Run with -list-services to see available values.")
	fs.Var(&cfg.Providers.AWS.ExcludeRegions, "aws.exclude-regions", "AWS region(s) to exclude from cost collection.")
	fs.Var(&cfg.Providers.Azure.Services, "azure.services", "Azure service(s) (comma-separated and/or repeat flag; case-insensitive). Run with -list-services to see available values.")
	fs.Var(&cfg.Providers.Azure.ExperimentalServices, "azure.experimental.services", "Experimental Azure service(s); their metrics are not covered by the backward-compatibility contract and may change. Run with -list-services to see available values.")
	fs.Var(&cfg.Providers.GCP.Services, "gcp.services", "GCP service(s). Run with -list-services to see available values.")
	fs.Var(&cfg.Providers.GCP.ExperimentalServices, "gcp.experimental.services", "Experimental GCP service(s); their metrics are not covered by the backward-compatibility contract and may change. Run with -list-services to see available values.")
	flag.StringVar(&cfg.Providers.AWS.Region, "aws.region", "", "AWS region")
	flag.StringVar(&cfg.Providers.AWS.RoleARN, "aws.roleARN", "", "Optional AWS role ARN to assume for cross-account access.")
	fs.StringVar(&cfg.Providers.AWS.BedrockFamilyFilter, "aws.bedrock.families", ".*", "Regex matched against the Bedrock model family label. Only matching families are emitted.")
	flag.DurationVar(&cfg.Providers.AWS.RDSRegionListTimeout, "aws.rds.region-timeout", 0, "Per-region timeout for listing RDS instances. 0 (default) bounds each region only by -collector-interval. Set a positive value (e.g. 15s) to fail slow or unreachable regions fast and protect scrape availability.")
	// TODO - PUT PROJECT-ID UNDER GCP
	flag.StringVar(&cfg.ProjectID, "project-id", "", "Project ID to target.")
	flag.StringVar(&cfg.Providers.Azure.SubscriptionID, "azure.subscription-id", "", "Azure subscription ID to pull data from.")
	flag.IntVar(&cfg.Providers.GCP.DefaultGCSDiscount, "gcp.default-discount", 19, "GCP default discount")
	flag.IntVar(&cfg.Providers.GCP.GKEZoneConcurrency, "gcp.gke.zone-concurrency", 10, "Cap on concurrent API calls during a GKE scrape per project. Two goroutines run per zone (ListInstances + ListDisks), so the parallel-zone count is this value divided by 2.")
}

// operationalFlags is a helper method that is responsible for setting up the flags that are used to configure the operational aspects of the application.
// TODO: This should probably be moved over to the config package.
func operationalFlags(cfg *config.Config) {
	flag.BoolVar(&cfg.ListServices, "list-services", false, "Print the services available per provider and exit. Does not require credentials.")
	flag.DurationVar(&cfg.Collector.ScrapeInterval, "scrape-interval", 1*time.Hour, "Scrape interval")
	flag.DurationVar(&cfg.Collector.Timeout, "collector-interval", 1*time.Minute, "Context timeout for collectors")
	flag.DurationVar(&cfg.Server.Timeout, "server-timeout", 30*time.Second, "Server timeout")
	flag.StringVar(&cfg.Server.Address, "server.address", ":8080", "Default address for the server to listen on.")
	flag.StringVar(&cfg.Server.Path, "server.path", "/metrics", "Default path for the server to listen on.")
	flag.StringVar(&cfg.LoggerOpts.Level, "log.level", "info", "Log level: debug, info, warn, error")
	flag.StringVar(&cfg.LoggerOpts.Output, "log.output", "stdout", "Log output stream: stdout, stderr, file")
	flag.StringVar(&cfg.LoggerOpts.Type, "log.type", "text", "Log type: json, text")
	flag.BoolVar(&cfg.LeaderElection.Enabled, "leader-election.enabled", false, "Enable lease-based leader election so only the leader replica collects from cloud provider APIs. Requires running in a Kubernetes cluster. Default off preserves single-replica behavior.")
	flag.StringVar(&cfg.LeaderElection.LeaseName, "leader-election.lease-name", "cloudcost-exporter", "Name of the Lease object used for leader election.")
	flag.StringVar(&cfg.LeaderElection.Namespace, "leader-election.namespace", "", "Namespace of the Lease object. Defaults to the pod's service account namespace, then \"default\".")
	flag.StringVar(&cfg.LeaderElection.Identity, "leader-election.id", "", "Unique identity for this replica in leader election. Defaults to the hostname.")
	flag.DurationVar(&cfg.LeaderElection.LeaseDuration, "leader-election.lease-duration", 15*time.Second, "Duration a non-leader waits before it can acquire leadership.")
	flag.DurationVar(&cfg.LeaderElection.RenewDeadline, "leader-election.renew-deadline", 10*time.Second, "Duration the leader retries refreshing the lease before giving up leadership.")
	flag.DurationVar(&cfg.LeaderElection.RetryPeriod, "leader-election.retry-period", 2*time.Second, "Interval between leader-election attempts.")
}

// setupLogger is a helper method that is responsible for creating a structured logger that is used throughout the application.
// It sets the log level, output, and type of log.
func setupLogger(level string, output string, logtype string) *slog.Logger {
	handler := logger.NewLevelHandler(logger.GetLogLevel(level), logger.HandlerForOutput(logtype, logger.WriterForOutput(output)))
	return slog.New(handler)
}

// runServer is a helper method that is responsible for starting the metrics server and handling shutdown signals.
func runServer(ctx context.Context, cfg *config.Config, registryHandler http.Handler, log *slog.Logger) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

	mux.HandleFunc("/", web.HomePageHandler(cfg.Server.Path)) // landing page
	mux.Handle(cfg.Server.Path, registryHandler)              // prom metrics handler (/metrics)

	server := &http.Server{Addr: cfg.Server.Address, Handler: mux}
	errChan := make(chan error)

	go func() {
		log.LogAttrs(ctx, slog.LevelInfo, "Starting server",
			slog.String("address", cfg.Server.Address),
			slog.String("path", cfg.Server.Path))
		errChan <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.LogAttrs(ctx, slog.LevelInfo, "Shutting down server")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Server.Timeout)
		defer shutdownCancel()

		err := server.Shutdown(shutdownCtx)
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

func regionFromConfig(cfg *config.Config) string {
	switch cfg.Provider {
	case "aws":
		return cfg.Providers.AWS.Region
	case "gcp":
		return cfg.Providers.GCP.Region
	default:
		// TODO: add region support for Azure (currently has no region in config)
		return ""
	}
}

// createPromRegistryHandler builds the metrics registry with the base
// operational collectors (build info, Go runtime, process, version, request
// instrumentation) and returns it alongside the instrumented HTTP handler. The
// registry is returned so the caller can register the provider's collectors,
// either up front or once the replica becomes leader.
func createPromRegistryHandler(region string) (*prometheus.Registry, http.Handler) {
	var subsystem = "metrics_handler"
	requestDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:                           prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "request_duration_seconds"),
			Help:                           "Duration of HTTP requests in seconds for the metrics endpoint",
			NativeHistogramBucketFactor:    1.1,
			NativeHistogramMaxBucketNumber: 10,
			Buckets:                        []float64{1, 2, 5, 7, 10, 20, 40, 80},
			ConstLabels:                    prometheus.Labels{"region": region},
		},
		[]string{"method"},
	)

	requestCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name:        prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "requests_total"),
			Help:        "Total number of HTTP requests for the metrics endpoint",
			ConstLabels: prometheus.Labels{"region": region},
		},
		[]string{"code", "method"},
	)
	registry := prometheus.NewRegistry()
	registry.MustRegister(
		collectors.NewBuildInfoCollector(),
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		version.NewCollector(cloudcost_exporter.ExporterName),
		requestCounter,
		requestDuration,
	)

	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})

	return registry, promhttp.InstrumentHandlerDuration(
		requestDuration,
		promhttp.InstrumentHandlerCounter(
			requestCounter,
			handler,
		),
	)
}

// registerProvider registers the provider and its collectors into the registry,
// which starts cloud provider metric collection.
func registerProvider(registry *prometheus.Registry, csp provider.Provider) error {
	registry.MustRegister(csp)
	return csp.RegisterCollectors(registry)
}

func selectProvider(ctx context.Context, cfg *config.Config) (provider.Provider, error) {
	return selectProviderWith(ctx, cfg,
		func(ctx context.Context, cfg *aws.Config) (provider.Provider, error) { return aws.New(ctx, cfg) },
		func(ctx context.Context, cfg *azure.Config) (provider.Provider, error) { return azure.New(ctx, cfg) },
		func(ctx context.Context, cfg *google.Config) (provider.Provider, error) { return google.New(ctx, cfg) },
	)
}

type newProviderFunc[T any] func(context.Context, T) (provider.Provider, error)

func selectProviderWith(
	ctx context.Context,
	cfg *config.Config,
	newAWS newProviderFunc[*aws.Config],
	newAzure newProviderFunc[*azure.Config],
	newGCP newProviderFunc[*google.Config],
) (provider.Provider, error) {
	// Set collector timeout with 1 minute default
	collectorTimeout := cfg.Collector.Timeout
	if collectorTimeout == 0 {
		collectorTimeout = 1 * time.Minute
	}

	switch cfg.Provider {
	case "azure":
		return newAzure(ctx, &azure.Config{
			Logger:               cfg.Logger,
			SubscriptionID:       cfg.Providers.Azure.SubscriptionID,
			ScrapeInterval:       cfg.Collector.ScrapeInterval,
			Services:             strings.Split(cfg.Providers.Azure.Services.String(), ","),
			ExperimentalServices: strings.Split(cfg.Providers.Azure.ExperimentalServices.String(), ","),
			CollectorTimeout:     collectorTimeout,
		})
	case "aws":
		return newAWS(ctx, &aws.Config{
			Logger:               cfg.Logger,
			Region:               cfg.Providers.AWS.Region,
			Profile:              cfg.Providers.AWS.Profile,
			RoleARN:              cfg.Providers.AWS.RoleARN,
			ScrapeInterval:       cfg.Collector.ScrapeInterval,
			Services:             strings.Split(cfg.Providers.AWS.Services.String(), ","),
			ExperimentalServices: strings.Split(cfg.Providers.AWS.ExperimentalServices.String(), ","),
			ExcludeRegions:       strings.Split(cfg.Providers.AWS.ExcludeRegions.String(), ","),
			CollectorTimeout:     collectorTimeout,
			BedrockFamilyFilter:  cfg.Providers.AWS.BedrockFamilyFilter,
			RDSRegionListTimeout: cfg.Providers.AWS.RDSRegionListTimeout,
		})

	case "gcp":
		return newGCP(ctx, &google.Config{
			Logger:               cfg.Logger,
			ProjectId:            cfg.ProjectID,
			Region:               cfg.Providers.GCP.Region,
			Projects:             cfg.Providers.GCP.Projects.String(),
			DefaultDiscount:      cfg.Providers.GCP.DefaultGCSDiscount,
			ScrapeInterval:       cfg.Collector.ScrapeInterval,
			Services:             strings.Split(cfg.Providers.GCP.Services.String(), ","),
			ExperimentalServices: strings.Split(cfg.Providers.GCP.ExperimentalServices.String(), ","),
			CollectorTimeout:     collectorTimeout,
			GKEZoneConcurrency:   cfg.Providers.GCP.GKEZoneConcurrency,
		})

	default:
		return nil, fmt.Errorf("unknown provider")
	}
}

// printAvailableServices writes a human-readable summary of every collector
// each provider supports, plus a ready-to-run example command per provider.
// It does not initialize any provider and does not require credentials.
func printAvailableServices(w io.Writer) {
	groups := []struct {
		label    string
		flag     string
		services []provider.ServiceInfo
		example  string
	}{
		{
			label:    "GCP",
			flag:     "-gcp.services",
			services: google.Services(),
			example:  "cloudcost-exporter -provider gcp -gcp.projects <project> -gcp.services GKE,GCS",
		},
		{
			label:    "AWS",
			flag:     "-aws.services",
			services: aws.Services(),
			example:  "cloudcost-exporter -provider aws -aws.region <region> -aws.services EC2,S3",
		},
		{
			label:    "Azure",
			flag:     "-azure.services",
			services: azure.Services(),
			example:  "cloudcost-exporter -provider azure -azure.subscription-id <id> -azure.services AKS",
		},
	}

	for i, g := range groups {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s services (%s):\n", g.label, g.flag)
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		for _, s := range g.services {
			name := s.Name
			if len(s.Aliases) > 0 {
				name = fmt.Sprintf("%s (alias: %s)", s.Name, strings.Join(s.Aliases, ", "))
			}
			desc := s.Description
			if s.DisplayName != "" && !strings.EqualFold(s.DisplayName, s.Name) {
				desc = fmt.Sprintf("%s: %s", s.DisplayName, s.Description)
			}
			fmt.Fprintf(tw, "  %s\t%s\n", name, desc)
		}
		_ = tw.Flush()
		fmt.Fprintf(w, "\n  Example: %s\n", g.example)
	}
}
