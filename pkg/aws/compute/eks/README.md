# EKS Module

This module is responsible for collecting pricing information for EKS clusters.
Specifically it collects the pricing information for the following resources:
- compute instances
- storage

## Compute Instances

The pricing information for the compute instances is collected from the AWS Pricing API.
Detailed documentation around the pricing API can be found [here](https://aws.amazon.com/ec2/pricing/on-demand/).
One of the main challenges with EKS compute instance pricing is that the pricing is for the full instance and not broken down by resource.
This means that the pricing information is not available for the CPU and memory separately.
`cloudcost-exporter` makes the assumption that the ratio of costs is relatively similar to that of GKE instances.
When fetching the list prices, `cloudcost-exporter` will use the ratio from GCP to break down the cost of the instance into CPU and memory.
- [ ] TODO: Document formulate to figure out

## Pricing Map assumptions

- The pricing map is generated based on the instance type and the region where the instance is running
- Each instance type has a different price per hour depending on the features of the instance
  - Operating System
  - Chipset
