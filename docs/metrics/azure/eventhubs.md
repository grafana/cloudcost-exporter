# Azure Event Hubs Metrics

| Metric name                                                | Metric type | Description                                                                 | Labels                                                                                                                                      |
|------------------------------------------------------------|-------------|-----------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_azure_eventhubs_compute_hourly_rate_usd_per_hour | Gauge       | Hourly compute cost of an Azure Event Hubs namespace. Cost represented in USD/hour | `region`=&lt;Azure region where the namespace runs&gt; <br/> `namespace_name`=&lt;Event Hubs namespace name&gt; <br/> `namespace`=&lt;Event Hubs namespace ARM resource ID&gt; <br/> `sku`=&lt;Event Hubs SKU&gt; |
| cloudcost_azure_eventhubs_storage_hourly_rate_usd_per_hour | Gauge       | Hourly storage cost of an Azure Event Hubs namespace. Cost represented in USD/hour | `region`=&lt;Azure region where the namespace runs&gt; <br/> `namespace_name`=&lt;Event Hubs namespace name&gt; <br/> `namespace`=&lt;Event Hubs namespace ARM resource ID&gt; <br/> `sku`=&lt;Event Hubs SKU&gt; |

## Overview

The Event Hubs collector exports hourly cost metrics for Kafka-compatible Azure Event Hubs namespaces.

V1 supports Standard namespaces with fixed throughput capacity only. It skips Premium, Dedicated, and auto-inflate namespaces.

## Configuration

Enable the Event Hubs collector by adding `EVENTHUBS` to your Azure services configuration:

```yaml
azure:
  services: ["AKS", "EVENTHUBS"]
```

Or via command line:

```bash
--azure.services=AKS,EVENTHUBS
```

## Notes

- Compute cost includes Standard throughput units, the Standard Kafka endpoint charge, and estimated ingress charges
- Ingress is estimated from Azure Monitor `IncomingMessages` and `IncomingBytes` metrics using `max(messages, ceil(bytes / 64 KB))` per minute
- Storage cost is estimated from the `Size` metric and only covers bytes above the included `84 GB` per throughput unit allowance
- Storage overage uses the regional Azure Blob Storage Hot LRS data-stored price
- The collector assumes Standard namespaces are used for Kafka unless Azure explicitly reports `kafkaEnabled=false`
- The collector leaves a TODO in code to split ingress into its own metric in a future change

## IAM Permissions

Required permissions for Event Hubs metrics collection:

- `Microsoft.EventHub/namespaces/read`
- `Microsoft.Insights/metrics/read`

The Azure Retail Prices API calls used for list-price lookup do not require additional IAM permissions.
