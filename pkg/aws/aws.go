package aws

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	msk "github.com/aws/aws-sdk-go-v2/service/kafka"
	awsPricing "github.com/aws/aws-sdk-go-v2/service/pricing"
	rds "github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	rdsCollector "github.com/grafana/cloudcost-exporter/pkg/aws/rds"
	"github.com/grafana/cloudcost-exporter/pkg/collectormetrics"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/cloudcost-exporter/pkg/aws/bedrock"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	ec2Collector "github.com/grafana/cloudcost-exporter/pkg/aws/ec2"
	"github.com/grafana/cloudcost-exporter/pkg/aws/elb"
	mskCollector "github.com/grafana/cloudcost-exporter/pkg/aws/msk"
	awsgwnat "github.com/grafana/cloudcost-exporter/pkg/aws/natgateway"
	awsvpc "github.com/grafana/cloudcost-exporter/pkg/aws/vpc"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/aws/s3"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

type Config struct {
	Services             []string
	ExperimentalServices []string
	Region               string
	Profile              string
	RoleARN              string
	ExcludeRegions       []string // AWS region names to skip (e.g. me-central-1)
	ScrapeInterval       time.Duration
	CollectorTimeout     time.Duration
	RDSRegionListTimeout time.Duration // per-region timeout for the background RDS refresh; 0 uses an internal safety ceiling
	Logger               *slog.Logger
	AccountID            string
	BedrockFamilyFilter  string // regex matched against family label; default "anthropic|amazon"
	// CapacityBlocksEnabled turns on pricing of EC2 Capacity Block for ML
	// reservations in the EC2 collector. Off by default: it adds Cost Explorer
	// calls (ce:GetCostAndUsage) and only applies to accounts that buy them.
	CapacityBlocksEnabled bool
	// CollectorRetryInitial and CollectorRetryMax bound the backoff between
	// attempts to create a collector that failed at startup. Zero uses the
	// defaults below; tests set them to shorten the wait.
	CollectorRetryInitial time.Duration
	CollectorRetryMax     time.Duration
}

type AWS struct {
	Config           *Config
	collectors       []provider.Collector
	logger           *slog.Logger
	ctx              context.Context
	collectorTimeout time.Duration

	retryInitial time.Duration
	retryMax     time.Duration

	// mu guards collectors, registry and pending, all of which the retry
	// goroutine writes while scrapes read them.
	mu       sync.RWMutex
	registry provider.Registry
	// pending maps a service that failed to create to the name its collector
	// reports, so Collect can report it under the same label the healthy
	// collector would use.
	pending map[string]string
}

var (
	collectorLastScrapeErrorDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "last_scrape_error"),
		"Counter of the number of errors that occurred during the last scrape.",
		[]string{"provider", "collector"},
		nil,
	)
	collectorDurationDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "last_scrape_duration_seconds"),
		"Duration of the last scrape in seconds.",
		[]string{"provider", "collector"},
		nil,
	)
	collectorLastScrapeTime = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcost_exporter.ExporterName, "collector", "last_scrape_time"),
		"Time of the last scrape.",
		[]string{"provider", "collector"},
		nil,
	)
)

const (
	subsystem        = "aws"
	maxRetryAttempts = 10

	// collectConcurrencyLimit caps the number of collector goroutines that run
	// simultaneously during a scrape. This prevents unbounded API fan-out when
	// many collectors are registered and keeps memory and rate-limit usage
	// predictable. The value matches Azure's ConcurrentGoroutineLimit.
	collectConcurrencyLimit = 10

	// AWS service names used across the AWS provider.
	serviceS3      = "S3"
	serviceEC2     = "EC2"
	serviceRDS     = "RDS"
	serviceNATGW   = "NATGATEWAY"
	serviceELB     = "ELB"
	serviceVPC     = "VPC"
	serviceMSK     = "MSK"
	serviceBedrock = "BEDROCK"
)

