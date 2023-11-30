package gcs

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	"cloud.google.com/go/storage"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/api/iterator"
)

var (
	StorageGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gcp_gcs_storage_hourly_cost",
		Help: "GCS storage hourly cost in GiB",
	},
		[]string{"location", "storage_class"},
	)

	StorageDiscountGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gcp_gcs_storage_discount",
		Help: "GCS storage discount",
	}, []string{"location", "storage_class"})

	OperationsGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gcp_gcs_operations_cost",
		Help: "GCS operations cost per 1k requests",
	},
		[]string{"location", "storage_class", "opclass"},
	)
	OperationsDiscountGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gcp_gcs_operations_discount",
		Help: "GCS operations discount",
	}, []string{"location_type", "storage_class", "opclass"})

	BucketInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gcp_gcs_bucket_info",
		Help: "GCS bucket info",
	},
		[]string{"location", "location_type", "storage_class", "bucket_name"},
	)

	NextScrapeScrapeGuage = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "gcp_cost_exporter_next_scrape",
		Help: "The next time the exporter will scrape GCP billing data. Can be used to trigger alerts if now - nextScrape > interval",
	})

	GCSBucketListHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "cloudcost_exporter_gcs_bucket_list_duration_seconds",
		Help: "Histogram for the duration of GCS bucket list operations",
	}, []string{"project_id"})

	GCSBucketListStatus = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "cloudcost_exporter_gcs_bucket_list_status",
		Help: "Status of GCS bucket list operations",
	}, []string{"project_id", "status"})
	CloudCostExporterHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "cloudcost_exporter_duration_seconds",
		Help: "Histogram for the duration of cloudcost exporter operations",
	}, []string{"provider"})
)

var (
	storageClasses = []string{"Standard", "Regional", "Nearline", "Coldline", "Archive"}
	baseRegions    = []string{"asia", "eu", "us", "asia1", "eur4", "nam4"}
)

// This data was pulled from https://console.cloud.google.com/billing/01330B-0FCEED-DEADF1/pricing?organizationId=803894190427&project=grafanalabs-global on 2023-07-28
// @pokom purposefully left out three discounts that don't fit:
// 1. Region Standard Tagging Class A Operations
// 2. Region Standard Tagging Class B Operations
// 3. Duplicated Regional Standard Class B Operations
// Filter on `Service Description: storage` and `Sku Description: operations`
// TODO: Pull this data directly from BigQuery
var operationsDiscountMap = map[string]map[string]map[string]float64{
	"region": {
		"archive": {
			"class-a": 0.190,
			"class-b": 0.190,
		},
		"coldline": {
			"class-a": 0.595,
			"class-b": 0.190,
		},
		"nearline": {
			"class-a": 0.190,
			"class-b": 0.190,
		},
		"standard": {
			"class-a": 0.190,
			"class-b": 0.190,
		},
		"regional": {
			"class-a": 0.190,
			"class-b": 0.190,
		},
	},
	"multi-region": {
		"coldline": {
			"class-a": 0.795,
			"class-b": 0.190,
		},
		"nearline": {
			"class-a": 0.595,
			"class-b": 0.190,
		},
		"standard": {
			"class-a": 0.595,
			"class-b": 0.190,
		},
		"multi_regional": {
			"class-a": 0.595,
			"class-b": 0.190,
		},
	},
	"dual-region": {
		"standard": {
			"class-a": 0.595,
			"class-b": 0.190,
		},
		"multi_regional": {
			"class-a": 0.595,
			"class-b": 0.190,
		},
	},
}

type Collector struct {
	ProjectID     string
	Projects      []string
	client        *billingv1.CloudCatalogClient
	serviceName   string
	ctx           context.Context
	interval      time.Duration
	nextScrape    time.Time
	regionsClient *compute.RegionsClient
	bucketClient  *BucketClient
	discount      int
	CachedBuckets *BucketCache
}

type BucketCache struct {
	Buckets map[string][]*storage.BucketAttrs
	m       sync.RWMutex
}

func (c *BucketCache) Get(project string) []*storage.BucketAttrs {
	c.m.RLock()
	defer c.m.RUnlock()
	return c.Buckets[project]
}

func (c *BucketCache) Set(project string, buckets []*storage.BucketAttrs) {
	c.m.Lock()
	defer c.m.Unlock()
	c.Buckets[project] = buckets
}

func NewBucketCache() *BucketCache {
	return &BucketCache{
		Buckets: make(map[string][]*storage.BucketAttrs),
		m:       sync.RWMutex{},
	}
}

