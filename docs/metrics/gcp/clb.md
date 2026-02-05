# GCP Cloud Load Balancer Metrics

| Metric name                                                               | Metric type | Description                                                                                     | Labels                                                                                                                                                                                                                     |
|---------------------------------------------------------------------------|-------------|-------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_gcp_clb_forwarding_rule_unit_per_hour                           | Gauge       | The unit cost of a forwarding rule per hour. Cost represented in USD/hour                       | `name`=&lt;forwarding rule name&gt; <br/> `region`=&lt;GCP region&gt; <br/> `project`=&lt;GCP project&gt; <br/> `ip_address`=&lt;IP address&gt; <br/> `load_balancing_scheme`=&lt;load balancing scheme&gt;            |
| cloudcost_gcp_clb_forwarding_rule_inbound_data_processed_per_gib          | Gauge       | The inbound data processed unit cost of a forwarding rule. Cost represented in USD/GiB          | `name`=&lt;forwarding rule name&gt; <br/> `region`=&lt;GCP region&gt; <br/> `project`=&lt;GCP project&gt; <br/> `ip_address`=&lt;IP address&gt; <br/> `load_balancing_scheme`=&lt;load balancing scheme&gt;            |
| cloudcost_gcp_clb_forwarding_rule_outbound_data_processed_per_gib         | Gauge       | The outbound data processed unit cost of a forwarding rule. Cost represented in USD/GiB         | `name`=&lt;forwarding rule name&gt; <br/> `region`=&lt;GCP region&gt; <br/> `project`=&lt;GCP project&gt; <br/> `ip_address`=&lt;IP address&gt; <br/> `load_balancing_scheme`=&lt;load balancing scheme&gt;            |

## Overview

The Cloud Load Balancer (CLB) collector exports pricing metrics for GCP forwarding rules, which are used to configure load balancing across multiple load balancing schemes (Internal, External, etc.).

## Forwarding Rules

Forwarding rules define how traffic is routed to backend services in Google Cloud. The collector tracks costs for:

- **Hourly rate**: The base cost per hour for each forwarding rule
- **Inbound data processing**: Cost per GiB of inbound traffic processed
- **Outbound data processing**: Cost per GiB of outbound traffic processed

## Configuration

Enable the CLB collector by adding `CLB` to your GCP services configuration:

```yaml
gcp:
  services: ["GCS", "GKE", "CLB"]
  projects: ["project1", "project2"]
```

Or via command line:
```bash
--gcp.services=GCS,GKE,CLB --gcp.projects=project1,project2
```

## Labels

- **name**: The name of the forwarding rule
- **region**: The GCP region where the forwarding rule is deployed
- **project**: The GCP project ID
- **ip_address**: The IP address associated with the forwarding rule
- **load_balancing_scheme**: The load balancing scheme (e.g., INTERNAL, EXTERNAL, INTERNAL_MANAGED)

## Load Balancing Schemes

GCP supports various load balancing schemes:
- **EXTERNAL**: Classic external load balancing
- **INTERNAL**: Internal TCP/UDP load balancing
- **INTERNAL_MANAGED**: Internal HTTP(S) load balancing
- **INTERNAL_SELF_MANAGED**: Traffic Director

Each scheme may have different pricing characteristics.

## Notes

- Pricing data is fetched from the GCP Cloud Billing API
- Metrics are automatically refreshed every 24 hours
- Data processing costs are separate from the hourly forwarding rule costs
- All costs are represented in USD
