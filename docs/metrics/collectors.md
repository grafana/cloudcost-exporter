# Collector Metrics

Metrics exported by the gatherer for each collector scrape operation. These metrics provide operational insights into collector performance and reliability.

| Metric name                                          | Metric type | Description                                                      | Labels                                    |
|------------------------------------------------------|-------------|------------------------------------------------------------------|-------------------------------------------|
| cloudcost_exporter_collector_duration_seconds        | Histogram   | Duration of a collector scrape in seconds                       | `collector`=&lt;name of the collector&gt; |
| cloudcost_exporter_collector_total                   | Counter     | Total number of scrapes performed by a collector                | `collector`=&lt;name of the collector&gt; |
| cloudcost_exporter_collector_error_total             | Counter     | Total number of errors that occurred during collector scrapes   | `collector`=&lt;name of the collector&gt; |

## Notes

These metrics are replacing `cloudcost_exporter_collector_success`, `cloudcost_collector_last_scrape_error`, `cloudcost_collector_last_scrape_time` and `cloudcost_exporter_<csp>`. Collector metrics can monitor scrapes for each collector individually, so they will be more reliable to monitor collector's health.

