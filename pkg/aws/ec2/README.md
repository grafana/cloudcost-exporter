# ec2 cost module

This module is responsible for collecting pricing information for EC2 instances.
See [metrics](/docs/metrics/aws/ec2.md) for more information on the metrics that are collected.

## Overview

EC2 instances are a foundational component in the AWS ecosystem. 
They can be used as bare bone virtual machines, or used as the underlying infrastructure for services like 
1. [EC2 instances](https://aws.amazon.com/ec2/pricing/on-demand/)
1. [ECS Clusters](https://aws.amazon.com/ecs/pricing/) that use ec2 instances*
1. [EKS Clusters](https://aws.amazon.com/eks/pricing/)

This module aims to emit metrics generically for ec2 instances that can be used for the services above.
A conscious decision was made the keep ec2 + eks implementations coupled.
See [#215](https://github.com/grafana/cloudcost-exporter/pull/215) for more details on _why_, as this decision _can_ be reversed in the future.

*Fargate is a serverless product which builds upon ec2 instances, but with a specific caveat: [Pricing is based upon the requests by workloads](https://aws.amazon.com/fargate/pricing/)
> ![WARNING]
> Even though Fargate uses ec2 instances under the hood, it would require a separate module since the pricing comes from a different end point

## Pricing Map

The pricing map is generated based on the machine type and the region where the instance is running.

Here's how the data structure looks like:

```
--> root
    --> region
        --> machine type
           --> reservation type(on-demand, spot)
              --> price
```

Regions are populated by making a [describe region](https://docs.aws.amazon.com/AWSEC2/latest/APIReference/API_DescribeRegions.html) api call to find the regions enabled for the account.
The [price](https://github.com/grafana/cloudcost-exporter/blob/eb6b3ed9e0d4ab4eb27bda71ada091730c95f709/pkg/aws/ec2/pricing_map.go#L62) keeps track of the hourly cost per:
1. price per cpu
2. price per GiB of ram
3. total price

The pricing information for the compute instances is collected from the AWS Pricing API.
Detailed documentation around the pricing API can be found [here](https://aws.amazon.com/ec2/pricing/on-demand/).
One of the main challenges with EKS compute instance pricing is that the pricing is for the full instance and not broken down by resource.
This means that the pricing information is not available for the CPU and memory separately.
`cloudcost-exporter` makes the assumption that the ratio of costs is relatively similar to that of GKE instances.
When fetching the list prices, `cloudcost-exporter` will use the ratio from GCP to break down the cost of the instance into CPU and memory.
 
## Collecting Machines

The following attributes must be available from the ec2 instance to make the lookup:
- region
- machine type
- reservation type
 
Every time the collector is scraped, a list of machines is collected _by region_ in a seperate goroutine.
This allows the collector to scrape each region in parallel, making the largest region be the bottleneck. 
For simplicity, there is no cache, though this is a nice feature to add in the future. 

## Cost Calculations

Here's some example PromQL queries that can be used to calculate the costs of ec2 instances:

```PromQL
// Calculate the total hourly cost of all ec2 instances
sum(cloudcost_aws_ec2_instance_total_usd_per_houry)
// Calculate the total hourly cost by region
sum by (region) (cloudcost_aws_ec2_instance_total_usd_per_houry)
// Calculate the total hourly cost by machine type
sum by (machine_type) (cloudcost_aws_ec2_instance_total_usd_per_houry)
// Calculate the total hourly cost by reservation type
sum by (reservation) (cloudcost_aws_ec2_instance_total_usd_per_houry)
```

You can do more interesting queries if you run [yet-another-cloudwatch-exporter](https://github.com/nerdswords/yet-another-cloudwatch-exporter) and export the following metrics:
- `aws_ec2_info`

All of these examples assume that you have created the tag name referenced in the examples.

```PromQL
// Calculate the total hourly cost by team
// Assumes a tag called `Team` has been created on the ec2 instances
sum by (team) (
    cloudcost_aws_ec2_instance_total_usd_per_houry
    * on (instance_id) group_right()
    label_join(aws_ec2_info, "team", "tag_Team")
)

// Calculate the total hourly cost by team and environment
// Assumes a tag called `Team` has been created on the ec2 instances
// Assumes a tag called `Environment` has been created on the ec2 instances
sum by (team, environment) (
    cloudcost_aws_ec2_instance_total_usd_per_houry
    * on (instance_id) group_right()
    label_join(
        label_join(aws_ec2_info, "environment", "tag_Environment")
    "team", "tag_Team")
)
```
