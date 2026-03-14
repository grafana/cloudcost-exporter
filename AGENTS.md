# Cloud Cost Exporter

Prometheus exporter that collects cost and pricing data from AWS, GCP, and Azure in real-time. Written in Go.

## Project Structure

```
cmd/exporter/      # Main entrypoint (exporter.go)
cmd/dashboards/    # Dashboard generation
pkg/aws/           # AWS provider + collectors
pkg/azure/         # Azure provider + collectors
pkg/google/        # GCP provider + collectors
pkg/gatherer/      # Metric gathering logic
pkg/metrics/       # Shared metrics definitions
pkg/provider/      # Provider interface
pkg/utils/         # Shared utilities
charts/            # Helm chart
cloudcost-exporter-dashboards/  # Grafana dashboards (grafana-foundation-sdk)
docs/              # Guides and references
```

Each cloud provider has an entrypoint at `pkg/{aws,azure,google}/{provider}.go` that initializes the provider and its collectors. A collector handles cost data for a single service and emits Prometheus metrics.

## Build Commands

```bash
make build-binary      # Compile the Go binary
make build-image       # Build Docker image
make build             # lint + generate + build-binary + build-image
make test              # Full test suite (lint, generate, build, go test)
make lint              # Run golangci-lint
make generate          # Run go generate (generates mocks)
make build-dashboards  # Generate Grafana dashboards via grafana-foundation-sdk
```

## Running Locally

Authenticate with the cloud provider first:

```bash
# AWS
aws sso login --profile $AWS_PROFILE

# GCP
gcloud auth application-default login

# Azure
az login
```

Then run:

```bash
go run cmd/exporter/exporter.go --help

go run cmd/exporter/exporter.go -provider gcp -project-id=$GCP_PROJECT_ID
go run cmd/exporter/exporter.go -provider aws -aws.profile $AWS_PROFILE
go run cmd/exporter/exporter.go -provider azure -azure.subscription-id $AZ_SUBSCRIPTION_ID
```

> **Warning:** AWS Cost Explorer charges $0.01 per request. Default settings limit to 1 request/hour, but each restart triggers a new request.

## Guidelines

### Safe Operations (execute without asking)

- `go test ./...`, `go build`, `go vet`
- `make lint`, `make generate`, `make build-binary`, `make build-dashboards`
- `gh pr view`, `gh api --method GET`
- Read-only cloud CLI commands

### Destructive Operations (ALWAYS ask for approval first)

- `docker push`, `make push`, `make push-dev`
- `gh pr merge`, `gh api --method POST/PUT/DELETE`
- Any release label changes on PRs

## Required Tools

- Go (1.21+)
- Docker
- `golangci-lint`
- `mockery` and `mockgen` (for `make generate` — install per their docs, not via `go install`)
- Cloud CLIs: `gcloud`, `aws`, `az`

## Releases

Automated via PR labels. Add `release:major`, `release:minor`, or `release:patch` when merging a PR to trigger a release.

## Workflow Requirements

- All changes via Pull Requests
- CI validates lint, tests, and builds
- **Do not mention AI tools (Claude, Copilot, etc.) in commit messages or PR descriptions**
