# GCP Managed Kafka Metrics

| Metric name                                                   | Metric type | Description                                                                          | Labels                                                                                                                                                                                                 |
|---------------------------------------------------------------|-------------|--------------------------------------------------------------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_gcp_managedkafka_compute_hourly_rate_usd_per_hour   | Gauge       | Hourly compute cost of a GCP Managed Kafka cluster. Cost represented in USD/hour     | `project`=&lt;GCP project that owns the cluster&gt; <br/> `region`=&lt;GCP region where the cluster runs&gt; <br/> `cluster_name`=&lt;Managed Kafka cluster ID&gt; <br/> `cluster`=&lt;Managed Kafka cluster resource name&gt; |
| cloudcost_gcp_managedkafka_storage_hourly_rate_usd_per_hour   | Gauge       | Hourly local storage cost of a GCP Managed Kafka cluster. Cost represented in USD/hour | `project`=&lt;GCP project that owns the cluster&gt; <br/> `region`=&lt;GCP region where the cluster runs&gt; <br/> `cluster_name`=&lt;Managed Kafka cluster ID&gt; <br/> `cluster`=&lt;Managed Kafka cluster resource name&gt; |

## Overview

The Managed Kafka collector exports list-price hourly cost metrics for Google Cloud Managed Service for Apache Kafka clusters across all configured GCP projects.

V1 covers cluster compute and pre-provisioned local storage. It does not include Kafka Connect, long-term storage, inter-zone transfer, inter-region transfer, or Private Service Connect charges.

## Configuration

Enable the Managed Kafka collector by adding `KAFKA` to your GCP services configuration:

```yaml
gcp:
  services: ["GKE", "GCS", "KAFKA"]
  projects: ["project-a", "project-b"]
```

Or via command line:

```bash
--gcp.services=GKE,GCS,KAFKA --gcp.projects=project-a,project-b
```

## Notes

- Compute cost uses Google Cloud's DCU pricing model: `1 vCPU = 0.6 DCU`, `1 GiB RAM = 0.1 DCU`
- Storage cost uses the service's billed local storage allocation of `100 GiB` per provisioned vCPU
- The collector uses the Cloud Billing Catalog API for pricing and the Managed Kafka API for cluster inventory
- The billing catalog exposes cluster compute as `Data Compute Units` and local storage as `Local Storage`; the collector converts local storage `GiBy.mo` prices to hourly rates before export
- Metrics use list pricing and ignore discounted CUD SKUs

## IAM Permissions

Required permissions for Managed Kafka metrics collection:

- `managedkafka.locations.list`
- `managedkafka.clusters.list`

The Cloud Billing Catalog API calls used for list-price SKU lookup do not require additional IAM permissions.
