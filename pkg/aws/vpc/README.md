# VPC Pricing Module

This module provides rate-only pricing data for AWS VPC services, following the same pattern as other pricing modules in the system.

## Overview

The VPC module collects hourly pricing rates for the following AWS VPC services:
- **VPC Endpoints**: Interface endpoints that connect to AWS services via PrivateLink
- **Transit Gateway**: Network transit hub attachments
- **Elastic IPs**: Both in-use and idle Elastic IP addresses

## Architecture

The VPC module uses a custom `VPCPricingMap` instead of the generic `PricingStore` to handle the complexity of VPC pricing data. This follows the same pattern as the ELB module.

**Important**: Like RDS, the VPC module explicitly uses a dedicated client configured for `us-east-1` region for all pricing API calls, since the AWS Pricing API is only available in that region.

### Components

1. **VPCPricingMap**: Custom pricing map that fetches and categorizes VPC pricing data
2. **Collector**: Prometheus metrics collector that exports pricing data
3. **pricing_map.go**: Core pricing logic and data structures

### Pricing Data Sources

The module uses the AWS Pricing API with the `AmazonVPC` service code to fetch pricing data for:

- VPC Endpoints: Usage types containing "VpcEndpoint"
- Transit Gateway: Usage types containing "TransitGateway"
- Elastic IPs: Usage types containing "PublicIPv4"

## Metrics Exported

The module exports the following Prometheus metrics:

| Metric | Description | Labels |
|--------|-------------|--------|
| `cloudcost_aws_vpc_endpoint_hourly_rate_usd_per_hour` | Hourly cost of VPC endpoints | region |
| `cloudcost_aws_vpc_transit_gateway_hourly_rate_usd_per_hour` | Hourly cost of Transit Gateway | region |
| `cloudcost_aws_vpc_elastic_ip_in_use_hourly_rate_usd_per_hour` | Hourly cost of in-use Elastic IPs | region |
| `cloudcost_aws_vpc_elastic_ip_idle_hourly_rate_usd_per_hour` | Hourly cost of idle Elastic IPs | region |

## Configuration

The VPC collector is configured through the main AWS provider configuration and registers automatically when the `VPC` service is enabled.

## Default Rates

If pricing data cannot be fetched from the AWS Pricing API, the following default rates are used:

- VPC Endpoints: $0.01/hour
- Transit Gateway: $0.05/hour
- Elastic IP (In Use): $0.005/hour
- Elastic IP (Idle): $0.005/hour

## Usage

The collector automatically refreshes pricing data every 24 hours to ensure rates remain current.

## Testing

Run the VPC module tests:

```bash
go test ./pkg/aws/vpc -v
```

The tests include:
- Mock client validation
- Default pricing behavior
- Metric descriptions
- Pricing map functionality
