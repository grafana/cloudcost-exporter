# Vertex AI Metrics

Metrics exported for the GCP Vertex AI service.

## Token Pricing

| Metric | Labels | Description |
|--------|--------|-------------|
| `cloudcost_gcp_vertex_input_usd_per_1k_tokens` | `model_id`, `family`, `region`, `price_tier` | Input cost in USD per 1k tokens, for models billed by token. Character-billed models use the character metric. |
| `cloudcost_gcp_vertex_output_usd_per_1k_tokens` | `model_id`, `family`, `region`, `price_tier` | Output cost in USD per 1k tokens, for models billed by token. Character-billed models use the character metric. |

### Labels

| Label | Values | Description |
|-------|--------|-------------|
| `model_id` | e.g. `gemini-1.5-flash`, `gemma-4`, `llama-4-maverick` | Model name, normalised to lowercase with spaces replaced by hyphens |
| `family` | `google`, `meta`, `alibaba`, `deepseek`, `minimax`, `moonshot`, `unknown` | Model provider family; `unknown` for unrecognised model prefixes |
| `region` | e.g. `us-central1` | GCP region |
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
| `cloudcost_gcp_vertex_input_usd_per_1k_characters` | `model_id`, `family`, `region`, `price_tier` | Input cost in USD per 1k characters, for models billed by character (e.g. translation models). |
| `cloudcost_gcp_vertex_output_usd_per_1k_characters` | `model_id`, `family`, `region`, `price_tier` | Output cost in USD per 1k characters, for models billed by character (e.g. translation models). |

### Labels

| Label | Values | Description |
|-------|--------|-------------|
| `model_id` | e.g. `translation-llm` | Model name, normalised to lowercase with spaces replaced by hyphens |
| `family` | `google`, `unknown` | Model provider family; `unknown` for unrecognised model prefixes |
| `region` | e.g. `global` | GCP region |
| `price_tier` | `on_demand`, `batch` | Running mode derived from the GCP SKU description |

## Compute Pricing

| Metric | Labels | Description |
|--------|--------|-------------|
| `cloudcost_gcp_vertex_instance_total_usd_per_hour` | `machine_type`, `use_case`, `region`, `price_tier` | Vertex AI custom training and prediction node cost in USD per hour |

### Labels

| Label | Values | Description |
|-------|--------|-------------|
| `machine_type` | e.g. `n1-standard-4` | Machine type used for the compute node |
| `use_case` | `training`, `prediction` | Whether the node is used for custom training or online prediction |
| `region` | e.g. `us-central1` | GCP region |
| `price_tier` | `on_demand`, `spot` | Pricing tier; spot metrics are only emitted when a spot price exists |

## Reranking

| Metric | Labels | Description |
|--------|--------|-------------|
| `cloudcost_gcp_vertex_search_unit_usd_per_1k_search_units` | `model_id`, `family`, `region` | Vertex AI reranking cost in USD per 1k ranking requests |

Reranking SKUs are fetched from the Cloud Discovery Engine billing service. If that service is unavailable at startup, reranking metrics are omitted and a warning is logged.

### Labels

| Label | Values | Description |
|-------|--------|-------------|
| `model_id` | e.g. `semantic-ranker-api` | Ranker model name, normalised to lowercase with spaces replaced by hyphens |
| `family` | `google` | Model provider family; Discovery Engine reranking models are Google's |
| `region` | e.g. `global` | GCP region |

## Notes

Pricing data is fetched from the GCP Billing API at startup and refreshed every 24 hours. SKU descriptions are matched using regular expressions; unknown SKUs are skipped. Verify SKU description patterns against the live Billing API when adding new models or machine types.
