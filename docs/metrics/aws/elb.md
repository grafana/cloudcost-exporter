# AWS ELB Metrics

The AWS ELB (Elastic Load Balancer) module exports cost metrics for Application Load Balancers (ALB) and Network Load Balancers (NLB).

## Configuration

To enable ELB metrics collection, add `"elb"` to the AWS services configuration:

```yaml
providers:
  aws:
    services:
      - "elb"
    # other configuration...
```

## Metrics

| Metric Name | Type | Description | Labels |
|-------------|------|-------------|--------|
| `cloudcost_aws_elb_loadbalancer_usage_total_usd_per_hour` | Gauge | The total cost of hourly usage of the load balancer in USD/h | `name`, `region`, `type` |
| `cloudcost_aws_elb_loadbalancer_capacity_units_total_usd_per_hour` | Gauge | The total cost of Load Balancer Capacity units (LCU) used in USD/hour | `name`, `region`, `type` |

### Labels

- **name**: Load balancer name
- **region**: AWS region (e.g., `us-east-1`)
- **type**: Load balancer type (`application` or `network`)

## Pricing Notes

- Pricing data is fetched from the AWS Pricing API for each region
- Pricing is refreshed based on the configured scrape interval
- Default fallback rates are used if pricing data is unavailable:
  - ALB: $0.0225/hour
  - NLB: $0.0225/hour
- Classic Load Balancers are not supported (they use ELB v1 API)

## IAM Permissions

Required permissions for ELB metrics collection:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "elasticloadbalancing:DescribeLoadBalancers"
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