// defaultCollectorRetryInitial and defaultCollectorRetryMax bound the backoff
// between attempts to create a collector that failed at startup.
//
// The cap is deliberately longer than the GCP provider's 15m. Every attempt is
// a full pricing fetch, there is no pricing cache, and the healthy path only
// refreshes once a day, so a broken collector queries the Pricing API far more
// often than a working one. AWS runs many more exporters than GCP does and the
// Pricing API already returns ThrottlingException under load, so retrying hard
// would push on the API whose slowness caused the failure.
const (
	defaultCollectorRetryInitial = 30 * time.Second
	defaultCollectorRetryMax     = time.Hour

	// retryJitterFraction spreads each wait by +/- this fraction. Exporters are
	// restarted together by a rollout, so without jitter every pod would retry
	// in lockstep and hit the Pricing API at the same instant.
	retryJitterFraction = 5 // one fifth, i.e. +/-20%
)

// errUnknownService marks a service name that has no collector, so startup can
// skip it rather than recording it as pending and retrying it forever.
var errUnknownService = errors.New("unknown service")

// collectorNames maps a service to the name its collector reports. Collect uses
// it to report a service that failed to create under the same label the healthy
// collector would use, so the failure and the recovery are one series over time.
// Hand-maintained; TestCollectorNamesMatchTheNameEachCollectorReports builds
// every collector and asserts these stay correct.
var collectorNames = map[string]string{
	serviceS3:      "S3",
	serviceEC2:     "aws_ec2",
	serviceRDS:     "aws_rds",
	serviceMSK:     "aws_msk",
	serviceELB:     "aws_elb",
	serviceNATGW:   "NATGATEWAY",
	serviceVPC:     "VPC",
	serviceBedrock: "aws_bedrock",
}

// collectorDeps carries everything the per-service constructors close over, so
// the switch can live in a function the retry calls as well as startup.
type collectorDeps struct {
	config        *Config
	logger        *slog.Logger
	awsClient     client.Client
	regionClients map[string]client.Client
	regions       []types.Region
	awsConfig     aws.Config
	pricingConfig aws.Config
	pricingAPI    *awsPricing.Client
}

// collectorFactory creates the collector for a service. Tests inject failures
// through it.
type collectorFactory func(ctx context.Context, service string) (provider.Collector, error)

// Services returns the collectors that can be enabled via -aws.services.
// The Name field is the canonical flag value; the dispatch switch in
// newWithDependencies cases on the same constants.
func Services() []provider.ServiceInfo {
	return []provider.ServiceInfo{
		{Name: serviceS3, DisplayName: "S3", Description: "Simple Storage Service buckets"},
		{Name: serviceEC2, DisplayName: "EC2", Description: "Elastic Compute Cloud instances (spot and on-demand pricing)"},
		{Name: serviceRDS, DisplayName: "RDS", Description: "Relational Database Service instances"},
		{Name: serviceMSK, DisplayName: "MSK", Description: "Managed Service for Apache Kafka clusters"},
		{Name: serviceELB, DisplayName: "ELB", Description: "Elastic Load Balancers (ALB, NLB)"},
		{Name: serviceNATGW, DisplayName: "NAT Gateway", Description: "Network Address Translation gateways"},
		{Name: serviceVPC, DisplayName: "VPC", Description: "VPC endpoints and services"},
		{Name: serviceBedrock, DisplayName: "Bedrock", Description: "Amazon Bedrock foundation model pricing"},
	}
}

func New(ctx context.Context, config *Config) (*AWS, error) {
	// There are two scenarios:
	// 1. Running locally, the user must pass in a region and profile to use
	// 2. Running within an EC2 instance and the region and profile can be derived
	// I'm going to use the AWS SDK to handle this for me. If the user has provided a region and profile, it will use that.
	// If not, it will use the EC2 instance metadata service to determine the region and credentials.
	// This is the same logic that the AWS CLI uses, so it should be fine.
	ac, err := createAWSConfig(ctx, config.Region, config.Profile, config.RoleARN)
	if err != nil {
		return nil, err
	}
	awsClient := client.NewAWSClient(client.Config{
		PricingService: awsPricing.NewFromConfig(ac),
		EC2Service:     ec2.NewFromConfig(ac),
		BillingService: costexplorer.NewFromConfig(ac),
		RDSService:     rds.NewFromConfig(ac),
		ELBService:     elbv2.NewFromConfig(ac),
	})

	// Get regions from the client
	regions, err := awsClient.DescribeRegions(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("error getting regions: %w", err)
	}

	regions = filterExcludedRegions(regions, config.ExcludeRegions)

	// Resolve the AWS account ID from the active credentials.
	stsClient := sts.NewFromConfig(ac)
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("resolving AWS account ID: %w", err)
	}
	config.AccountID = aws.ToString(identity.Account)

	pricingConfig, err := createAWSConfig(ctx, "us-east-1", config.Profile, config.RoleARN)
	if err != nil {
		return nil, err
	}

	// Create per-region clients
	awsClientPerRegion, err := newRegionClientMap(ctx, ac, pricingConfig, regions, config.Profile, config.RoleARN)
	if err != nil {
		return nil, err
	}

	return newWithDependencies(ctx, config, awsClient, awsClientPerRegion, regions, ac, pricingConfig)
}

