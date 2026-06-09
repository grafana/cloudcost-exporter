# AWS Bedrock Metrics

| Metric name                                              | Metric type | Description                                                                      | Labels                                                                                                                                                              |
|----------------------------------------------------------|-------------|---------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_aws_bedrock_input_usd_per_1k_tokens            | Gauge       | Input token price for an AWS Bedrock model. Cost represented in USD/1000 tokens  | `account_id`=&lt;AWS account ID&gt; <br/> `region`=&lt;AWS region&gt; <br/> `model_id`=&lt;model slug&gt; <br/> `family`=&lt;model provider&gt; <br/> `price_tier`=&lt;see Labels&gt; |
| cloudcost_aws_bedrock_output_usd_per_1k_tokens           | Gauge       | Output token price for an AWS Bedrock model. Cost represented in USD/1000 tokens | `account_id`=&lt;AWS account ID&gt; <br/> `region`=&lt;AWS region&gt; <br/> `model_id`=&lt;model slug&gt; <br/> `family`=&lt;model provider&gt; <br/> `price_tier`=&lt;see Labels&gt; |
| cloudcost_aws_bedrock_search_unit_usd_per_1k_search_units | Gauge      | Search unit price for an AWS Bedrock model (e.g. Cohere Rerank). Cost represented in USD/1000 search units | `account_id`=&lt;AWS account ID&gt; <br/> `region`=&lt;AWS region&gt; <br/> `model_id`=&lt;model slug&gt; <br/> `family`=&lt;model provider&gt; <br/> `price_tier`=&lt;see Labels&gt; |

## Overview

The Bedrock collector exports list-price cost metrics for AWS Bedrock foundation models across all configured regions. These are pricing rates, not measured spend. Multiply rates by token or search-unit usage (e.g. from CloudWatch `AWS/Bedrock` metrics) to compute estimated cost.

Prices come from two AWS Pricing API service codes, merged into the same metrics:

- **`AmazonBedrock`** — Amazon-native models (Nova, Titan) plus most third-party text models with an explicit `provider` attribute (Anthropic, Meta, Mistral, DeepSeek, Qwen, Google, and others).
- **`AmazonBedrockFoundationModels`** — Bedrock Marketplace models, including Claude 4.x, Cohere Rerank and Embed, Writer Palmyra, AI21 Jamba, and TwelveLabs.

See [Pricing Sources](#pricing-sources) for how the two are combined.

## Configuration

Enable the Bedrock collector by adding `bedrock` to your AWS services configuration:

```yaml
aws:
  services: ["ec2", "s3", "bedrock"]
  regions: ["us-east-1", "us-west-2"]
```

Or via command line:
```bash
--aws.services=ec2,s3,bedrock
```

Restrict which model families are emitted with `--aws.bedrock.families` (a regex matched against the `family` label). The default `.*` emits all families.

## Labels

- **account_id**: The AWS account ID (12-digit), resolved via STS GetCallerIdentity
- **region**: The AWS region for which the price applies
- **model_id**: Normalized model identifier, lowercase with spaces replaced by hyphens, uniform across both pricing sources (e.g. `claude-3-sonnet`, `claude-sonnet-4.6`, `nova-pro`, `llama-3.1-405b`, `cohere-rerank-v3.5`).
  - `AmazonBedrock` derives it from the human-readable `model` attribute, falling back to the normalized `usagetype` slug for the few SKUs (some Titan, Rerank) that lack `model`.
  - `AmazonBedrockFoundationModels` derives it from the `servicename`.
  - Models that AWS prices per modality under one name carry the modality (e.g. `nova-sonic-text`, `nova-sonic-speech`).

  `model_id` is a normalized label derived from pricing metadata, not a canonical Bedrock model ID or ARN. Because both sources normalize to the same `model_id`, a model priced under both (e.g. the legacy Claude generation) merges into one set of series: identical prices dedupe, and a price one source lacks (the standard source prices some legacy Claude models for input only) is filled in by the other.
- **family**: Model provider, normalized to lowercase (spaces become underscores) from the `AmazonBedrock` `provider` attribute, or derived from the `AmazonBedrockFoundationModels` `servicename`. The set tracks whatever AWS publishes, so it is open-ended; observed values include `anthropic`, `amazon`, `cohere`, `meta`, `ai21`, `mistral`, `deepseek`, `google`, `qwen`, `nvidia`, `openai`, `writer`, and `twelvelabs`. Amazon-developed models with no provider attribute (Nova, Titan) use `amazon`. A marketplace `servicename` with no recognized provider falls back to `unknown`. Filter with `--aws.bedrock.families`.
- **price_tier**: Inference tier:
  - Token metrics: `on_demand`, `on_demand_batch`, `on_demand_flex`, `on_demand_priority`, `cross_region`, `cross_region_batch`, `cross_region_flex`, `cross_region_priority`.
  - Search-unit metrics: `on_demand`, `cross_region`.

## Pricing Sources

The collector fetches `AmazonBedrock` and `AmazonBedrockFoundationModels` per region and merges them into one metric set. Each SKU's direction, family, model, and price tier are parsed from its `usagetype` and `servicename`; image, video, audio, cache, provisioned-throughput, and guardrail SKUs are skipped.

- `AmazonBedrock` publishes token prices per 1000 tokens directly.
- `AmazonBedrockFoundationModels` publishes token prices per 1,000,000 tokens (converted to per-1000) and search-unit prices per single unit (converted to per-1000).

When both service codes price the same model (the legacy Claude generation), they share a `model_id`, so per-region/direction/tier entries dedupe and any price only one source carries is retained. Overlapping prices are identical across the two sources.

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

**Note:** Bedrock metrics are collected via the AWS Pricing API only. No Bedrock-specific API calls are required, as the exporter provides pricing rates rather than tracking individual Bedrock resources.
