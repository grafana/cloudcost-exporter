# GCP VPC Networking Collector

This collector provides pricing metrics for Google Cloud Platform (GCP) VPC networking services. It fetches real-time pricing data from the GCP Cloud Billing API and exports Prometheus metrics for cost monitoring and optimization.

## Supported Services

### VPN Gateway
- **Service**: Site-to-site VPN connections
- **Metric**: `cloudcost_gcp_vpc_vpn_gateway_hourly_rate_usd_per_hour`
- **Equivalent to**: AWS VPN Gateway
- **Pricing**: $0.05-$0.08 per hour depending on region

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

## Example Grafana Queries

### VPN Gateway Cost by Region
```promql
cloudcost_gcp_vpc_vpn_gateway_hourly_rate_usd_per_hour
```

### VPN Gateway Cost for Specific Project
```promql
cloudcost_gcp_vpc_vpn_gateway_hourly_rate_usd_per_hour{project="my-project"}
```

### Most Expensive VPN Regions
```promql
topk(10, cloudcost_gcp_vpc_vpn_gateway_hourly_rate_usd_per_hour)
```

## Required Permissions

The GCP service account needs the following IAM roles:
- `roles/billing.viewer` - To access Cloud Billing API
- `roles/compute.viewer` - To access Compute Engine API for region information

## Limitations

The following VPC services do **not** have pricing exposed through the GCP Billing API:
- Cloud NAT Gateway
- Private Service Connect
- External IP Addresses (Static/Ephemeral)
- Cloud Router

These services would require manual configuration or alternative pricing sources.

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

