# AWS ELB Metrics

The AWS ELB (Elastic Load Balancer) module exports cost metrics for Application Load Balancers (ALB) and Network Load Balancers (NLB).

## Configuration

To enable ELB metrics collection, add `"ELB"` to the AWS services configuration:

```yaml
providers:
  aws:
    services:
      - "ELB"
    # other configuration...
```

## Metrics

| Metric Name | Type | Description | Labels |
|-------------|------|-------------|--------|
| `cloudcost_aws_elb_loadbalancer_total_usd_per_hour` | Gauge | Total hourly cost of the load balancer in USD | `name`, `arn`, `region`, `type` |

### Labels

- **name**: Load balancer name
- **arn**: Full ARN of the load balancer
- **region**: AWS region (e.g., `us-east-1`)
- **type**: Load balancer type (`application` or `network`)

## Example Queries

### Total ELB costs by region
```promql
sum by (region) (cloudcost_aws_elb_loadbalancer_total_usd_per_hour)
```

### Most expensive load balancers
```promql
topk(10, cloudcost_aws_elb_loadbalancer_total_usd_per_hour)
```

### ALB vs NLB cost comparison
```promql
sum by (type) (cloudcost_aws_elb_loadbalancer_total_usd_per_hour)
```

### Monthly cost estimation
```promql
cloudcost_aws_elb_loadbalancer_total_usd_per_hour * 24 * 30
```

## Pricing Notes

- Pricing data is fetched from the AWS Pricing API for each region
- Pricing is refreshed based on the configured scrape interval
- Default fallback rates are used if pricing data is unavailable:
  - ALB: $0.0225/hour
  - NLB: $0.0225/hour
- Classic Load Balancers are not supported (use ELB v1 API)

## IAM Permissions

Required permissions for ELB metrics collection:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "elasticloadbalancing:DescribeLoadBalancers",
                "elasticloadbalancing:DescribeTargetGroups"
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