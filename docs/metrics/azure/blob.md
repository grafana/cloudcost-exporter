# Azure Blob Storage metrics

Pass `blob` in `--azure.services` to enable this collector. Matching is case-insensitive.

The collector defines a storage cost `GaugeVec` that the Azure provider includes in its `Describe` and `Collect` fan-out (same gatherer pattern as `azure_aks`). The parent Azure collector forwards `StorageGauge.Collect(ch)` so blob cost metrics share one registration path with the rest of the Azure exporter. Scrape instrumentation publishes `cloudcost_exporter_collector_*` with label `collector="azure_blob"`.

## Cost metrics

| Metric name | Metric type | Description | Labels |
|-------------|-------------|-------------|--------|
| cloudcost_azure_blob_storage_by_location_usd_per_gibyte_hour | Gauge | Storage cost rate for Blob Storage by region and class. Cost represented in USD/(GiB*h) | |
