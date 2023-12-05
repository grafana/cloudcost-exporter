package s3

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/cloudcost-exporter/pkg/provider"
)

// HoursInMonth is the average hours in a month, used to calculate the cost of storage
// If we wanted to be clever, we can get the number of hours in the current month
// 365.25 * 24 / 12 ~= 730.5
const (
	HoursInMonth = 730.5
	// This needs to line up with yace so we can properly join the data in PromQL
	StandardLabel = "StandardStorage"
)

// billingToRegionMap maps the AWS billing region code to the AWS region
// Billing codes: https://docs.aws.amazon.com/AmazonS3/latest/userguide/aws-usage-report-understand.html
// Regions: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-regions-availability-zones.html#concepts-available-regions
var billingToRegionMap = map[string]string{
	"APE1":                   "ap-east-1",      // Hong Kong
	"APN1":                   "ap-northeast-1", // Tokyo
	"APN2":                   "ap-northeast-2", // Seoul
	"APN3":                   "ap-northeast-3", // Osaka
	"APS1":                   "ap-southeast-1", // Singapore
	"APS2":                   "ap-southeast-2", // Sydney
	"APS3":                   "ap-south-1",     // Mumbai
	"APS4":                   "ap-southeast-3", // Jakarta is APS4, but is southeast-3
	"APS5":                   "ap-south-2",     // Hyderabad
	"APS6":                   "ap-southeast-4", // Melbourne
	"CAN1":                   "ca-central-1",   // Canada
	"CNN1":                   "cn-north-1",     // Beijing
	"CNW1":                   "cn-northwest-1", // Ningxia
	"CPT1":                   "af-south-1",     // Cape Town
	"EUC1":                   "eu-central-1",   // Frankfurt
	"EUC2":                   "eu-central-2",   // Zurich
	"EU":                     "eu-west-1",      // Ireland
	"EUW2":                   "eu-west-2",      // London
	"EUW3":                   "eu-west-3",      // Paris
	"EUN1":                   "eu-north-1",     // Stockholm
	"EUS1":                   "eu-south-1",     // Milan
	"EUS2":                   "eu-south-2",     // Spain
	"MEC1":                   "me-central-1",   // UAE
	"MES1":                   "me-south-1",     // Bahrain
	"SAE1":                   "sa-east-1",      // Sao Paulo
	"US":                     "us-east-1",      // N. Virginia, documentations state there could be no prefix
	"USE1":                   "us-east-1",      // N. Virginia
	"USE2":                   "us-east-2",      // Ohio
	"USW1":                   "us-west-1",      // N. California
	"USW2":                   "us-west-2",      // Oregon
	"AWS GovCloud (US-East)": "us-gov-east-1",
	"AWS GovCloud (US)":      "us-gov-west-1",
}

// Create two metrics that are gauges
var (
	// StorageGauge measures the cost of storage in $/GiB, per region and class.
	StorageGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_s3_storage_hourly_cost",
		Help: "S3 storage hourly cost in GiB",
	},
		[]string{"region", "class"},
	)

	// OperationsGauge measures the cost of operations in $/1k requests
	OperationsGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_s3_operations_cost",
		Help: "S3 operations cost per 1k requests",
	},
		[]string{"region", "class", "tier"},
	)

	// RequestCount is a counter that tracks the number of requests made to the AWS Cost Explorer API
	RequestCount = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "aws_cost_exporter_requests_total",
		Help: "Total number of requests made to the AWS Cost Explorer API",
	})

	// RequestErrorsCount is a counter that tracks the number of errors when making requests to the AWS Cost Explorer API
	RequestErrorsCount = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "aws_cost_exporter_request_errors_total",
		Help: "Total number of errors when making requests to the AWS Cost Explorer API",
	})

	// NextScrapeGuage is a gauge that tracks the next time the exporter will scrape AWS billing data
	NextScrapeGuage = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "aws_cost_exporter_next_scrape",
		Help: "The next time the exporter will scrape AWS billing data. Can be used to trigger alerts if now - nextScrape > interval",
	})
)

// Collector is the AWS implementation of the Collector interface
// It is responsible for registering and collecting metrics
type Collector struct {
	client     *costexplorer.Client
	interval   time.Duration
	nextScrape time.Time
}