type Config struct {
	ProjectId       string
	Projects        string
	DefaultDiscount int
	ScrapeInterval  time.Duration
}

func New(config *Config, billingClient *billingv1.CloudCatalogClient, regionsClient *compute.RegionsClient, storageClient StorageClientInterface) (*Collector, error) {
	if config.ProjectId == "" {
		return nil, fmt.Errorf("projectID cannot be empty")
	}
	ctx := context.Background()

	projects := strings.Split(config.Projects, ",")
	if len(projects) == 1 && projects[0] == "" {
		log.Printf("No bucket projects specified, defaulting to %s", config.ProjectId)
		projects = []string{config.ProjectId}
	}
	bucketClient := NewBucketClient(storageClient)

	serviceName, err := getServiceNameByReadableName(ctx, billingClient, "Cloud Storage")
	if err != nil {
		return nil, err
	}
	return &Collector{
		ProjectID:     config.ProjectId,
		Projects:      projects,
		client:        billingClient,
		regionsClient: regionsClient,
		bucketClient:  bucketClient,
		discount:      config.DefaultDiscount,
		ctx:           ctx,
		serviceName:   serviceName,
		interval:      config.ScrapeInterval,
		// Set nextScrape to the current time minus the scrape interval so that the first scrape will run immediately
		nextScrape:    time.Now().Add(-config.ScrapeInterval),
		CachedBuckets: NewBucketCache(),
	}, nil
}

func (c *Collector) Name() string {
	return "GCS"
}

func getServiceNameByReadableName(ctx context.Context, client *billingv1.CloudCatalogClient, name string) (string, error) {
	serviceList := client.ListServices(ctx, &billingpb.ListServicesRequest{})
	for {
		row, err := serviceList.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return "", err
		}
		if row.DisplayName == name {
			return row.Name, nil
		}
	}
	return "", fmt.Errorf("service \"%s\" not found", name)
}

func (r *Collector) Register(registry *prometheus.Registry) error {
	log.Printf("Registering GCS metrics")
	registry.MustRegister(StorageGauge)
	registry.MustRegister(StorageDiscountGauge)
	registry.MustRegister(OperationsDiscountGauge)
	registry.MustRegister(OperationsGauge)
	registry.MustRegister(BucketInfo)
	registry.MustRegister(GCSBucketListHistogram)
	registry.MustRegister(GCSBucketListStatus)
	registry.MustRegister(CloudCostExporterHistogram)
	registry.MustRegister(NextScrapeScrapeGuage)
	return nil
}

func (c *Collector) Collect() error {
	log.Printf("Collecting GCS metrics")
	now := time.Now()

	// If the nextScrape time is in the future, return nil and do not scrape
	// Billing API calls are free in GCP, just use this logic so metrics are similiar to AWSD
	if c.nextScrape.After(now) {
		return nil
	}
	defer CloudCostExporterHistogram.WithLabelValues("gcp").Observe(time.Since(now).Seconds())
	c.nextScrape = time.Now().Add(c.interval)
	NextScrapeScrapeGuage.Set(float64(c.nextScrape.Unix()))
	ExporterOperationsDiscounts()
	err := ExportRegionalDiscounts(c.ctx, c.regionsClient, c.ProjectID, c.discount)
	if err != nil {
		log.Printf("Error exporting regional discounts: %v", err)
	}
	err = ExportBucketInfo(c.ctx, c.bucketClient, c.Projects, c.CachedBuckets)
	if err != nil {
		log.Printf("Error exporting bucket info: %v", err)
	}
	return ExportGCPCostData(c.ctx, c.client, c.serviceName)
}

// ExportBucketInfo will list all buckets for a given project and export the data as a prometheus metric.
// If there are any errors listing buckets, it will export the cached buckets for the project.
func ExportBucketInfo(ctx context.Context, client *BucketClient, projects []string, cachedBuckets *BucketCache) error {
	var buckets []*storage.BucketAttrs
	for _, project := range projects {
		start := time.Now()

		var err error
		buckets, err = client.List(ctx, project)
		if err != nil {
			// We don't want to block here as it's not critical to the exporter
			log.Printf("error listing buckets for %s: %v", project, err)
			GCSBucketListHistogram.WithLabelValues(project).Observe(time.Since(start).Seconds())
			GCSBucketListStatus.WithLabelValues(project, "error").Inc()
			buckets = cachedBuckets.Get(project)
			log.Printf("pulling %d cached buckets for project %s", len(buckets), project)
		}

		log.Printf("updating cached buckets for %s", project)
		cachedBuckets.Set(project, buckets)

		for _, bucket := range buckets {
			// Location is always in caps, and the metrics that need to join up on it are in lower case
			BucketInfo.WithLabelValues(strings.ToLower(bucket.Location), bucket.LocationType, bucket.StorageClass, bucket.Name).Set(1)
		}
		GCSBucketListHistogram.WithLabelValues(project).Observe(time.Since(start).Seconds())
		GCSBucketListStatus.WithLabelValues(project, "success").Inc()
	}

	return nil
}

