# AWS RDS Metrics

| Metric name                                    | Metric type | Description                                                                          | Labels                                                                                                                                              |
|------------------------------------------------|-------------|--------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_aws_rds_hourly_rate_usd_per_hour     | Gauge       | Hourly cost of AWS RDS instances by region, tier, and instance ID. Cost represented in USD/hour | `account_id`=&lt;AWS account ID&gt; <br/> `region`=&lt;AWS region&gt; <br/> `tier`=&lt;RDS instance tier&gt; <br/> `id`=&lt;RDS instance ID&gt; <br/> `arn_name`=&lt;RDS instance ARN name&gt; |

### Operational Metrics

| Metric name                                      | Metric type | Description                                                              | Labels                                                                                                                              |
|--------------------------------------------------|-------------|-------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_exporter_aws_rds_populate_errors_total | Counter     | Errors during background store population by store, region, and operation | `store`=&lt;always `instances`&gt; <br/> `region`=&lt;AWS region&gt; <br/> `operation`=&lt;`lookup_client`, `list_instances`, `list_prices`, `parse_pricing`&gt; |

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

### Per-region Timeout

The background refresh lists instances (`DescribeDBInstances`) and prices (`GetProducts`) in each region concurrently. `--aws.rds.region-timeout` bounds each region's work:

```bash
--aws.rds.region-timeout=15s
```

The default is `0`, which applies an internal safety ceiling so a hung AWS call can never stall the background refresh. Set a positive value (e.g. `15s`) to fail slow or unreachable regions faster: the region is logged, counted in `cloudcost_exporter_aws_rds_populate_errors_total`, and skipped while the others still warm. Because the refresh runs off the scrape path, a slow region delays only the freshness of that region's data, not the scrape itself.

## Labels

- **account_id**: The AWS account ID (12-digit), resolved via STS GetCallerIdentity
- **region**: The AWS region where the RDS instance is running
- **tier**: The pricing tier of the RDS instance (e.g., on-demand, reserved)
- **id**: The unique identifier of the RDS instance
- **arn_name**: The Amazon Resource Name (ARN) of the RDS instance

## Notes

- Pricing is listed in bulk per region from the AWS Pricing API and keyed by instance type, engine, deployment option, and license so `Collect` matches each instance to its price
- Instances whose engine is not recognized are skipped; recognized engines are MySQL, MariaDB, PostgreSQL, Aurora MySQL, Aurora PostgreSQL, Oracle, and SQL Server
- Inventory and pricing are refreshed in the background on the configured scrape interval; scrapes read the warm cache and make no AWS calls
- No metrics emit until the first background refresh completes after startup
- All costs are represented in USD per hour

## IAM Permissions

Required permissions for RDS metrics collection:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "rds:DescribeDBInstances"
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
