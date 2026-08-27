# GCE Compute Metrics

| Metric name                                          | Metric type | Description                                                                                                                | Labels                                                                                                                                                                                                                                                                                                        |
|-------------------------------------------------------|-------------|-----------------------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_exporter_gcp_gce_populate_errors_total       | Counter     | Total errors during background store population. | `store`=&lt;nodes\|machine_types&gt; <br/> `project`=&lt;GCP project&gt; <br/> `operation`=&lt;get_zones\|list_instances\|get_machine_type&gt; |
| cloudcost_gcp_gce_instance_cpu_usd_per_core_hour       | Gauge       | The processing cost of a standalone GCE Instance in USD/(core*h)                                                            | `instance`=&lt;name of the compute instance&gt; <br/> `region`=&lt;GCP region code&gt; <br/> `family`=&lt;broader compute family (n1, n2, c3 ...)&gt; <br/> `machine_type`=&lt;specific machine type, e.g.: n2-standard-2&gt; <br/> `project`=&lt;GCP project, where the instance is provisioned&gt; <br/> `price_tier`=&lt;spot\|ondemand&gt; |
| cloudcost_gcp_gce_instance_memory_usd_per_gib_hour     | Gauge       | The memory cost of a standalone GCE Instance in USD/(GiB*h)                                                                 | Same labels as above.                                                                                                                                                                                                                                                                                          |
| cloudcost_gcp_gce_instance_total_usd_per_hour          | Gauge       | The total hourly cost of a standalone GCE Instance in USD/h. Absent for an instance whose machine type spec hasn't resolved yet; the cpu/memory metrics are unaffected. | Same labels as above.                                                                                                                                                                                                                                                                                          |

## What counts as a GCE instance here

This collector only emits metrics for Compute Engine instances that are **not** part of a GKE cluster (no `goog-k8s-cluster-name` label). GKE-managed nodes are priced exclusively by the `gcp_gke_*` metrics documented in [gke.md](./gke.md). This split means a node's cost is never counted twice: running both `GKE` and `GCE` for the same project is safe, each instance shows up in exactly one collector's output.

## Collection model

Instance inventory and pricing are collected by background goroutines and cached in memory, the same architecture as the GKE collector. Each scrape reads from those caches, so scrape duration doesn't depend on GCP API latency.

- **Node inventory** refreshes every 5 minutes (`Compute.Zones.List` per project, `Instances.List` per zone). This is a separate, independent store from any concurrently running `GKE` collector's own node store, so enabling both `GKE` and `GCE` for the same project duplicates these API calls. That's a known, accepted tradeoff, not a bug: it mirrors the existing duplication between GKE's own node and disk stores.
- **Machine type specs** (vCPU count, memory) needed for the total cost metric are resolved per `(project, zone, machine_type)` via `Compute.MachineTypes.Get`, cached indefinitely since a machine type's spec never changes for a given zone. The cache warms on the same 5-minute cadence as node inventory. A lookup failure (transient API error, or a key not yet warmed) only suppresses `instance_total_usd_per_hour` for that instance; the cpu/memory rate metrics don't depend on it and are unaffected.
- **Pricing** refreshes on its own interval via the Cloud Billing Catalog API, same source as GKE's.

Per-zone API calls within a project are issued in parallel, capped at 10 concurrent in-flight calls by default. Override with the `--gcp.gce.zone-concurrency` flag; the value applies to both the node store and the machine-type cache warm.

### Staleness and partial-failure behaviour

- Instance metrics may be up to 5 minutes stale.
- The first scrape after startup emits no metrics until the node store completes its initial populate. The collector logs `node store not yet populated, skipping instance metrics` and continues.
- If `GetZones` fails for a project, that project's existing node cache is preserved.
- If a zone-level call fails (partial or total), the cache entry for that zone is left untouched; a subsequent successful populate refreshes it. Failures are logged and counted in `cloudcost_exporter_gcp_gce_populate_errors_total`.
- An instance on a machine type with no matching SKU in the pricing map (an unpriced or unrecognized machine type) is skipped entirely, no metrics are emitted for it, and the error is logged.
