# Azure Blob Storage metrics

## Status

The `BLOB` service is registered when you pass `BLOB` in `--azure.services`. The collector **does not query Azure Cost Management yet**. One storage cost `GaugeVec` is **registered** and appears in `Describe`, but **there are no samples** because **`Collect` does not call `Set`** until that integration exists.

## Planned behavior (see issue #54):

### Metric shape (AWS S3 and GCP GCS in this repo)

- **Storage rate:** Same naming pattern as S3 and GCS: `storage_by_location_usd_per_gibyte_hour` under the service subsystem (here `azure_blob` → `cloudcost_azure_blob_storage_by_location_usd_per_gibyte_hour`). Implemented today as `cloudcost_aws_s3_*` in `pkg/aws/s3/s3.go` and `cloudcost_gcp_gcs_*` in `pkg/google/metrics/metrics.go`.
- **Operations rate (optional):** S3 and GCS also define `operation_by_location_usd_per_krequest` (`pkg/aws/s3/s3.go`, `pkg/google/metrics/metrics.go`). **Not registered in the exporter yet** (see Planned future work under [Cost metrics](#cost-metrics)). A Blob collector may expose an analogous gauge when the chosen Azure dataset supports it.
- **Labels:** S3 uses `region` and `class` for storage (and `tier` for operations); GCS uses `location` and `storage_class` (and operation class labels for ops). Azure label sets would follow whatever dimensions the Cost Management query returns; the above collectors show the cross-cloud “by location + storage class/tier” pattern only.

### Azure Cost Management

- **Query API:** Azure documentation mentions `POST https://management.azure.com/{scope}/providers/Microsoft.CostManagement/query` and states that `{scope}` includes **`/subscriptions/{subscriptionId}/`** for subscription scope. Sample responses in that snapshot include cost columns such as **`PreTaxCost`** alongside dimensions like **`ResourceLocation`** in example filters.
- **Permissions:** Azure documentation states that you must have **Cost Management permissions at the appropriate scope** to use Cost Management APIs (the captured article links to Microsoft’s assign-access documentation for details). No specific Azure role name is asserted here.

### Provider operational metrics

- **`cloudcost_exporter_collector_*` with `collector="azure_blob"`** — same mechanism as other Azure collectors (e.g. `azure_aks`) via `pkg/azure/azure.go` and the shared gatherer pattern.

## Cost metrics

| Metric name | Type | Labels | Samples |
|-------------|------|--------|---------|
| `cloudcost_azure_blob_storage_by_location_usd_per_gibyte_hour` | Gauge | `region`, `class` | None; exporter does not populate this metric yet (no billing API calls in `Collect`) |

**Planned future work:** `cloudcost_azure_blob_operation_by_location_usd_per_krequest` (Gauge; `region`, `class`, `tier`) — parity with S3/GCS operation metrics; commented out in `pkg/azure/blob/blob.go` until operation pricing is implemented. How we group cost rows into labels is TBD when we add the API integration.