// newWithDependencies creates an AWS provider with all dependencies injected as parameters
func newWithDependencies(ctx context.Context, config *Config, awsClient client.Client, regionClients map[string]client.Client, regions []types.Region, awsConfig aws.Config, pricingConfig aws.Config) (*AWS, error) {
	var services []string
	logger := config.Logger.With("provider", subsystem)
	pricingAPI := awsPricing.NewFromConfig(pricingConfig)

	// Register stable services followed by experimental ones. Experimental collectors are outside
	// the backward-compatibility contract, so warn when registering them. A service already enabled
	// as stable is not registered again as experimental; registering it twice would fail collector
	// registration with a duplicate-descriptor error.
	stableNames := make(map[string]bool, len(config.Services))
	for _, s := range config.Services {
		stableNames[strings.ToUpper(strings.TrimSpace(s))] = true
	}
	allServices := slices.Concat(config.Services, config.ExperimentalServices)
	for i, service := range allServices {
		service = strings.TrimSpace(service)
		if service == "" {
			continue
		}
		if i >= len(config.Services) {
			if stableNames[strings.ToUpper(service)] {
				continue
			}
			logger.LogAttrs(ctx, slog.LevelWarn, "registering experimental collector; its metrics are not covered by the backward-compatibility contract and may change",
				slog.String("service", service))
		}
		service = strings.ToUpper(service)
		services = append(services, service)
	}

	a := &AWS{
		Config:           config,
		logger:           logger,
		ctx:              ctx,
		collectorTimeout: config.CollectorTimeout,
		retryInitial:     config.CollectorRetryInitial,
		retryMax:         config.CollectorRetryMax,
	}
	if a.retryInitial <= 0 {
		a.retryInitial = defaultCollectorRetryInitial
	}
	if a.retryMax <= 0 {
		a.retryMax = defaultCollectorRetryMax
	}

	deps := collectorDeps{
		config:        config,
		logger:        logger,
		awsClient:     awsClient,
		regionClients: regionClients,
		regions:       regions,
		awsConfig:     awsConfig,
		pricingConfig: pricingConfig,
		pricingAPI:    pricingAPI,
	}
	create := func(ctx context.Context, service string) (provider.Collector, error) {
		return createCollector(ctx, service, deps)
	}

	for _, service := range services {
		collector, err := createSafely(ctx, create, service)
		if errors.Is(err, errUnknownService) {
			logger.LogAttrs(ctx, slog.LevelWarn, "unknown service, skipping",
				slog.String("service", service))
			continue
		}
		if err != nil {
			// Recorded rather than dropped: Collect reports it with
			// last_scrape_error=1 and the retry below keeps trying, so a
			// pricing API that is briefly slow no longer costs the collector
			// for the life of the pod.
			logger.LogAttrs(ctx, slog.LevelError, "Error creating collector",
				slog.String("service", service),
				slog.String("message", err.Error()))
			a.markPending(service)
			continue
		}
		a.collectors = append(a.collectors, collector)
	}

	if len(a.pendingServices()) > 0 {
		go a.retryPending(ctx, create)
	}
	return a, nil
}

func (a *AWS) markPending(service string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending == nil {
		a.pending = map[string]string{}
	}
	name, ok := collectorNames[strings.ToUpper(service)]
	if !ok {
		name = strings.ToLower(service)
	}
	a.pending[service] = name
}

func (a *AWS) pendingServices() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	services := make([]string, 0, len(a.pending))
	for service := range a.pending {
		services = append(services, service)
	}
	slices.Sort(services)
	return services
}

