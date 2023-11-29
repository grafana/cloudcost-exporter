package google_pricing_export

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/api/iterator"

	"github.com/grafana/cloudcost-exporter/pkg/google/gcs"
)

var commonLabels = []string{"location"}
var bucketTags = []string{"bucket_env", "bucket_team"}
var cloudSqlTags = []string{"instance_env", "instance_team"}

var (
	ObjectStorageDailyCost = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "object_storage_daily_cost",
		},
		append(commonLabels, bucketTags...),
	)

	CloudSqlDailyCost = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "cloudsql_daily_cost",
		},
		append(commonLabels, cloudSqlTags...),
	)
	StorageGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gcp_gcs_storage_hourly_cost",
		Help: "GCS storage hourly cost in GiB",
	},
		[]string{"location", "storage_class"},
	)

	StorageListPriceGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gcp_gcs_storage_hourly_cost_list_price",
		Help: "GCS storage hourly cost in GiB list price",
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

	OperationsListPriceGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gcp_gcs_operations_cost_list_price",
		Help: "GCS operations cost per 1k requests list price",
	},
		[]string{"location", "storage_class", "opclass"},
	)

	OperationsDiscountGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gcp_gcs_operations_discount",
		Help: "GCS operations cost discount",
	},
		[]string{"location", "storage_class", "opclass"},
	)

	NextScrapeScrapeGuage = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "gcp_cost_exporter_next_scrape",
		Help: "The next time the exporter will scrape GCP billing data. Can be used to trigger alerts if now - nextScrape > interval",
	})
)

type Collector struct {
	ProjectID        string
	TableName        string
	client           *bigquery.Client
	ctx              context.Context
	interval         time.Duration
	nextScrape       time.Time
	maxPartitionTime time.Time
}
type BillingQueryArgs struct {
	TableName     string
	ServiceNames  []string
	PartitionTime time.Time
	QueryDate     string
}

func NewCollector(projectID string, scrapeInterval time.Duration, tableName string) (*Collector, error) {
	if projectID == "" {
		return nil, fmt.Errorf("projectID cannot be empty")
	}
	ctx := context.Background()

	client, err := bigquery.NewClient(ctx, projectID)
	if err != nil {
		log.Fatalf("bigquery.NewClient: %v", err)
	}

	return &Collector{
		ProjectID:        projectID,
		TableName:        tableName,
		client:           client,
		ctx:              ctx,
		interval:         scrapeInterval,
		nextScrape:       time.Now().Add(-scrapeInterval),
		maxPartitionTime: time.Now(),
	}, nil
}

func (r *Collector) Register(registry *prometheus.Registry) error {
	registry.MustRegister(StorageGauge)
	registry.MustRegister(StorageListPriceGauge)
	registry.MustRegister(StorageDiscountGauge)
	registry.MustRegister(OperationsGauge)
	registry.MustRegister(OperationsListPriceGauge)
	registry.MustRegister(OperationsDiscountGauge)
	return nil
}

type maxPartitionRecord struct {
	MaxPartitionTime time.Time `bigquery:"max_partition_time"`
}

func (c *Collector) Collect() error {
	now := time.Now()
	if c.nextScrape.After(now) {
		return nil
	}
	c.nextScrape = time.Now().Add(c.interval)
	NextScrapeScrapeGuage.Set(float64(c.nextScrape.Unix()))

	maxPartitionQuery := c.client.Query(
		"SELECT max(_PARTITIONTIME) as max_partition_time FROM " + fmt.Sprintf("`%s`", c.TableName),
	)
	it, err := maxPartitionQuery.Read(c.ctx)
	if err != nil {
		return err
	}
	for {
		var value maxPartitionRecord
		err := it.Next(&value)
		if err == iterator.Done {
			return fmt.Errorf("failed to query for max PartitionTime")
		}
		if value.MaxPartitionTime == c.maxPartitionTime {
			// now new records, don't need to update metrics
			return nil
		}
		queryArgs := BillingQueryArgs{
			TableName:     c.TableName,
			ServiceNames:  []string{"Cloud Storage"},
			PartitionTime: value.MaxPartitionTime,
		}
		iter, err := query(c.ctx, c.client, queryArgs)
		if err != nil {
			return err
		}

		err = parseBucketMetricsFromBilling(iter)
		if err != nil {
			return err
		}
		c.maxPartitionTime = value.MaxPartitionTime
		break
	}
	return nil
}

