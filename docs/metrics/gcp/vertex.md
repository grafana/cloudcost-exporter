# Vertex AI Metrics

Metrics exported for the GCP Vertex AI service.

## Token Pricing

| Metric | Labels | Description |
|--------|--------|-------------|
| `cloudcost_gcp_vertex_token_input_usd_per_1k_tokens` | `model_id`, `family`, `region` | Input token/character cost in USD per 1k tokens (covers Gemini, Claude, and embedding models) |
| `cloudcost_gcp_vertex_token_output_usd_per_1k_tokens` | `model_id`, `family`, `region` | Output token/character cost in USD per 1k tokens (covers Gemini and Claude models) |

### Labels

| Label | Values | Description |
|-------|--------|-------------|
| `model_id` | e.g. `gemini-1.5-flash`, `claude-3.5-sonnet`, `gemini-embedding-001` | Model name, normalised to lowercase with spaces replaced by hyphens |
| `family` | `google`, `anthropic`, `unknown` | Model provider family; `unknown` for unrecognised model prefixes |
| `region` | e.g. `us-central1` | GCP region |

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
