# GCP VPC Networking Collector

This collector provides pricing metrics for Google Cloud Platform (GCP) VPC networking services. It fetches real-time pricing data from the GCP Cloud Billing API and exports Prometheus metrics for cost monitoring and optimization.

## Supported Services

### Cloud NAT Gateway
- **Service**: Network Address Translation gateway for private instances
- **Metrics**:
  - `cloudcost_gcp_vpc_nat_gateway_hourly_rate_usd_per_hour`
  - `cloudcost_gcp_vpc_nat_gateway_data_processing_usd_per_gb`
- **Equivalent to**: AWS NAT Gateway
- **Pricing**: Global pricing applies to all regions
  - Gateway: $0.045 per hour
  - Data Processing: $0.045 per GB

### VPN Gateway
- **Service**: Site-to-site VPN connections
- **Metric**: `cloudcost_gcp_vpc_vpn_gateway_hourly_rate_usd_per_hour`
- **Equivalent to**: AWS VPN Gateway
- **Pricing**: Regional pricing varies ($0.05-$0.08 per hour)

### Private Service Connect (PSC)
- **Service**: Private endpoints for accessing Google Cloud services and third-party services
- **Metrics**:
  - `cloudcost_gcp_vpc_private_service_connect_endpoint_hourly_rate_usd_per_hour{endpoint_type}`
  - `cloudcost_gcp_vpc_private_service_connect_data_processing_usd_per_gb`
- **Equivalent to**: AWS VPC Endpoints
- **Endpoint Types**:
  - `consumer`: For consuming services via PSC
  - `partner`: For third-party service integrations
- **Pricing**: Global pricing applies to all regions
  - Endpoint: $0.01 per hour per endpoint type
  - Data Processing: $0.01 per GB

## Configuration

To enable the GCP VPC collector, add `VPC` to your GCP services configuration:

```yaml
gcp:
  services: ["GCS", "GKE", "CLB", "VPC"]  # Add VPC here
  projects: "project1,project2,project3"
```

Or via command line:
```bash
--gcp.services=GCS,GKE,CLB,VPC
```

## Metrics Labels

All metrics include the following labels:
- `region`: GCP region (e.g., `us-central1`, `europe-west1`)
- `project`: GCP project ID

## Pricing Data Source

- **API**: GCP Cloud Billing API
- **Refresh Interval**: 24 hours (configurable via `PriceRefreshInterval`)
- **Services Queried**: "Networking"
- **Error Handling**: Logs warnings for missing pricing data

## Global vs Regional Pricing

GCP uses two types of pricing for VPC services:

### Global Pricing
- **Cloud NAT Gateway**: Same price applies to all regions worldwide
- Pricing data has empty `Regions` array in the GCP Billing API
- The collector applies global rates to all regions automatically

### Regional Pricing
- **VPN Gateway**: Different prices per region
- Pricing data includes specific region in the GCP Billing API
- The collector uses region-specific rates when available

## Example Grafana Queries

### Cloud NAT Gateway Hourly Rate
```promql
cloudcost_gcp_vpc_nat_gateway_hourly_rate_usd_per_hour
```

### Cloud NAT Data Processing Rate
```promql
cloudcost_gcp_vpc_nat_gateway_data_processing_usd_per_gb
```

### VPN Gateway Cost by Region
```promql
cloudcost_gcp_vpc_vpn_gateway_hourly_rate_usd_per_hour
```

### Most Expensive VPN Regions
```promql
topk(10, cloudcost_gcp_vpc_vpn_gateway_hourly_rate_usd_per_hour)
```

### Private Service Connect Endpoint Costs by Type
```promql
cloudcost_gcp_vpc_private_service_connect_endpoint_hourly_rate_usd_per_hour
```

### Private Service Connect Data Processing Rate
```promql
cloudcost_gcp_vpc_private_service_connect_data_processing_usd_per_gb
```

### All VPC Costs for a Specific Project
```promql
{__name__=~"cloudcost_gcp_vpc_.*",project="my-project"}
```

### Total VPC Networking Costs
```promql
sum(cloudcost_gcp_vpc_nat_gateway_hourly_rate_usd_per_hour) +
sum(cloudcost_gcp_vpc_vpn_gateway_hourly_rate_usd_per_hour) +
sum(cloudcost_gcp_vpc_private_service_connect_endpoint_hourly_rate_usd_per_hour)
```

## Required Permissions

The GCP service account needs the following IAM roles:
- `roles/billing.viewer` - To access Cloud Billing API
- `roles/compute.viewer` - To access Compute Engine API for region information

## Limitations

The following VPC services do **not** have pricing exposed through the GCP Billing API:
- External IP Addresses (Static/Ephemeral with non-zero cost)
- Cloud Router (free service, charges only apply to NAT/VPN traffic)

These services would require manual configuration or alternative pricing sources. For comprehensive cost data, consider using **GCP Cloud Billing Export to BigQuery**.

**Note**: External IP addresses may have zero cost in certain cases (e.g., attached to running instances), which is different from being unavailable in the API.

## Troubleshooting

### No Metrics Appearing
1. Check if VPC is enabled in your configuration
2. Verify GCP service account permissions
3. Check logs for pricing API errors
4. Ensure projects are correctly configured

### Authentication Issues
- Ensure `GOOGLE_APPLICATION_CREDENTIALS` environment variable is set
- Verify service account has required billing permissions
- Check project access permissions

