package networking

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/api/compute/v1"

	cloudcostexporter "github.com/grafana/cloudcost-exporter"
)

const (
	collectorName        = "ForwardingRule"
	PriceRefreshInterval = 24 * time.Hour
	subsystem            = "gcp_clb_forwarding_rule"

	fwdRuleDescription             = "The unit cost of a forwarding rule per hour in USD"
	fwdRuleInboundDataDescription  = "The inbound data processed unit cost of a forwarding rule in USD/GiB"
	fwdRuleOutboundDataDescription = "The outbound data processed unit cost of a forwarding rule in USD/GiB"

	fwdRuleMetricName             = "unit_per_hour"
	fwdRuleInboundDataMetricName  = "inbound_data_processed_per_gib"
	fwdRuleOutboundDataMetricName = "outbound_data_processed_per_gib"
)

var (
	ForwardingRuleUnitCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, fwdRuleMetricName),
		fwdRuleDescription,
		[]string{"name", "region", "project", "ip_address", "load_balancing_scheme"},
		nil,
	)
	ForwardingRuleInboundDataProcessedCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, fwdRuleInboundDataMetricName),
		fwdRuleInboundDataDescription,
		[]string{"name", "region", "project", "ip_address", "load_balancing_scheme"}, nil,
	)
	ForwardingRuleOutboundDataProcessedCostDesc = prometheus.NewDesc(
		prometheus.BuildFQName(cloudcostexporter.MetricPrefix, subsystem, fwdRuleOutboundDataMetricName),
		fwdRuleOutboundDataDescription,
		[]string{"name", "region", "project", "ip_address", "load_balancing_scheme"}, nil,
	)
)

type Config struct {
	Logger         *slog.Logger
	ScrapeInterval time.Duration
	Projects       string
}
type Collector struct {
	gcpClient  client.Client
	projects   []string
	pricingMap *pricingMap
	logger     *slog.Logger
	ctx        context.Context
}

type ForwardingRuleInfo struct {
	Name                      string
	Region                    string
	Project                   string
	IPAddress                 string
	LoadBalancingScheme       string
	ForwardingRuleCost        float64
	InboundDataProcessedCost  float64
	OutboundDataProcessedCost float64
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- ForwardingRuleUnitCostDesc
	ch <- ForwardingRuleInboundDataProcessedCostDesc
	ch <- ForwardingRuleOutboundDataProcessedCostDesc
	return nil
}

func (c *Collector) CollectMetrics(_ chan<- prometheus.Metric) float64 {
	return 0
}

func New(ctx context.Context, config *Config, gcpClient client.Client) (*Collector, error) {
	logger := config.Logger.With("collector", "forwarding_rule")

	priceTicker := time.NewTicker(PriceRefreshInterval)
	pm, err := newPricingMap(logger, gcpClient)
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-priceTicker.C:
				if err := pm.populate(ctx); err != nil {
					logger.Error("failed to refresh pricing map", "error", err)
				}
			}
		}
	}()

	return &Collector{
		projects:   strings.Split(config.Projects, ","),
		gcpClient:  gcpClient,
		logger:     logger,
		pricingMap: pm,
		ctx:        ctx,
	}, nil
}

func (c *Collector) Name() string {
	return collectorName
}

func (c *Collector) Register(registry provider.Registry) error {
	c.logger.LogAttrs(c.ctx, slog.LevelInfo, "Registering Forwarding Rule metrics")
	return nil
}

func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	c.logger.LogAttrs(ctx, slog.LevelInfo, "Collecting Forwarding Rule metrics")

	forwardingRuleInfo, err := c.getForwardingRuleInfo(ctx)
	if err != nil {
		c.logger.Error("error getting forwarding rule info", "error", err)
		return err
	}

	for _, forwardingRule := range forwardingRuleInfo {
		labelValues := []string{
			forwardingRule.Name,
			forwardingRule.Region,
			forwardingRule.Project,
			forwardingRule.IPAddress,
			forwardingRule.LoadBalancingScheme,
		}
		ch <- prometheus.MustNewConstMetric(
			ForwardingRuleUnitCostDesc,
			prometheus.GaugeValue,
			forwardingRule.ForwardingRuleCost,
			labelValues...,
		)
		ch <- prometheus.MustNewConstMetric(
			ForwardingRuleInboundDataProcessedCostDesc,
			prometheus.GaugeValue,
			forwardingRule.InboundDataProcessedCost,
			labelValues...,
		)
		ch <- prometheus.MustNewConstMetric(
			ForwardingRuleOutboundDataProcessedCostDesc,
			prometheus.GaugeValue,
			forwardingRule.OutboundDataProcessedCost,
			labelValues...,
		)
	}
	return nil
}

