# EKS compute Metrics

| Metric name                                                | Metric type | Description                                                                                  | Labels                                                                                                                                                                                                                                                                                                                                                     |
|------------------------------------------------------------|-------------|----------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_aws_eks_instance_cpu_usd_per_core_hour           | Gauge       | The processing cost of a EC2 Compute Instance, associated to an EKS cluster, in USD/(core*h) | `cluster_name`=&lt;name of the cluster the instance is associated with&gt; <br/> `instance`=&lt;name of the compute instance&gt; <br/> `region`=&lt;GCP region code&gt; <br/> `family`=&lt;broader compute family (n1, n2, c3 ...) &gt; <br/> `machine_type`=&lt;specific machine type, e.g.: n2-standard-2&gt; <br/>  `price_tier`=&lt;spot\|ondemand&gt; |
| cloudcost_aws_eks_compute_instance_memory_usd_per_gib_hour | Gauge       | The memory cost of a EC2 Compute Instance, associated to a EK2 cluster, in USD/(GiB*h)       | `cluster_name`=&lt;name of the cluster the instance is associated with&gt; <br/> `instance`=&lt;name of the compute instance&gt; <br/> `region`=&lt;GCP region code&gt; <br/> `family`=&lt;broader compute family (n1, n2, c3 ...) &gt; <br/> `machine_type`=&lt;specific machine type, e.g.: n2-standard-2&gt; <br/>  `price_tier`=&lt;spot\|ondemand&gt; |

## Pricing Source

The pricing data is sourced from the [AWS Pricing API](https://docs.aws.amazon.com/aws-cost-management/latest/APIReference/API_pricing_GetProducts.html) and is updated every 24 hours.
There are a few assumptions that we're making specific to Grafana Labs:
1. All costs are in USD
2. Only consider Linux based instances
3. `cloudcost-exporter` emits the list price and does not take into account any discounts or savings plans
4. Only ec2 instances that are associated with an EKS cluster have their pricing metrics exported