func ExportRegionalDiscounts(ctx context.Context, client *compute.RegionsClient, projectID string, discount int) error {
	req := &computepb.ListRegionsRequest{
		Project: projectID,
	}
	it := client.List(ctx, req)
	regions := make([]string, 0)
	for {
		resp, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return fmt.Errorf("error getting regions: %v", err)
		}
		regions = append(regions, *resp.Name)
	}

	percentDiscount := float64(discount) / 100.0
	for _, storageClass := range storageClasses {
		for _, region := range regions {
			StorageDiscountGauge.WithLabelValues(region, strings.ToUpper(storageClass)).Set(percentDiscount)
		}
		// Base Regions are specific to `MULTI_REGION` buckets that do not have a specific region
		// Breakdown for buckets with these regions: https://ops.grafana-ops.net/explore?panes=%7B%229oU%22:%7B%22datasource%22:%22000000134%22,%22queries%22:%5B%7B%22refId%22:%22A%22,%22expr%22:%22sum%28count%20by%20%28bucket_name%29%20%28stackdriver_gcs_bucket_storage_googleapis_com_storage_total_bytes%7Blocation%3D~%5C%22asia%7Ceu%7Cus%5C%22%7D%29%29%22,%22range%22:true,%22instant%22:true,%22datasource%22:%7B%22type%22:%22prometheus%22,%22uid%22:%22000000134%22%7D,%22editorMode%22:%22code%22,%22legendFormat%22:%22__auto%22%7D,%7B%22refId%22:%22B%22,%22expr%22:%22sum%28count%20by%20%28bucket_name%29%20%28stackdriver_gcs_bucket_storage_googleapis_com_storage_total_bytes%7Blocation%21~%5C%22asia%7Ceu%7Cus%5C%22%7D%29%29%22,%22range%22:true,%22instant%22:true,%22datasource%22:%7B%22type%22:%22prometheus%22,%22uid%22:%22000000134%22%7D,%22editorMode%22:%22code%22,%22legendFormat%22:%22__auto%22%7D%5D,%22range%22:%7B%22from%22:%22now-6h%22,%22to%22:%22now%22%7D%7D%7D&schemaVersion=1&orgId=1
		for _, region := range baseRegions {
			if storageClass == "Regional" {
				// This is a hack to align storage classes with stackdriver_exporter
				storageClass = "MULTI_REGIONAL"
			}
			StorageDiscountGauge.WithLabelValues(region, strings.ToUpper(storageClass)).Set(percentDiscount)
		}
	}

	return nil
}

func ExporterOperationsDiscounts() {
	for locationType, locationMap := range operationsDiscountMap {
		for storageClass, storageClassmap := range locationMap {
			for opsClass, discount := range storageClassmap {
				OperationsDiscountGauge.WithLabelValues(locationType, strings.ToUpper(storageClass), opsClass).Set(discount)
			}
		}
	}
}

func ExportGCPCostData(ctx context.Context, client *billingv1.CloudCatalogClient, serviceName string) error {
	skuResponse := client.ListSkus(ctx, &billingpb.ListSkusRequest{
		Parent: serviceName,
	})
	for {
		sku, err := skuResponse.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return fmt.Errorf("error getting skus: %v", err)
		}
		// Skip Egress and Download costs as we don't count them yet
		// Check category first as I've had random segfaults locally
		if sku.Category != nil && sku.Category.ResourceFamily == "Network" {
			continue
		}
		if strings.HasSuffix(sku.Description, "Data Retrieval") {
			continue
		}
		if sku.Description == "Autoclass Management Fee" {
			continue
		}
		if sku.Description == "Bucket Tagging Storage" {
			continue
		}
		if strings.HasSuffix(sku.Category.ResourceGroup, "Storage") {
			if strings.Contains(sku.Description, "Early Delete") {
				continue // to skip "Unknown sku"
			}
			parseStorageSku(sku)
			continue
		}
		if strings.HasSuffix(sku.Category.ResourceGroup, "Ops") {
			parseOpSku(sku)
			continue
		}
		fmt.Fprintf(os.Stderr, "Unknown sku: %s\n", sku.Description)
	}
	return nil
}