func (c *Collector) getForwardingRuleInfo(ctx context.Context) ([]ForwardingRuleInfo, error) {
	var allForwardingRuleInfo = []ForwardingRuleInfo{}
	var mu sync.Mutex

	// Process projects sequentially
	for _, project := range c.projects {
		regions, err := c.gcpClient.GetRegions(project)
		if err != nil {
			c.logger.Error("error getting regions for project", "project", project, "error", err)
			continue
		}

		// Process regions in parallel with limited concurrency
		regionChan := make(chan *compute.Region, len(regions))
		for _, region := range regions {
			regionChan <- region
		}
		close(regionChan)

		// Limit concurrent region processing to avoid overwhelming the API
		maxConcurrentRegions := 5
		if len(regions) < maxConcurrentRegions {
			maxConcurrentRegions = len(regions)
		}

		regionWaitGroup := sync.WaitGroup{}
		for i := 0; i < maxConcurrentRegions; i++ {
			regionWaitGroup.Add(1)
			go func() {
				defer regionWaitGroup.Done()
				for region := range regionChan {
					c.processRegion(ctx, project, region, &allForwardingRuleInfo, &mu)
				}
			}()
		}
		regionWaitGroup.Wait()
	}

	return allForwardingRuleInfo, nil
}

// processRegion processes a single region
func (c *Collector) processRegion(ctx context.Context, project string, region *compute.Region, allForwardingRuleInfo *[]ForwardingRuleInfo, mu *sync.Mutex) {
	// Always fetch fresh forwarding rules (no caching)
	forwardingRules, err := c.gcpClient.ListForwardingRules(ctx, project, region.Name)
	if err != nil {
		c.logger.Error("error listing forwarding rules for project", "project", project, "region", region.Name, "error", err)
		return
	}

	// Process forwarding rules in batches to reduce goroutine overhead
	batchSize := 10
	for i := 0; i < len(forwardingRules); i += batchSize {
		end := i + batchSize
		if end > len(forwardingRules) {
			end = len(forwardingRules)
		}

		batch := forwardingRules[i:end]
		ruleInfos := make([]ForwardingRuleInfo, 0, len(batch))

		// Process batch synchronously to avoid excessive goroutines
		for _, forwardingRule := range batch {
			ruleInfo := c.processForwardingRule(forwardingRule, region.Name, project)
			ruleInfos = append(ruleInfos, ruleInfo)
		}

		// Add batch results atomically
		mu.Lock()
		*allForwardingRuleInfo = append(*allForwardingRuleInfo, ruleInfos...)
		mu.Unlock()
	}
}

func (c *Collector) getForwardingRuleCost(region string) (float64, error) {
	return c.pricingMap.GetCostOfForwardingRule(region)
}

func (c *Collector) getInboundDataProcessedCost(region string) (float64, error) {
	return c.pricingMap.GetCostOfInboundData(region)
}

func (c *Collector) getOutboundDataProcessedCost(region string) (float64, error) {
	return c.pricingMap.GetCostOfOutboundData(region)
}

func (c *Collector) processForwardingRule(forwardingRule *compute.ForwardingRule, region string, project string) ForwardingRuleInfo {
	fwdRuleCost, err := c.getForwardingRuleCost(region)
	if err != nil {
		c.logger.Error("error getting cost of forwarding rule", "name", forwardingRule.Name, "region", region, "project", project, "error", err)
	}
	fwdRuleInboundDataProcessedCost, err := c.getInboundDataProcessedCost(region)
	if err != nil {
		c.logger.Error("error getting cost of inbound data processed for forwarding rule", "name", forwardingRule.Name, "region", region, "project", project, "error", err)
	}
	fwdRuleOutboundDataProcessedCost, err := c.getOutboundDataProcessedCost(region)
	if err != nil {
		c.logger.Error("error getting cost of outbound data processed for forwarding rule", "name", forwardingRule.Name, "region", region, "project", project, "error", err)
	}
	return ForwardingRuleInfo{
		Name:                      forwardingRule.Name,
		Region:                    region,
		Project:                   project,
		IPAddress:                 forwardingRule.IPAddress,
		LoadBalancingScheme:       forwardingRule.LoadBalancingScheme,
		ForwardingRuleCost:        fwdRuleCost,
		InboundDataProcessedCost:  fwdRuleInboundDataProcessedCost,
		OutboundDataProcessedCost: fwdRuleOutboundDataProcessedCost,
	}
}
