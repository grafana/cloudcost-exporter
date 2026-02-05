# Cloud Cost Exporter

[![Go Reference](https://pkg.go.dev/badge/github.com/grafana/cloudcost-exporter.svg)](https://pkg.go.dev/github.com/grafana/cloudcost-exporter)
[![License](https://img.shields.io/github/license/grafana/cloudcost-exporter)](https://github.com/grafana/cloudcost-exporter/blob/main/LICENSE)
[![Docker Pulls](https://img.shields.io/docker/pulls/grafana/cloudcost-exporter)](https://hub.docker.com/r/grafana/cloudcost-exporter)

Cloud Cost Exporter is a Prometheus exporter that collects cost and pricing data from cloud providers (AWS, GCP, Azure) in real-time.

**Quick Links:**
- [Docker Hub](https://hub.docker.com/r/grafana/cloudcost-exporter) - Official Docker images
- [Helm Chart](https://github.com/grafana/helm-charts/tree/main/charts/cloudcost-exporter) - Kubernetes deployment
- [Documentation](./docs) - Detailed guides and references
- [KubeCon Talk](https://youtu.be/8eiLXtL3oLk?si=wm-43ZQ9Fr51wS4a&t=1) - Measuring cloud costs at scale

## Overview

Cloud Cost Exporter provides near real-time cost visibility by exporting resource pricing rates as Prometheus metrics. The cost data can be combined with usage metrics from tools like [Stackdriver](https://cloud.google.com/stackdriver), [YACE](https://github.com/nerdswords/yet-another-cloudwatch-exporter), [Grafana AWS CloudWatch Metric Streams](https://grafana.com/docs/grafana-cloud/monitor-infrastructure/monitor-cloud-provider/aws/cloudwatch-metrics/metric-streams/config-cw-metric-streams-cloudformation/), and [Promitor](https://promitor.io/) to calculate granular spending.

Cloud provider billing data takes hours to days to become fully accurate. This exporter bridges that gap by providing per-minute cost rate visibility for both Kubernetes and non-Kubernetes resources across multiple cloud providers.

### Primary Goals

- **Multi-cloud support** - Collect cost rates from AWS, GCP, and Azure through a consistent interface
- **Real-time rates** - Export per-minute pricing rates (e.g., $/cpu/hr) for cloud resources
- **Prometheus native** - Export metrics in Prometheus format for easy integration with monitoring stacks

### Non-Goals

- **Billing accuracy** - This is not a replacement for official cloud billing reports
- **Spend calculation** - Exports rates, not total spend (use recording rules to calculate spend)
- **Discount modeling** - Does not account for CUDs, reservations, or negotiated discounts

## Project Maturity

This project is in active development and is used in production by Grafana Labs. We build and maintain this project as part of our commitment to the open-source community.

**Current state:**
- Exports cost rates for cloud resources across AWS, GCP, and Azure
- Production-ready for rate collection use cases
- Does not include built-in spend aggregation (use Prometheus recording rules)

## Supported Cloud Providers

- **AWS** - Amazon Web Services
- **GCP** - Google Cloud Platform
- **Azure** - Microsoft Azure

## Prerequisites

- **Go 1.24+** (for building from source)
- **Docker** (for container deployments)
- **Kubernetes cluster** (for Helm deployments)
- **Cloud provider credentials** with read-only access to billing/pricing APIs (see [Cloud Provider Authentication](#cloud-provider-authentication))

## Installation

Each tagged release publishes:
- Docker image to [Docker Hub](https://hub.docker.com/r/grafana/cloudcost-exporter)
- Helm chart to [Grafana Helm Charts](https://github.com/grafana/helm-charts/tree/main/charts/cloudcost-exporter)

### Cloud Provider Authentication

Cloud Cost Exporter uses each provider's default authentication mechanisms:

| Provider | Authentication Method | Documentation |
|----------|----------------------|---------------|
| **AWS** | AWS credentials file, environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`), or IAM role | [AWS credentials](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-files.html) |
| **GCP** | Application Default Credentials (ADC) | [GCP ADC](https://cloud.google.com/docs/authentication/application-default-credentials) |
| **Azure** | DefaultAzureCredential chain (environment variables: `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_CLIENT_SECRET`) | [Azure authentication](https://learn.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication?tabs=bash) |

### Quick Start with Docker

```bash
# Run with Docker (example for AWS)
docker run -d \
  -p 8080:8080 \
  -e AWS_ACCESS_KEY_ID=your-key \
  -e AWS_SECRET_ACCESS_KEY=your-secret \
  -e AWS_REGION=us-east-1 \
  grafana/cloudcost-exporter:latest
```

Metrics will be available at `http://localhost:8080/metrics`

### Kubernetes Deployment

For Kubernetes deployments, use the official [Helm chart](https://github.com/grafana/helm-charts/tree/main/charts/cloudcost-exporter). See the chart repository for configuration options, values, and examples.

```bash
# Add Grafana Helm repository
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update

# Install cloudcost-exporter
helm install cloudcost-exporter grafana/cloudcost-exporter
```

**Required:** Use IAM Roles for Service Accounts (IRSA) for secure credential management:
- **AWS:** [IRSA Setup Guide](./docs/deploying/aws/README.md#1-setup-the-iam-role) and [Helm Configuration](./docs/deploying/aws/README.md#2-configure-the-helm-chart)
- **GCP:** Workload Identity (documentation in development - [contribute here](https://github.com/grafana/cloudcost-exporter/issues/456))
- **Azure:** Workload Identity (documentation in development - [contribute here](https://github.com/grafana/cloudcost-exporter/issues/458))

## Metrics

Cloud Cost Exporter exposes Prometheus metrics for cost rates of cloud resources. Each service exports metrics specific to that resource type - see the service documentation below for metric details.

### Supported Services

#### AWS Services

- **[EC2](docs/metrics/aws/ec2.md)** - Elastic Compute Cloud instances (spot and on-demand pricing)
- **[S3](docs/metrics/aws/s3.md)** - Simple Storage Service buckets
- **[RDS](docs/metrics/aws/rds.md)** - Relational Database Service instances
- **[ELB](docs/metrics/aws/elb.md)** - Elastic Load Balancers (ALB, NLB)
- **[NAT Gateway](docs/metrics/aws/natgateway.md)** - Network Address Translation gateways
- **[VPC](docs/metrics/aws/vpc.md)** - VPC endpoints and services

#### GCP Services

- **[GKE](docs/metrics/gcp/gke.md)** - Google Kubernetes Engine clusters
- **[GCS](docs/metrics/gcp/gcs.md)** - Google Cloud Storage buckets
- **[Cloud SQL](docs/metrics/gcp/cloudsql.md)** - Managed database instances
- **[CLB](docs/metrics/gcp/clb.md)** - Cloud Load Balancers via forwarding rules
- **[VPC](docs/metrics/gcp/vpc.md)** - Cloud NAT Gateway, VPN Gateway, Private Service Connect

#### Azure Services

- **[AKS](docs/metrics/azure/aks.md)** - Azure Kubernetes Service VM instances and managed disks

## Development

### Prerequisites

- Go 1.24 or higher
- Docker
- golangci-lint

### Building from Source

```bash
# Clone the repository
git clone https://github.com/grafana/cloudcost-exporter.git
cd cloudcost-exporter

# Build the binary
make build-binary

# Run the exporter
./cloudcost-exporter
```

### Running Tests

```bash
# Run tests with linting and code generation
make test

# Run only unit tests
go test -v ./...

# Run linter
make lint
```

### Building Docker Image

```bash
# Build Docker image
make build-image

# Build and push (requires appropriate permissions)
make push
```

### Code Generation

The project uses code generation for dashboards and other resources:

```bash
# Run code generation
make generate

# Build Grafana dashboards
make build-dashboards
```

For more detailed development information, see the [developer guide](./docs/contribute/developer-guide.md).

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Cloud Cost Exporter is licensed under the [Apache License 2.0](LICENSE).
