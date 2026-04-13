# Cloud Cost Exporter

Prometheus exporter that collects pricing rates from AWS, GCP, and Azure. Exports rates per instance of a given resource (`$/core/hr`, `$/GiB/hr`) as Prometheus metrics. Rates, not total spend. Go 1.25+.

Agent self-check: If you find information in this AGENTS.md that contradicts the code, flag it to the user.

This is a public repository. Never reference private Grafana repos in issues, PRs, or code comments.

## Architecture

Two interfaces in `pkg/provider/provider.go`:

- **Provider** (cloud platform): implements `prometheus.Collector` + `RegisterCollectors()`. Created via `aws.New()`, `google.New()`, `azure.New()`.
- **Collector** (cloud service): implements `Register()`, `Collect(ctx, ch)`, `Describe()`, `Name()`. Each provider wires collectors from the `-{provider}.services` flag.

On scrape, the provider fans out `Collect()` to all collectors concurrently. Each provider file defines its own concurrency strategy (see `pkg/{aws,google,azure}/*.go`). `gatherer.CollectWithGatherer()` wraps each call with duration and error tracking.

```
cmd/exporter/exporter.go             # Entrypoint: flags, provider selection, HTTP server
  config/config.go                   # Config structs, StringSliceFlag type
pkg/provider/provider.go             # Provider, Collector, Registry interfaces
pkg/aws/aws.go                       # AWS: S3, EC2, RDS, NATGATEWAY, ELB, VPC
pkg/google/gcp.go                    # GCP: GCS, GKE, CLB, SQL, VPC
pkg/azure/azure.go                   # Azure: AKS, blob
pkg/gatherer/gatherer.go             # Wraps Collect(): duration, errors, metadata metrics
pkg/utils/consts.go                  # Shared metric suffixes, HoursInMonth, GenerateDesc()
cmd/dashboards/main.go               # Dashboard generation (grafana-foundation-sdk)
cloudcost-exporter-dashboards/       # Generated output. Never edit by hand.
```

### Metric naming

Pattern: `cloudcost_{provider}_{service}_{description}_{unit}`

- `MetricPrefix = "cloudcost"` for cost metrics. `ExporterName = "cloudcost_exporter"` for operational metrics. Both in `main.go`. Mixing them produces wrong names.
- Subsystems: `aws_s3`, `gcp_gke`, `azure_aks`
- Standard suffixes in `pkg/utils/consts.go`: `instance_cpu_usd_per_core_hour`, `instance_memory_usd_per_gib_hour`, `instance_total_usd_per_hour`, `persistent_volume_usd_per_hour`
- Build with `prometheus.BuildFQName(prefix, subsystem, suffix)`

## Build and test

```bash
make build-binary      # Compile binary (CGO_ENABLED=0)
make build-image       # Docker image (multi-stage, scratch base)
make build             # lint + generate + build-binary + build-image
make test              # lint + generate + build-dashboards + go test
make lint              # golangci-lint v2
make generate          # go generate (mocks via mockgen/mockery)
make build-dashboards  # Grafana dashboards
```

CI runs on push to `main` and PRs: build, lint, test, dashboard drift check.

Rule: Never push to `main`.

### Running locally

```bash
go run cmd/exporter/exporter.go -provider gcp -project-id=$GCP_PROJECT_ID -gcp.services GKE,GCS
go run cmd/exporter/exporter.go -provider aws -aws.profile $AWS_PROFILE -aws.services EC2,S3
go run cmd/exporter/exporter.go -provider azure -azure.subscription-id $AZ_SUBSCRIPTION_ID -azure.services AKS
go run cmd/exporter/exporter.go -provider azure -azure.subscription-id $AZ_SUBSCRIPTION_ID -azure.services blob
```

### Adding a collector

See `docs/contribute/creating-a-new-module.md`. Reference implementations per provider:
- AWS: `pkg/aws/ec2/` (worker pool pattern, spot/on-demand price tiers, instance + volume metrics)
- GCP: `pkg/google/gke/` (errgroup concurrency, multi-project, disk deduplication)
- Azure: `pkg/azure/aks/` (ticker-based store refresh, dual storage metrics per-GiB + total)

New collectors, and changes to collectors, must **always** be documented in `docs/metrics/<provider>/`.

### Testing

Run `make generate` before writing tests. See reference implementations above for patterns.

### Code patterns

- **Reuse constants**: Use existing suffix constants from `pkg/utils/consts.go`.
- **Provider parity**: Match structure, naming, and config initialization patterns of existing services across providers.
- **Dead code**: Remove unused functions, commented-out tests, and unused imports.
- **Code generators**: Keep generation tools (e.g., region generators, SKU generators) in dedicated packages (e.g., `pkg/azure/generate/`), separate from service logic.

## Caveats

- **`ExporterName` vs `MetricPrefix`**: `ExporterName` (`cloudcost_exporter`) is for operational metrics. `MetricPrefix` (`cloudcost`) is for cost metrics.
- **Dashboard drift**: Edit `cmd/dashboards/`, run `make build-dashboards`, commit generated output. CI fails on mismatch.
- **`main.go` exists for mockery**: Root `main.go` exports constants so mockery finds the package. Entrypoint is `cmd/exporter/exporter.go`.
- **Silent collector init failures**: Provider skips failed collectors and continues by design so that a collector failing does not fail the whole app. Check startup logs.

## Documentation Writing Style

Active voice. Cut every word that serves no function. No meta-commentary.

- Drop redundant adverbs: `fully accurate` → `accurate`
- Drop wordy phrases: `we need to remove the availability zone` → `remove the availability zone`
- Prefer active verbs: `is able to pull metrics` → `can pull metrics`, `unable to create` → `failed to create`
- Write timeless documentation: Describe things as they are, not how they changed or will change.

## Operations

### Safe to execute

- `go test ./...`, `go build ./...`, `go vet ./...`
- `make lint`, `make generate`, `make build-binary`, `make build-dashboards`
- `gh pr view`, `gh api --method GET`

### Requires user approval

- `docker push`, `make push`, `make push-dev`
- Release label changes on PRs
- Destructive git operations (`push --force`, `reset --hard`)

## Releases

Automated via PR labels on merge to `main`. Ask user whether to add release label when drafting PR. Add exactly one: `release:major`, `release:minor`, or `release:patch`. Multiple labels fail the workflow.