// addCollector adds a collector created after startup and drops the service
// from pending. Both happen under one lock: Collect reports every pending
// service under the collector's own name, so a scrape that saw the collector
// appended while the service was still pending would emit that label set twice
// and fail the whole scrape on a duplicate metric. Once RegisterCollectors has
// run this also registers the collector's metrics. A panic during registration
// is returned as an error so a background goroutine cannot crash the exporter,
// and leaves the service pending for the next retry.
func (a *AWS) addCollector(service string, c provider.Collector) (err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.registry != nil {
		defer func() {
			if p := recover(); p != nil {
				err = fmt.Errorf("panic registering collector %s: %v", c.Name(), p)
			}
		}()
		if err := c.Register(a.registry); err != nil {
			return err
		}
	}
	a.collectors = append(a.collectors, c)
	delete(a.pending, service)
	return nil
}

// snapshot returns the collectors, and the names of those that failed to
// create, both safe to use without holding the lock.
func (a *AWS) snapshot() ([]provider.Collector, []string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	names := make([]string, 0, len(a.pending))
	for _, name := range a.pending {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return slices.Clone(a.collectors), names
}

// createSafely turns a panicking constructor into an error so one bad collector
// is skipped and retried rather than killing the exporter.
func createSafely(ctx context.Context, create collectorFactory, service string) (c provider.Collector, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic creating collector for %s: %v", service, p)
		}
	}()
	return create(ctx, service)
}

// jittered spreads a wait by +/-20% so pods restarted together by a rollout do
// not all hit the Pricing API at the same instant.
func jittered(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	spread := int64(d) / retryJitterFraction
	if spread <= 0 {
		return d
	}
	return d + time.Duration(rand.Int64N(2*spread+1)-spread)
}

// retryPending keeps trying to create the collectors that failed at startup,
// backing off from retryInitial to retryMax, until each succeeds or ctx ends.
func (a *AWS) retryPending(ctx context.Context, create collectorFactory) {
	queue := a.pendingServices()
	delay := a.retryInitial
	for len(queue) > 0 {
		timer := time.NewTimer(jittered(delay))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		var remaining []string
		for _, service := range queue {
			collector, err := createSafely(ctx, create, service)
			if err == nil {
				err = a.addCollector(service, collector)
			}
			if err != nil {
				a.logger.LogAttrs(ctx, slog.LevelWarn, "Retrying collector creation failed",
					slog.String("service", service),
					slog.String("message", err.Error()))
				remaining = append(remaining, service)
				continue
			}
			a.logger.LogAttrs(ctx, slog.LevelInfo, "Collector created after retry",
				slog.String("service", service))
		}

		queue = remaining
		delay = min(delay*2, a.retryMax)
	}
}