// New creates a new Collector with a client and scrape interval defined.
func New(scrapeInterval time.Duration, client *costexplorer.Client) (*Collector, error) {
	return &Collector{
		client:   client,
		interval: scrapeInterval,
		// Initially Set nextScrape to the current time minus the scrape interval so that the first scrape will run immediately
		nextScrape: time.Now().Add(-scrapeInterval),
	}, nil
}

func (r *Collector) Name() string {
	return "S3"
}

// Register is called prior to the first collection. It registers any custom metric that needs to be exported for AWS billing data
func (r *Collector) Register(registry provider.Registry) error {
	registry.MustRegister(StorageGauge)
	registry.MustRegister(OperationsGauge)
	registry.MustRegister(RequestCount)
	registry.MustRegister(NextScrapeGuage)
	registry.MustRegister(RequestErrorsCount)

	return nil
}

// Collect is the function that will be called by the Prometheus client anytime a scrape is performed.
func (r *Collector) Collect() error {
	now := time.Now()
	// If the nextScrape time is in the future, return nil and do not scrape
	// :fire: This is to _mitigate_ expensive API calls to the cost explorer API
	if r.nextScrape.After(now) {
		return nil
	}
	r.nextScrape = time.Now().Add(r.interval)
	NextScrapeGuage.Set(float64(r.nextScrape.Unix()))
	return ExportBillingData(r.client)
}

// S3BillingData is the struct for the data we will be collecting
type S3BillingData struct {
	// Regions is a map where string is the region and PricingModel is the value
	Regions map[string]*PricingModel
}

type PricingModel struct {
	// Model is a map where string is the component and Pricing is the usage, cost and unit
	Model map[string]*Pricing
}

type Pricing struct {
	Usage    float64
	Cost     float64
	Units    string
	UnitCost float64
}

func NewS3BillingData() S3BillingData {
	return S3BillingData{
		// string represents the region
		Regions: make(map[string]*PricingModel),
	}
}

// AddMetricGroup adds a metric group to the Region. If the key is empty, it will not add the metric group
// to the Region. If the dimension is empty, it will not add the metric group to the Region.
// Dimensions are cumulative and will be added to the same dimension if the dimension already exists.
func (s S3BillingData) AddMetricGroup(region string, component string, group types.Group) {
	if region == "" || component == "" {
		return
	}

	// Check if the region is in the billingToRegionMap
	// If not we need to instantiate the map, otherwise it will panic
	if _, ok := s.Regions[region]; !ok {
		s.Regions[region] = &PricingModel{
			Model: make(map[string]*Pricing),
		}
	}

	// Check if the component is in the map
	// If not we need to instantiate the map, otherwise it will panic
	if _, ok := s.Regions[region].Model[component]; !ok {
		s.Regions[region].Model[component] = &Pricing{}
	}

	componentsMap := s.Regions[region].Model[component]
	for name, metric := range group.Metrics {
		if name == "UsageQuantity" {
			usageAmount, err := strconv.ParseFloat(*metric.Amount, 64)
			if err != nil {
				fmt.Printf("Error parsing usage amount: %v\n", err)
				continue
			}
			componentsMap.Usage += usageAmount
			componentsMap.Units = *metric.Unit
		}

		if name == "UnblendedCost" {
			cost, err := strconv.ParseFloat(*metric.Amount, 64)
			if err != nil {
				fmt.Printf("Error parsing cost amount: %v\n", err)
				continue
			}
			componentsMap.Cost += cost
		}
		componentsMap.UnitCost = unitCostForComponent(component, componentsMap)
	}
}

