package metrics

import (
	cloudcost_exporter "github.com/grafana/cloudcost-exporter"
	"github.com/prometheus/client_golang/prometheus"
)

const subsystem = "gcp_gcs"

type Metrics struct {
	StorageGauge            *prometheus.GaugeVec
	StorageDiscountGauge    *prometheus.GaugeVec
	OperationsGauge         *prometheus.GaugeVec
	OperationsDiscountGauge *prometheus.GaugeVec
	BucketInfo              *prometheus.GaugeVec
	BucketListHistogram     *prometheus.HistogramVec
	BucketListStatus        *prometheus.CounterVec
	NextScrapeGauge         prometheus.Gauge
}

func NewMetrics() *Metrics {
	return &Metrics{
		StorageGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "storage_by_location_usd_per_gibyte_hour"),
			Help: "Storage cost of GCS objects by location and storage_class. Cost represented in USD/(GiB*h)",
		},
			[]string{"location", "storage_class"},
		),
		StorageDiscountGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "storage_discount_by_location_usd_per_gibyte_hour"),
			Help: "Discount for storage cost of GCS objects by location and storage_class. Cost represented in USD/(GiB*h)",
		}, []string{"location", "storage_class"}),
		OperationsGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "operation_by_location_usd_per_krequest"),
			Help: "Operation cost of GCS objects by location, storage_class, and opclass. Cost represented in USD/(1k req)",
		},
			[]string{"location", "storage_class", "opclass"},
		),
		OperationsDiscountGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "operation_discount_by_location_usd_per_krequest"),
			Help: "Discount for operation cost of GCS objects by location, storage_class, and opclass. Cost represented in USD/(1k req)",
		}, []string{"location_type", "storage_class", "opclass"}),
		BucketInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.MetricPrefix, subsystem, "bucket_info"),
			Help: "Location, location_type and storage class information for a GCS object by bucket_name",
		},
			[]string{"location", "location_type", "storage_class", "bucket_name"},
		),
		// todo: every module so far has a "next_scrape" metric. Should we have a metric cloudcost_exporter_next_scrape{module=<gcp_gcs,gcp_compute,aws...>}?
		NextScrapeGauge: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "next_scrape"),
			Help: "The next time the exporter will scrape GCP billing data. Can be used to trigger alerts if now - nextScrape > interval",
		}),

		BucketListHistogram: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "bucket_list_duration_seconds"),
			Help: "Histogram for the duration of GCS bucket list operations in seconds",
		}, []string{"project_id"}),

		BucketListStatus: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: prometheus.BuildFQName(cloudcost_exporter.ExporterName, subsystem, "bucket_list_status_total"),
			Help: "Status of GCS bucket list operations",
		}, []string{"project_id", "status"})}
}
