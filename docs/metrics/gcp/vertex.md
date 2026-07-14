# Vertex AI Metrics

Metrics exported for the GCP Vertex AI service.

## Alignment with the Bedrock collector

These metrics mirror the AWS Bedrock collector's shape where GCP pricing allows: input and output are
one metric distinguished by `gen_ai_token_type`, the model label is `gen_ai_request_model`, and every
metric carries a billing-scope label (`project_id` here, `account_id` on Bedrock).

The tier stays a single composed `price_tier` label rather than splitting into Bedrock's orthogonal
`region_tier` / `quota_tier` / `cache_ttl`, because Vertex pricing does not decompose the same way:

- Vertex has no cross-region inference pricing, so there is no `region_tier` equivalent.
- `price_tier` composes four independent dimensions (quota, caching, long context, thinking). Folding
  them into a single `quota_tier` would let SKUs with different prices collide and overwrite each
  other, so the composed label is retained to keep every dimension.

## Configuration

Enable the Vertex collector by adding `vertex` to the experimental GCP services:

```bash
--project-id=<project> --gcp.experimental.services=vertex
```

Restrict which model families are emitted with `--gcp.vertex.families`, a regex matched against the
`family` label. The default `.*` emits all families; set e.g. `google|anthropic` to drop the Model
Garden long tail (`deepseek`, `alibaba`, `meta`, and so on). Mirrors Bedrock's `--aws.bedrock.families`.

## Token Pricing

| Metric | Labels | Description |
|--------|--------|-------------|
| `cloudcost_gcp_vertex_usd_per_1k_tokens` | `project_id`, `region`, `gen_ai_request_model`, `family`, `gen_ai_token_type`, `price_tier` | Cost in USD per 1k tokens, by `gen_ai_token_type`, for models billed by token. Character-billed models use the character metric. |

### Labels

| Label | Values | Description |
|-------|--------|-------------|
| `project_id` | e.g. `my-gcp-project` | Billing-scope project: the single auth project (`--project-id`). Prices are project-independent, so one value is stamped on every series, mirroring the single `account_id` on the Bedrock metrics |
| `region` | e.g. `us-central1` | GCP region |
| `gen_ai_request_model` | e.g. `gemini-1.5-flash`, `gemma-4`, `llama-4-maverick` | Model name, normalised to lowercase with spaces replaced by hyphens |
| `family` | `google`, `meta`, `alibaba`, `deepseek`, `minimax`, `moonshot`, `unknown` | Model provider family; `unknown` for unrecognised model prefixes |
| `gen_ai_token_type` | `input`, `output` | Whether the price is for input or output tokens |
| `price_tier` | see below | Running mode or pricing tier derived from the GCP SKU description |

#### `price_tier` Values for Token Metrics

Tiers are composed from up to three modifiers: a `thinking_` prefix, a `cached_` prefix, and a `_long_context` suffix. Not all combinations exist in GCP's SKU catalogue; only tiers with a matching SKU are emitted.

**Simple tiers**

| Value | Description |
|-------|-------------|
| `on_demand` | Standard real-time inference |
| `batch` | Batch prediction |
| `long_context` | Long-context window at standard priority |
| `cached` | Context-cached input |
| `cache_storage` | Context cache storage cost |
| `thinking` | Extended thinking, standard priority |
| `priority` | Priority tier |
| `flex` | Flex tier |
| `live` | Live (streaming) mode |

**Compound tiers**

| Value | Description |
|-------|-------------|
| `batch_long_context` | Batch + long context |
| `priority_long_context` | Priority + long context |
| `flex_long_context` | Flex + long context |
| `cached_long_context` | Cached input + long context |
| `cached_batch` | Cached input + batch |
| `cached_flex` | Cached input + flex tier |
| `cached_priority` | Cached input + priority tier |
| `cached_batch_long_context` | Cached input + batch + long context |
| `cached_flex_long_context` | Cached input + flex + long context |
| `cached_priority_long_context` | Cached input + priority + long context |
| `thinking_batch` | Thinking + batch |
| `thinking_flex` | Thinking + flex tier |
| `thinking_priority` | Thinking + priority tier |
| `thinking_long_context` | Thinking + long context |
| `thinking_batch_long_context` | Thinking + batch + long context |
| `thinking_flex_long_context` | Thinking + flex + long context |
| `thinking_priority_long_context` | Thinking + priority + long context |

## Character Pricing

| Metric | Labels | Description |
|--------|--------|-------------|
| `cloudcost_gcp_vertex_usd_per_1k_characters` | `project_id`, `region`, `gen_ai_request_model`, `family`, `gen_ai_token_type`, `price_tier` | Cost in USD per 1k characters, by `gen_ai_token_type`, for models billed by character (e.g. translation models). |

### Labels

| Label | Values | Description |
|-------|--------|-------------|
| `project_id` | e.g. `my-gcp-project` | Billing-scope project (see Token Pricing) |
| `region` | e.g. `global` | GCP region |
| `gen_ai_request_model` | e.g. `translation-llm` | Model name, normalised to lowercase with spaces replaced by hyphens |
| `family` | `google`, `unknown` | Model provider family; `unknown` for unrecognised model prefixes |
| `gen_ai_token_type` | `input`, `output` | Whether the price is for input or output characters |
| `price_tier` | `on_demand` | GCP Translation is flat-rate; no batch tier exists in the billing API |

## Reranking

| Metric | Labels | Description |
|--------|--------|-------------|
| `cloudcost_gcp_vertex_search_unit_usd_per_1k_search_units` | `project_id`, `region`, `gen_ai_request_model`, `family`, `price_tier` | Vertex AI reranking cost in USD per 1k ranking requests |

Reranking is priced from the `Vertex AI Search: Ranking` SKU (the Semantic Ranker the Assistant uses), fetched from the Cloud Discovery Engine billing service. If that service is unavailable at startup, reranking metrics are omitted and a warning is logged. Agent Builder (`AI Dev Tools:`) SKUs share this service but price a different product and are skipped.

### Labels

| Label | Values | Description |
|-------|--------|-------------|
| `project_id` | e.g. `my-gcp-project` | Billing-scope project (see Token Pricing) |
| `region` | e.g. `global` | GCP region |
| `gen_ai_request_model` | `semantic-ranker` | GCP catalogs ranking as a service SKU with no model name; this recognizable slug stands in |
| `family` | `google` | Model provider family; the Ranking API is a Google service |
| `price_tier` | `on_demand` | The Ranking API is a single flat rate; the label is constant and mirrors the other Vertex metrics. Analogous to Bedrock's search-unit metric, this carries no `gen_ai_token_type` |

## Notes

Pricing data is fetched from the GCP Billing API at startup and refreshed every 24 hours. SKU descriptions are matched using regular expressions; unknown SKUs are skipped. Verify SKU description patterns against the live Billing API when adding new models or machine types.
