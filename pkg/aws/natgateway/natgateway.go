package natgateway

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscostexplorer "github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/prometheus/client_golang/prometheus"

	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	awsclient "github.com/grafana/cloudcost-exporter/pkg/aws/client"
	"github.com/grafana/cloudcost-exporter/pkg/aws/common"
	"github.com/grafana/cloudcost-exporter/pkg/aws/services/costexplorer"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
)

const (
	subsystem = "aws_natgateway"
)

// Map re-used from the shared AWS client package.

// Metrics exported by this collector
type Metrics struct {
	HourlyGauge         *prometheus.GaugeVec
	DataProcessingGauge *prometheus.GaugeVec

	RequestCount       prometheus.Counter
	RequestErrorsCount prometheus.Counter
	NextScrapeGauge    prometheus.Gauge
}

func NewMetrics() Metrics {
	return Metrics{
		HourlyGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "hourly_rate_usd_per_hour"),
			Help: "Hourly cost of NAT Gateway by region. Cost represented in USD/hour",
		}, []string{"region", "usage_type", "service"}),
		DataProcessingGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "data_processing_usd_per_gb"),
			Help: "Data processing cost of NAT Gateway by region. Cost represented in USD/GB",
		}, []string{"region", "usage_type", "service"}),
		RequestCount: prometheus.NewCounter(prometheus.CounterOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "cost_api_requests_total"),
			Help: "Total number of requests made to the AWS Cost Explorer API",
		}),
		RequestErrorsCount: prometheus.NewCounter(prometheus.CounterOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "cost_api_requests_errors_total"),
			Help: "Total number of errors when making requests to the AWS Cost Explorer API",
		}),
		NextScrapeGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "next_scrape"),
			Help: "The next time the exporter will scrape AWS billing data. Can be used to trigger alerts if now - nextScrape > interval",
		}),
	}
}

// Collector implements provider.Collector for AWS NAT Gateway
type Collector struct {
	client     costexplorer.CostExplorer
	interval   time.Duration
	nextScrape time.Time
	metrics    Metrics
	billing    *awsclient.BillingData
	m          sync.RWMutex
}

func New(scrapeInterval time.Duration, client costexplorer.CostExplorer) *Collector {
	return &Collector{
		client:     client,
		interval:   scrapeInterval,
		nextScrape: time.Now().Add(-scrapeInterval),
		metrics:    NewMetrics(),
		m:          sync.RWMutex{},
	}
}

func (c *Collector) Name() string { return "NATGATEWAY" }

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error { return nil }

func (c *Collector) Register(registry provider.Registry) error {
	registry.MustRegister(c.metrics.HourlyGauge)
	registry.MustRegister(c.metrics.DataProcessingGauge)
	registry.MustRegister(c.metrics.RequestCount)
	registry.MustRegister(c.metrics.RequestErrorsCount)
	registry.MustRegister(c.metrics.NextScrapeGauge)
	return nil
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) error {
	if up := c.collectInternal(); up == 0 {
		return fmt.Errorf("error collecting NAT Gateway metrics")
	}
	return nil
}

func (c *Collector) CollectMetrics(ch chan<- prometheus.Metric) float64 {
	return c.collectInternal()
}

func (c *Collector) collectInternal() float64 {
	c.m.Lock()
	defer c.m.Unlock()
	now := time.Now()
	if c.billing == nil || now.After(c.nextScrape) {
		endDate := time.Now().AddDate(0, 0, -1)
		startDate := endDate.AddDate(0, 0, -30)
		billing, err := getBillingData(c.client, startDate, endDate, c.metrics)
		if err != nil {
			log.Printf("Error getting NAT Gateway billing data: %v\n", err)
			return 0
		}
		c.billing = billing
		c.nextScrape = time.Now().Add(c.interval)
		c.metrics.NextScrapeGauge.Set(float64(c.nextScrape.Unix()))
	}
	exportMetrics(c.billing, c.metrics)
	return 1
}

// Billing structures now reuse the shared AWS client types.
func getBillingData(client costexplorer.CostExplorer, startDate, endDate time.Time, m Metrics) (*awsclient.BillingData, error) {
	input := &awscostexplorer.GetCostAndUsageInput{
		TimePeriod:  &types.DateInterval{Start: aws.String(startDate.Format("2006-01-02")), End: aws.String(endDate.Format("2006-01-02"))},
		Granularity: types.GranularityDaily,
		Metrics:     []string{"UsageQuantity", "UnblendedCost"},
		GroupBy: []types.GroupDefinition{
			{Type: types.GroupDefinitionTypeDimension, Key: aws.String("SERVICE")},
			{Type: types.GroupDefinitionTypeDimension, Key: aws.String("USAGE_TYPE")},
		},
	}
	var outputs []*awscostexplorer.GetCostAndUsageOutput
	for {
		var out *awscostexplorer.GetCostAndUsageOutput
		// Use generic retry with exponential backoff for BillExpirationException
		err := utils.Retry(5, 200*time.Millisecond, 3*time.Second, func(err error) bool {
			return common.IsBillExpirationError(err)
		}, func() error {
			m.RequestCount.Inc()
			resp, err := client.GetCostAndUsage(context.TODO(), input)
			if err != nil {
				m.RequestErrorsCount.Inc()
				return err
			}
			out = resp
			return nil
		})
		if err != nil {
			return &awsclient.BillingData{}, err
		}
		outputs = append(outputs, out)
		if out.NextPageToken == nil {
			break
		}
		input.NextPageToken = out.NextPageToken
	}
	return common.ParseBilling(outputs, "(?i)natgateway"), nil
}

func exportMetrics(b *awsclient.BillingData, m Metrics) {
	for region, model := range b.Regions {
		for component, pricing := range model.Model {
			// Normalize component checks for NATGateway-Hours / NatGateway-Bytes
			lc := strings.ToLower(component)
			parts := strings.SplitN(component, "|", 2)
			service := ""
			usageType := component
			if len(parts) == 2 {
				service = parts[0]
				usageType = parts[1]
			}
			if strings.Contains(lc, "natgateway-hours") {
				m.HourlyGauge.WithLabelValues(region, usageType, service).Set(pricing.UnitCost)
			} else if strings.Contains(lc, "natgateway-bytes") {
				m.DataProcessingGauge.WithLabelValues(region, usageType, service).Set(pricing.UnitCost)
			}
		}
	}
}