// Query returns a row iterator suitable for reading Query results.
func query(ctx context.Context, client *bigquery.Client, queryArgs BillingQueryArgs) (*bigquery.RowIterator, error) {

	// TODO: use SKU to count different metrics: storage, requests, network traffic, etc.
	query := client.Query(
		`
		SELECT
		service.description as service,
		sku.description as sku,
		product_taxonomy,
		geo_taxonomy.regions,
		list_price.tiered_rates as list_price,
		billing_account_price.tiered_rates as contracted_price
		FROM ` + fmt.Sprintf("`%s`", queryArgs.TableName) + `
		WHERE
		_PARTITIONTIME = @queryDate
		AND service.description in UNNEST(@serviceNames)`)
	query.Parameters = []bigquery.QueryParameter{
		{Name: "queryDate", Value: queryArgs.PartitionTime},
		{Name: "serviceNames", Value: queryArgs.ServiceNames},
	}
	return query.Read(ctx)
}

type PricingRecord struct {
	PricingUnitQuantity   float64 `bigquery:"pricing_unit_quantity"`
	StartUsageAmount      float64 `bigquery:"start_usage_amount"`
	UsdAmount             float64 `bigquery:"usd_amount"`
	AccountCurrencyAmount float64 `bigquery:"account_currency_amount"`
}
type BillingRecord struct {
	Service         string          `bigquery:"service"`
	SKU             string          `bigquery:"sku"`
	ProductTaxonomy []string        `bigquery:"product_taxonomy"`
	Regions         []string        `bigquery:"regions"`
	ListPrice       []PricingRecord `bigquery:"list_price"`
	ContractedPrice []PricingRecord `bigquery:"contracted_price"`
}

func getLocationName(regions []string) string {
	if len(regions) == 0 {
		return "GLOBAL"
	}
	if len(regions) == 1 {
		return regions[0]
	}
	dualRegions := map[string]string{
		"asia":      "asia1",
		"australia": "oceania1", // Networking Traffic Egress GCP Replication within Oceania
		"europe":    "eur4",
		"us":        "nam4",
	}
	if len(regions) == 2 { // DUAL_REGIONAL: nam4, eur4, asia1
		for prefix, region := range dualRegions {
			if strings.HasPrefix(regions[0], prefix) {
				return region
			}
		}
		return "uknownDualRegion"
	}

	multiRegions := []string{"asia", "europe", "us"}
	for _, prefix := range multiRegions {
		if strings.HasPrefix(regions[0], prefix) {
			return prefix
		}
	}
	return "unknownLocation"
}

func parseBucketMetricsFromBilling(iter *bigquery.RowIterator) error {
	for {
		var row BillingRecord
		err := iter.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("error iterating through results: %w", err)
		}
		if row.Service != "Cloud Storage" {
			continue
		}
		location := getLocationName(row.Regions)
		storageClass := gcs.StorageClassFromSkuDescription(row.SKU, location)
		if len(row.ProductTaxonomy) < 4 ||
			row.ProductTaxonomy[0] != "GCP" ||
			row.ProductTaxonomy[1] != "Storage" ||
			row.ProductTaxonomy[2] != "GCS" {
			// not a GCS record
			log.Printf("failed to parse product_taxonomy: %v", row.ProductTaxonomy)
			continue
		}
		if len(row.ListPrice) == 0 || len(row.ContractedPrice) == 0 {
			log.Printf("row with no prices: %v", row)
			continue
		}
		listPrice := row.ListPrice[len(row.ListPrice)-1]
		contractedPrice := row.ContractedPrice[len(row.ContractedPrice)-1]

		switch row.ProductTaxonomy[3] {
		case "Storage":
			if row.ProductTaxonomy[len(row.ProductTaxonomy)-1] == "Early Delete" {
				continue
			}
			labels := prometheus.Labels{"location": location, "storage_class": storageClass}
			StorageListPriceGauge.With(labels).Set(listPrice.UsdAmount)
			StorageGauge.With(labels).Set(contractedPrice.UsdAmount)
			StorageDiscountGauge.With(labels).Set(1.0 - contractedPrice.UsdAmount/listPrice.UsdAmount)
		case "Ops":
			labels := prometheus.Labels{
				"location":      location,
				"storage_class": storageClass,
				"opclass":       gcs.OpClassFromSkuDescription(row.SKU),
			}
			OperationsListPriceGauge.With(labels).Set(listPrice.UsdAmount)
			OperationsGauge.With(labels).Set(contractedPrice.UsdAmount)
			OperationsDiscountGauge.With(labels).Set(1.0 - contractedPrice.UsdAmount/listPrice.UsdAmount)
		case "Network":
			continue
		case "Egress":
			continue
		default:
			log.Printf("uknown product_taxonomy for %v", row.ProductTaxonomy)
			continue

		}
	}
	return nil
}