func getPriceFromSku(sku *billingpb.Sku) (float64, error) {
	// TODO: Do we need to support Multiple PricingInfo?
	// That not needed here as we query only actual pricing
	if len(sku.PricingInfo) < 1 {
		return 0.0, fmt.Errorf("found sku without PricingInfo: %+v", sku)
	}
	priceInfo := sku.PricingInfo[0]

	// PricingInfo could have several Costs Tiers.
	// From observed data when there are several tiers first tiers are "free tiers",
	// so we should return actual prices.
	tierRatesLen := len(priceInfo.PricingExpression.TieredRates)
	if tierRatesLen < 1 {
		return 0.0, fmt.Errorf("found sku without TieredRates: %+v", sku)
	}
	tierRate := priceInfo.PricingExpression.TieredRates[tierRatesLen-1]

	return 1e-9 * float64(tierRate.UnitPrice.Nanos), nil // Convert NanoUSD to USD when return
}

func parseStorageSku(sku *billingpb.Sku) {
	priceInfo := sku.PricingInfo[0]
	price, err := getPriceFromSku(sku)
	if err != nil {
		// TODO(fedor): just skip bad sku for now
		log.Printf("error getting price for sku: %v", err)
	}
	priceUnit := priceInfo.PricingExpression.UsageUnitDescription

	// Adjust price to hourly
	if priceUnit == "gibibyte month" {
		price = price / 31 / 24
	} else if priceUnit == "gibibyte day" { // For Early-Delete in Archive, CloudStorage and Nearline classes
		price = price / 24
	} else {
		fmt.Fprintf(os.Stderr, "Unknown price unit for SKU: %s, unit: %s\n", sku.Description, priceUnit)
	}

	region := RegionNameSameAsStackdriver(sku.ServiceRegions[0])
	storageclass := StorageClassFromSkuDescription(sku.Description, region)
	StorageGauge.WithLabelValues(region, storageclass).Set(price)
}

func parseOpSku(sku *billingpb.Sku) {
	if strings.Contains(sku.Description, "Tagging") {
		return
	}

	price, err := getPriceFromSku(sku)
	if err != nil {
		// TODO(fedor): just skip bad sku for now
		log.Printf("error getting price for sku: %v", err)
	}

	region := RegionNameSameAsStackdriver(sku.ServiceRegions[0])
	storageclass := StorageClassFromSkuDescription(sku.Description, region)
	opclass := OpClassFromSkuDescription(sku.Description)

	OperationsGauge.WithLabelValues(region, storageclass, opclass).Set(price)
}

// Return StorageClass similiar to what StackDriver has
func StorageClassFromSkuDescription(s string, region string) string {
	if strings.Contains(s, "Coldline") {
		return "COLDLINE"
	} else if strings.Contains(s, "Nearline") {
		return "NEARLINE"
	} else if strings.Contains(s, "Durable Reduced Availability") {
		return "DRA"
	} else if strings.Contains(s, "Archive") {
		return "ARCHIVE"
	} else if strings.Contains(s, "Dual-Region") || strings.Contains(s, "Dual-region") {
		// Iowa and South Carolina regions (us-central1 and us-east1) are using "REGIONAL"
		// in billing and pricing, but sku.description state SKU as "Dual-region"
		if region == "us-central1" || region == "us-east1" {
			return "REGIONAL"
		}
		return "MULTI_REGIONAL"
	} else if strings.Contains(s, "Multi-Region") || strings.Contains(s, "Multi-region") {
		return "MULTI_REGIONAL"
	} else if strings.Contains(s, "Regional") || strings.Contains(s, "Storage") || strings.Contains(s, "Standard") {
		return "REGIONAL"
	}
	return s
}

func OpClassFromSkuDescription(s string) string {
	if strings.Contains(s, "Class A") {
		return "class-a"
	} else if strings.Contains(s, "Class B") {
		return "class-b"
	}
	return s
}

// Google Cost API returns region names exactly the same how they are refered in StackDriver metrics,
// except one case:
// For Europe multi-region:
// API returns "europe", while Stackdriver uses "eu" label value.
func RegionNameSameAsStackdriver(s string) string {
	if s == "europe" {
		return "eu"
	}
	return s
}
