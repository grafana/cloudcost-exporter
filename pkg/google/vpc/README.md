# GCP VPC Networking Collector

This collector provides pricing metrics for Google Cloud Platform (GCP) VPC networking services, similar to the AWS VPC collector. It fetches real-time pricing data from the GCP Cloud Billing API and exports Prometheus metrics for cost monitoring and optimization.

## Supported Services

The GCP VPC collector covers the following networking services:

### 1. Cloud NAT Gateway
- **Service**: Managed NAT service for outbound internet connectivity
- **Metrics**:
  - `cloudcost_gcp_vpc_nat_gateway_hourly_rate_usd_per_hour` - Hourly rate for the NAT gateway
  - `cloudcost_gcp_vpc_nat_gateway_data_processing_usd_per_gb` - Data processing cost per GB
- **Equivalent to**: AWS NAT Gateway

### 2. VPN Gateway
- **Service**: Site-to-site VPN connections
- **Metric**: `cloudcost_gcp_vpc_vpn_gateway_hourly_rate_usd_per_hour`
- **Equivalent to**: AWS VPN Gateway

### 3. Private Service Connect
- **Service**: Private connectivity to Google services and third-party services
- **Metric**: `cloudcost_gcp_vpc_private_service_connect_hourly_rate_usd_per_hour`
- **Equivalent to**: AWS VPC Endpoints

### 4. External IP Addresses
- **Static IPs**: Reserved external IP addresses
  - **Metric**: `cloudcost_gcp_vpc_external_ip_static_hourly_rate_usd_per_hour`
- **Ephemeral IPs**: Temporary external IP addresses
  - **Metric**: `cloudcost_gcp_vpc_external_ip_ephemeral_hourly_rate_usd_per_hour`
- **Equivalent to**: AWS Elastic IP addresses

### 5. Cloud Router
- **Service**: BGP routing for hybrid connectivity
- **Metric**: `cloudcost_gcp_vpc_cloud_router_hourly_rate_usd_per_hour`
- **Equivalent to**: AWS Transit Gateway (partial functionality)

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
- **Services Queried**: "Compute Engine" and "Networking"
- **Error Handling**: Returns errors for missing pricing data (no default values)

## Architecture

The collector follows the same pattern as other GCP collectors:

1. **VPCPricingMap**: Manages pricing data across regions and services
2. **Rate-Only Collection**: Provides unit rates rather than discovering resources
3. **Context Awareness**: Supports graceful shutdown and cancellation
4. **Error Handling**: Logs warnings for missing pricing data and skips metric export

## Example Grafana Queries

### Cost Comparison Across Regions
```promql
cloudcost_gcp_vpc_nat_gateway_hourly_rate_usd_per_hour
```

### Most Expensive VPC Services
```promql
# Compare different VPC service costs
{__name__=~"cloudcost_gcp_vpc_.*_hourly_rate_usd_per_hour"}
```

### Project-Specific VPC Costs
```promql
sum by (project) (
  {__name__=~"cloudcost_gcp_vpc_.*_hourly_rate_usd_per_hour"}
)
```

### Regional Cost Analysis
```promql
sum by (region) (
  {__name__=~"cloudcost_gcp_vpc_.*_hourly_rate_usd_per_hour"}
)
```

## Required Permissions

The GCP service account needs the following IAM roles:
- `roles/billing.viewer` - To access Cloud Billing API
- `roles/compute.viewer` - To access Compute Engine API for region information

## Implementation Notes

- **Multi-Project Support**: Collects metrics for all configured projects
- **Regional Coverage**: Supports all GCP regions
- **Concurrent Processing**: Handles multiple projects and regions efficiently
- **Pricing Accuracy**: Uses real-time GCP pricing data with no hardcoded defaults
- **Zero Price Handling**: Correctly handles legitimate zero prices (free tier, promotions)

## Troubleshooting

### No Metrics Appearing
1. Check if VPC is enabled in your configuration
2. Verify GCP service account permissions
3. Check logs for pricing API errors
4. Ensure projects are correctly configured

### Missing Regional Data
- Some regions may not have pricing data for all services
- Check logs for specific region/service warnings
- Verify region names match GCP region identifiers

### Authentication Issues
- Ensure `GOOGLE_APPLICATION_CREDENTIALS` environment variable is set
- Verify service account has required billing permissions
- Check project access permissions

