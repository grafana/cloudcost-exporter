# Azure Blob Storage metrics

Pass `blob` in `--azure.services` to enable this collector. Matching is case-insensitive.

The collector defines a storage cost `GaugeVec` that the Azure provider includes in its `Describe` and `Collect` fan-out (same gatherer pattern as `azure_aks`). `Collect` calls `StorageCostQuerier.QueryBlobStorage` with a **30-day** lookback (`defaultQueryLookback` in `pkg/azure/blob/cost_query.go`) and sets the gauge from each row. `Config.CostQuerier` supplies the querier; when it is nil the collector uses a no-op querier (no rows). The parent Azure collector forwards `StorageGauge.Collect(ch)` so blob cost metrics share one registration path with the rest of the Azure exporter. Scrape instrumentation publishes `cloudcost_exporter_collector_*` with label `collector="azure_blob"`.

## Cost metrics

| Metric name | Metric type | Description | Labels |
|-------------|-------------|-------------|--------|
| cloudcost_azure_blob_storage_by_location_usd_per_gibyte_hour | Gauge | Storage cost rate for Blob Storage by region and class. Cost represented in USD/(GiB*h) | `region`=&lt;Azure region&gt;<br/> `class`=&lt;Blob access tier or storage class&gt; |