// createCollector builds the collector for one service. Lifted out of
// newWithDependencies unchanged so the background retry can call it too.
func createCollector(ctx context.Context, service string, d collectorDeps) (provider.Collector, error) {
	switch service {
	case serviceS3:
		collector, err := s3.New(ctx, &s3.Config{
			ScrapeInterval: d.config.ScrapeInterval,
			AccountID:      d.config.AccountID,
		}, d.logger, d.awsClient)
		if err != nil {
			return nil, err
		}
		return collector, nil
	case serviceEC2:
		collector, err := ec2Collector.New(ctx, &ec2Collector.Config{
			Regions:               d.regions,
			ScrapeInterval:        d.config.ScrapeInterval,
			RegionMap:             d.regionClients,
			AccountID:             d.config.AccountID,
			CapacityBlocksEnabled: d.config.CapacityBlocksEnabled,
		}, d.logger)
		if err != nil {
			return nil, err
		}
		return collector, nil
	case serviceRDS:
		awsRDSClient := client.NewAWSClient(client.Config{
			PricingService: d.pricingAPI,
			RDSService:     rds.NewFromConfig(d.awsConfig),
		})
		collector, err := rdsCollector.New(ctx, &rdsCollector.Config{
			ScrapeInterval:    d.config.ScrapeInterval,
			Regions:           d.regions,
			RegionMap:         d.regionClients,
			Client:            awsRDSClient,
			AccountID:         d.config.AccountID,
			RegionListTimeout: d.config.RDSRegionListTimeout,
		}, d.logger)
		if err != nil {
			return nil, err
		}
		return collector, nil
	case serviceNATGW:
		collector, err := awsgwnat.New(ctx, &awsgwnat.Config{
			ScrapeInterval: d.config.ScrapeInterval,
			Regions:        d.regions,
			RegionMap:      d.regionClients,
			AccountID:      d.config.AccountID,
		}, d.logger)
		if err != nil {
			return nil, err
		}
		return collector, nil
	case serviceELB:
		awsELBPricingClient := client.NewAWSClient(client.Config{
			PricingService: d.pricingAPI,
		})
		collector, err := elb.New(ctx, &elb.Config{
			Regions:        d.regions,
			PricingClient:  awsELBPricingClient,
			RegionMap:      d.regionClients,
			ScrapeInterval: d.config.ScrapeInterval,
			AccountID:      d.config.AccountID,
		}, d.logger)
		if err != nil {
			return nil, err
		}
		return collector, nil
	case serviceVPC:
		awsVPCClient := client.NewAWSClient(client.Config{
			PricingService: d.pricingAPI,
			EC2Service:     ec2.NewFromConfig(d.pricingConfig),
		})
		collector, err := awsvpc.New(ctx, &awsvpc.Config{
			ScrapeInterval: d.config.ScrapeInterval,
			Regions:        d.regions,
			Client:         awsVPCClient,
			AccountID:      d.config.AccountID,
		}, d.logger)
		if err != nil {
			return nil, err
		}
		return collector, nil
	case serviceMSK:
		awsMSKClient := client.NewAWSClient(client.Config{
			PricingService: d.pricingAPI,
		})
		collector, err := mskCollector.New(ctx, &mskCollector.Config{
			Regions:   d.regions,
			RegionMap: d.regionClients,
			Client:    awsMSKClient,
			AccountID: d.config.AccountID,
		}, d.logger)
		if err != nil {
			return nil, err
		}
		return collector, nil
	case serviceBedrock:
		// Bedrock pricing lookups succeed regardless of the collector's configured regions.
		// Note: this pins the *endpoint*, not the queried region — the collector still
		// fetches prices per configured region via a regionCode filter. See
		// pkg/aws/bedrock.go newPriceFetcher().
		awsBedrockClient := client.NewAWSClient(client.Config{
			PricingService: d.pricingAPI,
		})
		collector, err := bedrock.New(ctx, &bedrock.Config{
			Regions:       d.regions,
			PricingClient: awsBedrockClient,
			AccountID:     d.config.AccountID,
			FamilyFilter:  d.config.BedrockFamilyFilter,
		}, d.logger)
		if err != nil {
			return nil, err
		}
		return collector, nil
	default:
		return nil, fmt.Errorf("%w: %s", errUnknownService, service)
	}
}

func (a *AWS) RegisterCollectors(registry provider.Registry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.logger.LogAttrs(a.ctx, slog.LevelInfo, "registering collectors",
		slog.Int("count", len(a.collectors)),
	)
	// Remembered so addCollector can register a collector created later, once
	// the registry is already serving scrapes.
	a.registry = registry
	for _, c := range a.collectors {
		if err := c.Register(registry); err != nil {
			return err
		}
	}
	return nil
}

func (a *AWS) Describe(ch chan<- *prometheus.Desc) {
	ch <- collectorLastScrapeErrorDesc
	ch <- collectorDurationDesc
	ch <- collectorLastScrapeTime
	collectors, _ := a.snapshot()
	for _, c := range collectors {
		if err := c.Describe(ch); err != nil {
			a.logger.LogAttrs(a.ctx, slog.LevelError, "failed to describe collector",
				slog.String("message", err.Error()),
				slog.String("collector", c.Name()),
			)
		}
	}
}

