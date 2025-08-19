# NAT Gateway Metrics

| Metric name                                        | Metric type | Description                                                                                  | Labels                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
|----------------------------------------------------|-------------|----------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_aws_natgateway_hourly_rate_usd_per_hour   | Gauge       | The hourly cost of a NAT Gateway in USD/hour | `region`=&lt;AWS region code&gt; |
| cloudcost_aws_natgateway_data_processing_usd_per_gb | Gauge       | The data processing cost of a NAT Gateway in USD/GB       | `region`=&lt;AWS region code&gt; |

## Overview

The NAT Gateway module collects pricing information for [AWS NAT Gateways](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-nat-gateway.html), which provide outbound internet access for resources in private subnets. The cost components of NAT Gateways are [split into two Usage Types](https://aws.amazon.com/vpc/pricing/):

1. **Hourly Rate**: A fixed hourly charge for running the NAT Gateway, regardless of usage
2. **Data Processing**: A variable charge based on the amount of data processed through the NAT Gateway

## Metrics Details

### Hourly Rate (`cloudcost_aws_natgateway_hourly_rate_usd_per_hour`)

- **Type**: Gauge
- **Unit**: USD per hour
- **Description**: The fixed hourly cost for running a NAT Gateway, charged for each hour the NAT Gateway is available
- **Labels**: `region` - The AWS region where the NAT Gateway is deployed
- **Usage**: This metric helps track the baseline cost of NAT Gateway infrastructure

### Data Processing (`cloudcost_aws_natgateway_data_processing_usd_per_gb`)

- **Type**: Gauge
- **Unit**: USD per GB
- **Description**: The cost for processing data through the NAT Gateway
- **Labels**: `region` - The AWS region where the NAT Gateway is deployed
- **Usage**: This metric helps estimate costs based on data transfer volume through NAT Gateways

## Pricing Source

The pricing data is sourced from the [AWS Pricing API](https://docs.aws.amazon.com/aws-cost-management/latest/APIReference/API_pricing_GetProducts.html) and is updated every 24 hours.

The pricing filters used are:
- **Product Family**: `NAT Gateway`
- **Usage Types**:
  - `NatGateway-Hours` - for hourly rates
  - `NatGateway-Bytes` - for data processing costs
