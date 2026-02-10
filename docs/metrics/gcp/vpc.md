# GCP VPC Networking Metrics

| Metric name                                                                     | Metric type | Description                                                                              | Labels                                                                                                |
|---------------------------------------------------------------------------------|-------------|------------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------|
| cloudcost_gcp_vpc_nat_gateway_hourly_rate_usd_per_hour                         | Gauge       | Hourly cost of Cloud NAT Gateway. Cost represented in USD/hour                           | `region`=&lt;GCP region&gt;                                                                           |
| cloudcost_gcp_vpc_nat_gateway_data_processing_usd_per_gb                        | Gauge       | Data processing cost for Cloud NAT Gateway. Cost represented in USD/GB                   | `region`=&lt;GCP region&gt;                                                                           |
| cloudcost_gcp_vpc_vpn_gateway_hourly_rate_usd_per_hour                          | Gauge       | Hourly cost of VPN Gateway. Cost represented in USD/hour                                 | `region`=&lt;GCP region&gt;                                                                           |
| cloudcost_gcp_vpc_private_service_connect_endpoint_hourly_rate_usd_per_hour     | Gauge       | Hourly cost of Private Service Connect endpoints. Cost represented in USD/hour           | `region`=&lt;GCP region&gt; <br/> `endpoint_type`=&lt;consumer\|partner&gt;                          |
| cloudcost_gcp_vpc_private_service_connect_data_processing_usd_per_gb            | Gauge       | Data processing cost for Private Service Connect. Cost represented in USD/GB             | `region`=&lt;GCP region&gt;                                                                           |

## Overview

The VPC collector exports pricing metrics for Google Cloud Platform VPC networking services, including Cloud NAT, VPN Gateway, and Private Service Connect.

## Supported Services

### Cloud NAT Gateway

Network Address Translation gateway for private instances to access the internet without public IP addresses.

- **Equivalent to**: AWS NAT Gateway
- **Pricing**: Global pricing applies to all regions
  - Gateway: $0.045 per hour
  - Data Processing: $0.045 per GB

### VPN Gateway

Site-to-site VPN connections for secure connectivity between your on-premises network and GCP.

- **Equivalent to**: AWS VPN Gateway
- **Pricing**: Regional pricing varies ($0.05-$0.08 per hour)

### Private Service Connect (PSC)

Private endpoints for accessing Google Cloud services and third-party services without using public IP addresses.

- **Equivalent to**: AWS VPC Endpoints
- **Endpoint Types**:
  - `consumer`: For consuming services via PSC
  - `partner`: For third-party service integrations
- **Pricing**: Global pricing applies to all regions
  - Endpoint: $0.01 per hour per endpoint type
  - Data Processing: $0.01 per GB

## Configuration

Enable the VPC collector by adding `VPC` to your GCP services configuration:

```yaml
gcp:
  services: ["GCS", "GKE", "CLB", "VPC"]
  projects: ["project1", "project2"]
```

Or via command line:
```bash
--gcp.services=GCS,GKE,CLB,VPC --gcp.projects=project1,project2
```

## Labels

- **region**: The GCP region where the service is deployed
- **endpoint_type**: (PSC only) The type of Private Service Connect endpoint (`consumer` or `partner`)

## Notes

- Pricing data is fetched from the GCP Cloud Billing API
- Metrics are automatically refreshed every 24 hours
- Global pricing applies to Cloud NAT and Private Service Connect
- VPN Gateway pricing varies by region
- All costs are represented in USD
