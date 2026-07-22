# EKS compute Metrics

| Metric name                                        | Metric type | Description                                                                                  | Labels                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
|----------------------------------------------------|-------------|----------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_aws_ec2_instance_cpu_usd_per_core_hour   | Gauge       | The processing cost of a EC2 Compute Instance in USD/(core*h) | `account_id`=&lt;AWS account ID&gt; <br/> `cluster_name`=&lt;name of the cluster the instance is associated with, if it exists. Can be empty&gt; <br/> `instance`=&lt;name of the compute instance&gt; <br/> `instance_id`=&lt;The unique id associated with the compute instance&gt; <br/> `region`=&lt;AWS region code&gt; <br/> `family`=&lt;broader compute family (General Purpose, Compute Optimized, Memory Optimized, ...) &gt; <br/> `machine_type`=&lt;specific machine type, e.g.: m7a.large&gt; <br/>  `price_tier`=&lt;spot|ondemand|capacityblock&gt; `architecture`=&lt;arm64\|x86_64 &gt; |
| cloudcost_aws_ec2_instance_memory_usd_per_gib_hour | Gauge       | The memory cost of a EC2 Compute Instance in USD/(GiB*h)       | `account_id`=&lt;AWS account ID&gt; <br/> `cluster_name`=&lt;name of the cluster the instance is associated with, if it exists. Can be empty&gt; <br/> `instance`=&lt;name of the compute instance&gt; <br/> `instance_id`=&lt;The unique id associated with the compute instance&gt; <br/> `region`=&lt;AWS region code&gt; <br/> `family`=&lt;broader compute family (General Purpose, Compute Optimized, Memory Optimized, ...)  &gt; <br/> `machine_type`=&lt;specific machine type, e.g.: m7a.large&gt; <br/>  `price_tier`=&lt;spot|ondemand|capacityblock&gt; `architecture`=&lt;arm64\|x86_64 &gt;                                     |
| cloudcost_aws_ec2_instance_total_usd_per_hour      | Gauge       | The total cost of an EC2 Compute Instance in USD/*h)           | `account_id`=&lt;AWS account ID&gt; <br/> `cluster_name`=&lt;name of the cluster the instance is associated with, if it exists. Can be empty&gt; <br/> `instance`=&lt;name of the compute instance&gt; <br/> `instance_id`=&lt;The unique id associated with the compute instance&gt; <br/> `region`=&lt;AWS region code&gt; <br/> `family`=&lt;broader compute family (General Purpose, Compute Optimized, Memory Optimized, ...)  &gt; <br/> `machine_type`=&lt;specific machine type, e.g.: m7a.large&gt; <br/>  `price_tier`=&lt;spot|ondemand|capacityblock&gt; `architecture`=&lt;arm64\|x86_64 &gt;                                     |

## EBS Metrics

| Metric name                                        | Metric type | Description                                                                                  | Labels                                                                                                                                                                                                                                                                                                                                                                                                                          |
|----------------------------------------------------|-------------|----------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_aws_ec2_persistent_volume_usd_per_hour   | Gauge       | The cost of an EBS Volume in USD/h | `account_id`=&lt;AWS account ID&gt; <br/> `availability_zone`=&lt;AWS AZ code&gt; <br/> `disk`=&lt;EBS volume ID&gt; <br/> `persistentvolume`=&lt;k8s persistent volume ID&gt; <br/> `region`=&lt;AWS region code&gt; <br/> `size_gib`=&lt;volume size in GiB, can always be parsed to an integer&gt; <br/> `state`=&lt;volume state, eg: available, in-use; <br/>  `type`=&lt;volume type, eg: gp2, gp3&gt;   |


## Pricing Source

The pricing data is sourced from the [AWS Pricing API](https://docs.aws.amazon.com/aws-cost-management/latest/APIReference/API_pricing_GetProducts.html) and is updated every 24 hours.
There are a few assumptions that we're making specific to Grafana Labs:
1. All costs are in USD
2. Only consider Linux based instances
3. `cloudcost-exporter` emits the list price and does not take into account any discounts or savings plans

### Capacity Blocks for ML

Instances running in an [EC2 Capacity Block for ML](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-capacity-blocks.html) are emitted with `price_tier=capacityblock`. Unlike on-demand and spot (which use list prices from the Pricing API), Capacity Blocks are prepaid: the price is **spend-derived** from Cost Explorer, like the S3 collector.

This is **opt-in** via `-aws.ec2.capacity-blocks` (off by default) because it adds Cost Explorer calls and only applies to accounts that purchase Capacity Blocks. Notes:

- The upfront fee is amortized into an hourly rate per reservation: `fee / (instance_count * block_hours)`, where the fee comes from Cost Explorer (dated to the reservation's start date) and the count/duration come from `DescribeCapacityReservations`. Running instances are matched to their reservation via `CapacityReservationId`.
- Only **active** reservations are priced, so expired/cancelled blocks are excluded.
- The rate is emitted only while an instance is running, even though the block is paid for regardless of usage.
- For **Linux** instances the upfront fee is the complete cost; a premium operating system would incur an additional usage charge that is not modeled.

## IAM Permissions

Required permissions for EC2 and EBS metrics collection:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "ec2:DescribeRegions",
                "ec2:DescribeInstances",
                "ec2:DescribeSpotPriceHistory",
                "ec2:DescribeVolumes"
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

When `-aws.ec2.capacity-blocks` is enabled, the EC2 role additionally requires `ec2:DescribeCapacityReservations` and `ce:GetCostAndUsage`.
