# AWS Bedrock Metrics

| Metric name                                               | Metric type | Description                                                   | Labels                                                                                                                                                                |
|-----------------------------------------------------------|-------------|---------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `cloudcost_aws_bedrock_input_usd_per_1k_tokens`           | Gauge       | AWS Bedrock input token price in USD per 1000 tokens          | `account_id`=<AWS account ID> <br/> `region`=<AWS region> <br/> `model_id`=<model slug> <br/> `family`=<model provider> <br/> `price_tier`=<see price_tier> |
| `cloudcost_aws_bedrock_output_usd_per_1k_tokens`          | Gauge       | AWS Bedrock output token price in USD per 1000 tokens         | `account_id`=<AWS account ID> <br/> `region`=<AWS region> <br/> `model_id`=<model slug> <br/> `family`=<model provider> <br/> `price_tier`=<see price_tier> |
| `cloudcost_aws_bedrock_search_unit_usd_per_1k_search_units` | Gauge     | AWS Bedrock search unit price in USD per 1000 search units (e.g. Cohere Rerank) | `account_id`=<AWS account ID> <br/> `region`=<AWS region> <br/> `model_id`=<model slug> <br/> `family`=<model provider> <br/> `price_tier`=<see price_tier> |

## Overview

The Bedrock collector exports list-price cost metrics for AWS Bedrock foundation models across all configured regions. These are pricing rates, not measured spend. Multiply rates by token or search-unit usage (e.g. from CloudWatch `AWS/Bedrock` metrics) to compute estimated cost.

Prices come from two AWS Pricing API service codes, merged into the same metrics:

- **`AmazonBedrock`** — first-party and earlier models (Claude 2/3, Amazon Nova and Titan).
- **`AmazonBedrockFoundationModels`** — Bedrock Marketplace models, including Claude 4.x, Cohere Rerank and Embed, Meta Llama, AI21, Writer Palmyra, and TwelveLabs.

See [Pricing sources](#pricing-sources) for how the two are combined.

## Configuration

Add `bedrock` to your AWS services configuration:

```yaml
aws:
  services: ["bedrock"]
  regions: ["us-east-1", "us-west-2"]
```

Or via command line:
```bash
--aws.services=bedrock
```

Restrict which model families are emitted with `--aws.bedrock.families` (a regex matched against the `family` label). The default `.*` emits all families.

## Labels

- **`account_id`**: AWS account ID (12-digit), resolved via STS `GetCallerIdentity`
- **`region`**: AWS region for which the price applies
- **`model_id`**: Normalized model identifier. The two pricing sources use different conventions:
  - `AmazonBedrock` emits the `usagetype` slug as published (e.g. `Claude3Sonnet`, `NovaPro`, `Llama4-Scout-17B`).
  - `AmazonBedrockFoundationModels` emits the `servicename` lowercased with spaces replaced by hyphens (e.g. `claude-sonnet-4.6`, `cohere-rerank-v3.5`, `palmyra-x5`).

  `model_id` is a normalized label derived from pricing metadata, not a canonical Bedrock model ID or ARN. A few legacy Claude models (`Claude3Haiku`/`claude-3-haiku`, `Claude3Sonnet`/`claude-3-sonnet`, `ClaudeInstant`/`claude-instant`) are priced under both service codes and so appear under both conventions; the prices agree where the two overlap.
- **`family`**: Model provider, derived from the pricing metadata. One of `anthropic`, `amazon`, `cohere`, `meta`, `ai21`, `stability`, `writer`, `twelvelabs`, or `unknown` for an unrecognized provider. Amazon-developed models with no provider attribute (Nova, Titan) use `amazon`. Filter with `--aws.bedrock.families`.
- **`price_tier`**: Inference tier:
  - Token metrics: `on_demand`, `on_demand_batch`, `on_demand_flex`, `on_demand_priority`, `cross_region`, `cross_region_batch`, `cross_region_flex`, `cross_region_priority`.
  - Search-unit metrics: `on_demand`, `cross_region`.

## Pricing sources

The collector fetches `AmazonBedrock` and `AmazonBedrockFoundationModels` per region and merges them into one metric set. SKUs are matched by description; image, video, audio, cache, provisioned-throughput, and guardrail SKUs are skipped.

- `AmazonBedrock` publishes token prices per 1000 tokens directly.
- `AmazonBedrockFoundationModels` publishes token prices per 1,000,000 tokens (converted to per-1000) and search-unit prices per single unit (converted to per-1000).

`AmazonBedrockFoundationModels` pricing is best-effort: if that service code is unavailable, the collector logs a warning and continues to emit `AmazonBedrock` pricing rather than failing.

## Notes

- Pricing data is fetched from the AWS Pricing API (`us-east-1` endpoint) and refreshed every 24 hours
- Image, video, audio, cache, provisioned-throughput, and guardrail SKUs are skipped; only token and search-unit SKUs are emitted
- `model_id` is a normalized pricing identifier, not the canonical Bedrock model ARN

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
