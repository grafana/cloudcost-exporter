# AWS RDS Metrics

| Metric name                                    | Metric type | Description                                                                          | Labels                                                                                                                                              |
|------------------------------------------------|-------------|--------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_aws_rds_hourly_rate_usd_per_hour     | Gauge       | Hourly cost of AWS RDS instances by region, tier, and instance ID. Cost represented in USD/hour | `region`=&lt;AWS region&gt; <br/> `tier`=&lt;RDS instance tier&gt; <br/> `id`=&lt;RDS instance ID&gt; <br/> `arn_name`=&lt;RDS instance ARN name&gt; |

## Overview

The RDS collector exports pricing metrics for Amazon Relational Database Service instances across all configured AWS regions. It collects hourly pricing rates for RDS database instances based on their instance type and tier.

## Configuration

Enable the RDS collector by adding `rds` to your AWS services configuration:

```yaml
aws:
  services: ["ec2", "s3", "rds"]
  regions: ["us-east-1", "us-west-2"]
```

Or via command line:
```bash
--aws.services=ec2,s3,rds
```

## Labels

- **region**: The AWS region where the RDS instance is running
- **tier**: The pricing tier of the RDS instance (e.g., on-demand, reserved)
- **id**: The unique identifier of the RDS instance
- **arn_name**: The Amazon Resource Name (ARN) of the RDS instance

## Notes

- Pricing data is fetched from the AWS Pricing API (us-east-1 region)
- Metrics are refreshed periodically based on the configured scrape interval
- All costs are represented in USD per hour
