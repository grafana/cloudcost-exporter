# AWS Bedrock Metrics

| Metric name                                               | Metric type | Description                                                        | Labels                                                                                                                                                                |
|-----------------------------------------------------------|-------------|--------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_aws_bedrock_token_input_usd_per_1k_tokens       | Gauge       | List price for AWS Bedrock input tokens in USD per 1000 tokens     | `account_id`=<AWS account ID> <br/> `region`=<AWS region> <br/> `model_id`=<model slug> <br/> `family`=<model provider> <br/> `price_tier`=<on_demand\|on_demand_batch\|on_demand_flex\|on_demand_priority\|cross_region> |
| cloudcost_aws_bedrock_token_output_usd_per_1k_tokens      | Gauge       | List price for AWS Bedrock output tokens in USD per 1000 tokens    | `account_id`=<AWS account ID> <br/> `region`=<AWS region> <br/> `model_id`=<model slug> <br/> `family`=<model provider> <br/> `price_tier`=<on_demand\|on_demand_batch\|on_demand_flex\|on_demand_priority\|cross_region> |
| cloudcost_aws_bedrock_search_unit_usd_per_1k_search_units | Gauge       | List price for AWS Bedrock search units in USD per 1000 search units (e.g. Cohere Rerank) | `account_id`=<AWS account ID> <br/> `region`=<AWS region> <br/> `model_id`=<model slug> <br/> `family`=<model provider> <br/> `price_tier`=<on_demand\|cross_region> |

## Overview

The Bedrock collector exports list-price token cost metrics for AWS Bedrock foundation models across all configured regions. These are pricing rates, not measured spend. Multiply rates by token usage (e.g. from CloudWatch `AWS/Bedrock` metrics) to compute estimated cost.

## Configuration

Enable the Bedrock collector by adding `bedrock` to your AWS services configuration:

```yaml
aws:
  services: ["bedrock"]
  regions: ["us-east-1", "us-west-2"]
```

Or via command line:
```bash
--aws.services=bedrock
```

## Labels

- **account_id**: The AWS account ID (12-digit), resolved via STS GetCallerIdentity
- **region**: The AWS region for which the price applies
- **model_id**: The model slug from the AWS Pricing API `usagetype` field (e.g. `Claude3Sonnet`, `Llama4-Scout-17B`, `Nova2.0Pro`)
- **family**: The model provider, lowercased with spaces replaced by underscores (e.g. `anthropic`, `amazon`, `meta`, `mistral_ai`). Amazon-developed models with no provider attribute use `amazon`.
- **price_tier**: The inference tier: `on_demand`, `on_demand_batch`, `on_demand_flex`, `on_demand_priority`, or `cross_region`

## Notes

- Pricing data is fetched from the AWS Pricing API (us-east-1 endpoint)
- Prices are refreshed every 24 hours
- Only text token SKUs are emitted (image, video, audio, cache, and guardrail SKUs are silently skipped)
- The `model_id` label is the pricing SKU slug, not the canonical Bedrock model ARN

## IAM Permissions

Required permissions for Bedrock metrics collection:

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
