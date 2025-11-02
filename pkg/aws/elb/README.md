# AWS ELB (Elastic Load Balancer) Cost Exporter

This module provides cost reporting capabilities for AWS Elastic Load Balancers (ELBv2) including Application Load Balancers (ALB) and Network Load Balancers (NLB).

## Overview

The ELB collector gathers pricing information from the AWS Pricing API and combines it with load balancer configuration data from the ELBv2 API to provide hourly cost metrics for each load balancer.

## Supported Load Balancer Types

- **Application Load Balancer (ALB)**: Layer 7 load balancer for HTTP/HTTPS traffic
- **Network Load Balancer (NLB)**: Layer 4 load balancer for TCP/UDP traffic

Note: Classic Load Balancers (CLB) are not supported as they use the ELB v1 API.

## Metrics

| Metric Name | Type | Description | Labels |
|-------------|------|-------------|--------|
| `cloudcost_aws_elb_loadbalancer_total_usd_per_hour` | Gauge | Total hourly cost of the load balancer in USD | `name`, `arn`, `region`, `type` |

### Metric Labels

- `name`: The name of the load balancer
- `arn`: The full ARN of the load balancer
- `region`: The AWS region where the load balancer is deployed
- `type`: The type of load balancer (`application` or `network`)

## Configuration

To enable ELB cost collection, add `"ELB"` to the services list in your AWS provider configuration:

```yaml
providers:
  aws:
    services:
      - "ELB"
    # other AWS configuration...
```

## Pricing Data

The collector fetches pricing data from the AWS Pricing API for each region. Pricing is refreshed periodically based on the configured scrape interval. If pricing data is unavailable, the collector falls back to default rates:

- ALB: $0.0225 per hour
- NLB: $0.0225 per hour

## Permissions

The following AWS IAM permissions are required:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "elasticloadbalancing:DescribeLoadBalancers",
            ],
            "Resource": "*"
        },
        {
            "Effect": "Allow",
            "Action": [
                "pricing:GetProducts"
            ],
            "Resource": "*"
        }
    ]
}
```

## Implementation Details

- **Concurrent Collection**: The collector uses goroutines to fetch data from multiple regions concurrently
- **Pricing Cache**: Pricing data is cached and refreshed based on the scrape interval to minimize API calls
- **Error Handling**: Failed requests are logged but don't prevent collection from other regions
- **Thread Safety**: All shared state is protected with appropriate synchronization primitives