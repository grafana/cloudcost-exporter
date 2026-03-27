# AWS MSK Metrics

| Metric name                                              | Metric type | Description                                                                          | Labels                                                                                                                                               |
|----------------------------------------------------------|-------------|--------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_aws_msk_compute_hourly_rate_usd_per_hour       | Gauge       | Hourly broker compute cost for an AWS MSK cluster. Cost represented in USD/hour      | `region`=<AWS region> <br/> `cluster_name`=<MSK cluster name> <br/> `cluster_arn`=<MSK cluster ARN> <br/> `instance_type`=<broker instance type> |
| cloudcost_aws_msk_storage_hourly_rate_usd_per_hour       | Gauge       | Hourly provisioned storage cost for an AWS MSK cluster. Cost represented in USD/hour | `region`=<AWS region> <br/> `cluster_name`=<MSK cluster name> <br/> `cluster_arn`=<MSK cluster ARN>                             |

## Overview

The MSK collector exports list-price hourly cost metrics for supported Amazon Managed Streaming for Apache Kafka clusters across all configured AWS regions.

V1 supports provisioned clusters backed by local EBS storage only. Serverless, Express, tiered storage, provisioned throughput, replicator, and transfer pricing are not included.

## Configuration

Enable the MSK collector by adding `msk` to your AWS services configuration:

```yaml
aws:
  services: ["ec2", "s3", "msk"]
  regions: ["us-east-1", "us-west-2"]
```

Or via command line:
```bash
--aws.services=ec2,s3,msk
```

## Labels

- **region**: The AWS region where the MSK cluster is running
- **cluster_name**: The name of the MSK cluster
- **cluster_arn**: The Amazon Resource Name (ARN) of the MSK cluster
- **instance_type**: The broker instance type (compute metric only)

## Notes

- Compute cost is the broker-hour list price multiplied by the cluster's broker count
- Storage cost is derived from provisioned storage (`broker_count * volume_size_gib_per_broker`), not live disk consumption
- Clusters that require extra pricing dimensions are skipped with a warning so the scrape can continue
- Pricing data is fetched from the AWS Pricing API (us-east-1 region)
- Metrics are refreshed periodically based on the configured scrape interval
- All costs are represented in USD per hour

## IAM Permissions

Required permissions for MSK metrics collection:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "kafka:ListClustersV2"
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
