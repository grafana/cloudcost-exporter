# NAT Gateway Metrics

| Metric name | Metric type | Description | Labels |
|---|---|---|---|
| cloudcost_aws_natgateway_hourly_rate_usd_per_hour | Gauge | NAT Gateway price in USD/hour; Regional rates apply per availability zone | `account_id`, `region`, `gateway_type` |
| cloudcost_aws_natgateway_data_processing_usd_per_gb | Gauge | NAT Gateway data processing price in USD/GB | `account_id`, `region`, `gateway_type` |

## Overview

The NAT Gateway module collects pricing information for [AWS NAT Gateways](https://docs.aws.amazon.com/vpc/latest/userguide/vpc-nat-gateway.html), which provide outbound internet access for resources in private subnets. The cost components of NAT Gateways are [split into two Usage Types](https://aws.amazon.com/vpc/pricing/):

1. **Hourly Rate**: A fixed hourly charge for running the NAT Gateway, regardless of usage
2. **Data Processing**: A variable charge based on the amount of data processed through the NAT Gateway

## Metrics Details

### Hourly Rate (`cloudcost_aws_natgateway_hourly_rate_usd_per_hour`)

- **Type**: Gauge
- **Unit**: USD per hour
- **Description**: The fixed hourly cost for running a NAT Gateway, charged for each hour the NAT Gateway is available
- **Labels**: `account_id` - AWS account ID; `region` - AWS region code; `gateway_type` - `standard` or `regional`
- **Usage**: This metric helps track the baseline cost of NAT Gateway infrastructure

### Data Processing (`cloudcost_aws_natgateway_data_processing_usd_per_gb`)

- **Type**: Gauge
- **Unit**: USD per GB
- **Description**: The cost for processing data through the NAT Gateway
- **Labels**: `account_id` - AWS account ID; `region` - AWS region code; `gateway_type` - `standard` or `regional`
- **Usage**: This metric helps estimate costs based on data transfer volume through NAT Gateways

### Regional NAT Gateways

Both metrics use `gateway_type="standard"` for standard NAT Gateways and `gateway_type="regional"` for Regional NAT Gateways. The hourly rate applies to each availability zone where the Regional NAT Gateway is configured. Multiply that rate by the number of active availability zones to estimate its hourly charge.

Standard and Regional prices are alternatives. Do not add them together for the same traffic or gateway.

### Query compatibility

Filter by `gateway_type` before aggregating prices. For standard NAT Gateways, this selector also accepts older series without the label:

```promql
cloudcost_aws_natgateway_data_processing_usd_per_gb{gateway_type!="regional"}
```

Use `{gateway_type="regional"}` for Regional NAT Gateways. Apply the same filtering to the hourly metric. Update consumers before deploying the labeled metrics: an unfiltered `sum` adds alternative prices, and an unfiltered `max` can select the other gateway type's price.

## Pricing Source

The pricing data is sourced from the [AWS Pricing API](https://docs.aws.amazon.com/aws-cost-management/latest/APIReference/API_pricing_GetProducts.html) and is updated every 24 hours.

The pricing filters used are:
- **Product Family**: `NAT Gateway`
- **Usage Types**:
  - `NatGateway-Hours` - for hourly rates
  - `NatGateway-Bytes` - for standard data processing costs
  - `RegionalNatGateway-Hours` - for Regional hourly rates per availability zone
  - `RegionalNatGateway-Bytes` - for Regional data processing costs

Usage types match exactly, with or without an AWS region prefix such as `USE1-`. Prices from different gateway types are never combined. If the catalog includes both prefixed and unprefixed aliases for the same gateway type and cost component, the exporter uses their maximum rate instead of summing them. A metric is emitted only when its price is positive.

## IAM Permissions

Required permissions for NAT Gateway metrics collection:

```json
{
    "Version": "2012-10-17",
    "Statement": [
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

**Note:** NAT Gateway metrics are collected via the AWS Pricing API only. No NAT Gateway-specific API calls are required, as the exporter provides pricing rates rather than tracking individual NAT Gateway resources.
