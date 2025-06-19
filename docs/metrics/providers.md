# Provider Metrics

Baseline metrics that are exported when you run `cloudcost-exporter` with any of the supported providers.
These metrics are meant to be generic enough to create an operational dashboard and alerting rules for the exporter.
Each provider _must_ implement these metrics.

| Metric name                                                   | Metric type | Description                                   | Labels                                                                                        |
|---------------------------------------------------------------|-------------|-----------------------------------------------|-----------------------------------------------------------------------------------------------|
| cloudcost_exporter_collector_scrapes_total                | Counter     | Total number of scrapes, by collector.        | `provider`=&lt;name of the provider&gt; <br/> `collector`=&lt;name of the collector&gt; <br/> |
| cloudcost_exporter_collector_last_scrape_duration_seconds | Gauge       | Duration of the last scrape in seconds. | `provider`=&lt;name of the provider&gt; <br/> `collector`=&lt;name of the collector&gt; <br/> |
| cloudcost_exporter_collector_last_scrape_error            | Gauge       | Was the last scrape an error. 1 is an error.  | `provider`=&lt;name of the provider&gt; <br/> `collector`=&lt;name of the collector&gt; <br/> |

 