// getBillingData is responsible for making the API call to the AWS Cost Explorer API and parsing the response
// into a S3BillingData struct
func getBillingData(client *costexplorer.Client, startDate time.Time, endDate time.Time) (S3BillingData, error) {
	log.Printf("Getting billing data for %s to %s\n", startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	input := &costexplorer.GetCostAndUsageInput{
		TimePeriod: &types.DateInterval{
			Start: aws.String(startDate.Format("2006-01-02")), // Specify the start date
			End:   aws.String(endDate.Format("2006-01-02")),   // Specify the end date
		},
		Granularity: types.GranularityDaily,
		Metrics:     []string{"UsageQuantity", "UnblendedCost"},
		// NB: You can only pass in one USAGE_TYPE per query
		GroupBy: []types.GroupDefinition{
			{
				Type: types.GroupDefinitionTypeDimension,
				Key:  aws.String("USAGE_TYPE"),
			},
		},
		Filter: &types.Expression{
			Dimensions: &types.DimensionValues{
				Key:    types.DimensionService,
				Values: []string{"Amazon Simple Storage Service"},
			},
		},
	}

	var outputs []*costexplorer.GetCostAndUsageOutput
	for {
		RequestCount.Inc()
		output, err := client.GetCostAndUsage(context.TODO(), input)
		if err != nil {
			log.Printf("Error getting cost and usage: %v\n", err)
			RequestErrorsCount.Inc()
			return S3BillingData{}, err
		}
		outputs = append(outputs, output)
		if output.NextPageToken == nil {
			break
		}
		input.NextPageToken = output.NextPageToken
	}

	return parseBillingData(outputs), nil
}

// parseBillingData takes the output from the AWS Cost Explorer API and parses it into a S3BillingData struct
func parseBillingData(outputs []*costexplorer.GetCostAndUsageOutput) S3BillingData {
	billingData := NewS3BillingData()

	// Process the billing data in the 'output' variable
	for _, output := range outputs {
		for _, result := range output.ResultsByTime {
			for _, group := range result.Groups {
				key := group.Keys[0]
				region := getRegionFromKey(key)
				component := getComponentFromKey(key)
				if region == "" || component == "" {
					continue
				}
				billingData.AddMetricGroup(region, component, group)
			}
		}
	}
	return billingData
}

// getRegionFromKey returns the region from the key. If the key is requests, it will return an empty string because there is no region associated with it.
func getRegionFromKey(key string) string {
	if key == "Requests-Tier1" || key == "Requests-Tier2" {
		return ""
	}

	split := strings.Split(key, "-")
	if len(split) < 2 {
		log.Printf("Could not find region in key: %s\n", key)
		return ""
	}

	billingRegion := split[0]
	if region, ok := billingToRegionMap[billingRegion]; ok {
		return region
	}
	log.Printf("Could not find mapped region: %s:%s\n", key, billingRegion)
	return ""
}

// getComponentFromKey returns the component from the key. If the component does not contain a region, it will return
// an empty string. If the component is requests, it will return the tier as well.
func getComponentFromKey(key string) string {
	if key == "Requests-Tier1" || key == "Requests-Tier2" {
		return ""
	}
	val := ""
	split := strings.Split(key, "-")
	if len(split) < 2 {
		return val
	}
	val = split[1]
	// Check to see if the value is a region. If so, set val to empty string to skip the dimension
	// Currently this is such a minor part of our bill that it's not worth it.
	if _, ok := billingToRegionMap[val]; ok {
		val = ""
	}
	// If it's requests, we want to include if it's tier 1 or tier 2
	if val == "Requests" {
		val += "-" + split[2]
	}
	return val
}

// ExportBillingData will query the previous 30 days of S3 billing data and export it to the prometheus metrics
func ExportBillingData(client *costexplorer.Client) error {
	// We go one day into the past as the current days billing data has no guarantee of being complete
	endDate := time.Now().AddDate(0, 0, -1)
	// Current assumption is that we're going to pull 30 days worth of billing data
	startDate := endDate.AddDate(0, 0, -30)
	s3BillingData, err := getBillingData(client, startDate, endDate)
	if err != nil {
		return err
	}

	exportMetrics(s3BillingData)
	return nil
}

// exportMetrics will iterate over the S3BillingData and export the metrics to prometheus
func exportMetrics(s3BillingData S3BillingData) {
	log.Printf("Exporting metrics for %d regions\n", len(s3BillingData.Regions))
	for region, pricingModel := range s3BillingData.Regions {
		for component, pricing := range pricingModel.Model {
			switch component {
			case "Requests-Tier1":
				OperationsGauge.WithLabelValues(region, StandardLabel, "1").Set(pricing.UnitCost)
			case "Requests-Tier2":
				OperationsGauge.WithLabelValues(region, StandardLabel, "2").Set(pricing.UnitCost)
			case "TimedStorage":
				StorageGauge.WithLabelValues(region, StandardLabel).Set(pricing.UnitCost)
			}
		}
	}
}

// unitCostForComponent will calculate the unit cost for a given component. This is necessary because the
// unit cost will depend on the type of component.
func unitCostForComponent(component string, pricing *Pricing) float64 {
	switch component {
	case "Requests-Tier1", "Requests-Tier2":
		return pricing.Cost / (pricing.Usage / 1000)
	case "TimedStorage":
		return (pricing.Cost / HoursInMonth) / pricing.Usage
	default:
		return pricing.Cost / pricing.Usage
	}
}
