# ec2 cost module

This module is responsible for collecting pricing information for EC2 instances.
See [metrics](/docs/metrics.md) for more information on the metrics that are collected.

## Overview

ec2 has a fairly large overlap with [eks]() for the pricing map and collecting of instances.
The primary reason to have two dedicated implementations is that they emit two distinct sets of metrics.
There are two differences in the ec2 implementation:
- filters out instances associated with eks clusters
- emits the __total__ price of a machine

The primary use case for this metric is to associate an ec2 instance to a team, by environment.
A major assumption made for the module is that each machine has one distinct owner. 

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

Regions are populated by making an [api call]() to find the regions enabled for the account.
The [price]() instance has an attribute for the __total__ hourly cost of a machine.

The following attributes must be available to make the lookup:
- region
- machine type
- reservation type
- operating system

The pricing information for the compute instances is collected from the AWS Pricing API.
Detailed documentation around the pricing API can be found [here](https://aws.amazon.com/ec2/pricing/on-demand/).
One of the main challenges with EKS compute instance pricing is that the pricing is for the full instance and not broken down by resource.
This means that the pricing information is not available for the CPU and memory separately.
`cloudcost-exporter` makes the assumption that the ratio of costs is relatively similar to that of GKE instances.
When fetching the list prices, `cloudcost-exporter` will use the ratio from GCP to break down the cost of the instance into CPU and memory.
 
## Collecting Machines



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

You can do more interesting queries if you run [yace]() and export the following metrics:
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