# AWS MSK Metrics

| Metric name                                              | Metric type | Description                                                                 | Labels                                                                                                                                               |
|----------------------------------------------------------|-------------|-----------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_aws_msk_compute_hourly_rate_usd_per_hour       | Gauge       | Hourly broker compute cost for an AWS MSK cluster. Cost represented in USD/hour | `region`=&lt;AWS region&gt; <br/> `cluster_name`=&lt;MSK cluster name&gt; <br/> `cluster_arn`=&lt;MSK cluster ARN&gt; <br/> `instance_type`=&lt;broker instance type&gt; |
| cloudcost_aws_msk_storage_hourly_rate_usd_per_hour       | Gauge       | Hourly provisioned storage cost for an AWS MSK cluster. Cost represented in USD/hour | `region`=&lt;AWS region&gt; <br/> `cluster_name`=&lt;MSK cluster name&gt; <br/> `cluster_arn`=&lt;MSK cluster ARN&gt;                             |

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

## Metric semantics

- `cloudcost_aws_msk_compute_hourly_rate_usd_per_hour` is the broker-hour list price multiplied by the cluster's broker count.
- `cloudcost_aws_msk_storage_hourly_rate_usd_per_hour` is derived from provisioned storage, not live disk consumption.
- Storage cost uses total allocated cluster storage: `broker_count * volume_size_gib_per_broker`.
- Clusters that require extra pricing dimensions are skipped with a warning so the scrape can continue.

## Pricing source

- Broker pricing is resolved from the AWS Pricing API in `us-east-1` using the regional broker shape.
- Storage pricing is resolved from the AWS Pricing API in `us-east-1` using the regional local-storage SKU.
- All costs are emitted in USD and represent list price only.

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