func (a *AWS) Collect(ch chan<- prometheus.Metric) {
	// Create a context with timeout for this collection cycle
	collectCtx, cancel := context.WithTimeout(a.ctx, a.collectorTimeout)
	defer cancel()

	collectors, notCreated := a.snapshot()
	// A collector that failed to create emits the same three metrics a healthy
	// one does, so the family keeps a single label set and a recovery reads as
	// the same series clearing rather than a new one appearing.
	for _, name := range notCreated {
		ch <- prometheus.MustNewConstMetric(collectorLastScrapeErrorDesc, prometheus.CounterValue, 1, subsystem, name)
		ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, 0, subsystem, name)
		ch <- prometheus.MustNewConstMetric(collectorLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem, name)
	}

	g, collectCtx := errgroup.WithContext(collectCtx)
	g.SetLimit(collectConcurrencyLimit)
	for _, c := range collectors {
		g.Go(func() error {
			duration, hasError := collectormetrics.Collect(collectCtx, c, ch, a.logger, subsystem)

			//TODO: remove collectorErrors once we have the new metrics
			collectorErrors := 0.0
			if hasError {
				collectorErrors = 1.0
			}
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeErrorDesc, prometheus.CounterValue, collectorErrors, subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorDurationDesc, prometheus.GaugeValue, duration, subsystem, c.Name())
			ch <- prometheus.MustNewConstMetric(collectorLastScrapeTime, prometheus.GaugeValue, float64(time.Now().Unix()), subsystem, c.Name())
			return nil
		})
	}
	// Goroutines always return nil; Wait() will not return an error.
	_ = g.Wait()
}

// filterExcludedRegions returns regions with any in excludeList removed. excludeList entries are trimmed of whitespace.
// When excludeList is non-empty, regions with nil RegionName are omitted from the result.
func filterExcludedRegions(regions []types.Region, excludeList []string) []types.Region {
	if len(excludeList) == 0 {
		return regions
	}
	excludeSet := make(map[string]struct{}, len(excludeList))
	for _, r := range excludeList {
		excludeSet[strings.TrimSpace(r)] = struct{}{}
	}
	filtered := make([]types.Region, 0, len(regions))
	for _, r := range regions {
		if r.RegionName == nil {
			continue
		}
		if _, excluded := excludeSet[*r.RegionName]; !excluded {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

func newRegionClientMap(ctx context.Context, globalConfig aws.Config, pricingConfig aws.Config, regions []types.Region, profile string, roleARN string) (map[string]client.Client, error) {
	awsClientPerRegion := make(map[string]client.Client)
	pricingAPI := awsPricing.NewFromConfig(pricingConfig)
	for _, region := range regions {
		ac, err := createAWSConfig(ctx, *region.RegionName, profile, roleARN)
		if err != nil {
			return nil, err
		}
		awsClientPerRegion[*region.RegionName] = client.NewAWSClient(
			client.Config{
				PricingService: pricingAPI,
				EC2Service:     ec2.NewFromConfig(ac),
				BillingService: costexplorer.NewFromConfig(globalConfig),
				RDSService:     rds.NewFromConfig(ac),
				ELBService:     elbv2.NewFromConfig(ac),
				MSKService:     msk.NewFromConfig(ac),
			})
	}

	return awsClientPerRegion, nil
}

func createAWSConfig(ctx context.Context, region, profile, roleARN string) (aws.Config, error) {
	optionsFunc := make([]func(options *awsconfig.LoadOptions) error, 0)
	optionsFunc = append(optionsFunc, awsconfig.WithEC2IMDSRegion())
	optionsFunc = append(optionsFunc, awsconfig.WithRetryMaxAttempts(maxRetryAttempts))

	if region != "" {
		optionsFunc = append(optionsFunc, awsconfig.WithRegion(region))
	}

	if profile != "" {
		optionsFunc = append(optionsFunc, awsconfig.WithSharedConfigProfile(profile))
	}

	if roleARN != "" {
		role, err := assumeRole(ctx, roleARN, optionsFunc)
		if err != nil {
			return aws.Config{}, err
		}
		optionsFunc = append(optionsFunc, role)
	}

	return awsconfig.LoadDefaultConfig(ctx, optionsFunc...)
}

func assumeRole(ctx context.Context, roleARN string, options []func(*awsconfig.LoadOptions) error) (awsconfig.LoadOptionsFunc, error) {
	// Add the credentials to assume the role specified in config.RoleARN
	ac, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, err
	}

	stsService := sts.NewFromConfig(ac)

	return awsconfig.WithCredentialsProvider(
		aws.NewCredentialsCache(
			stscreds.NewAssumeRoleProvider(
				stsService,
				roleARN,
			),
		),
	), nil
}
