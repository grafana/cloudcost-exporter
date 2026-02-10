# Cloud Cost Exporter

[![Go Reference](https://pkg.go.dev/badge/github.com/grafana/cloudcost-exporter.svg)](https://pkg.go.dev/github.com/grafana/cloudcost-exporter)
[![License](https://img.shields.io/github/license/grafana/cloudcost-exporter)](https://github.com/grafana/cloudcost-exporter/blob/main/LICENSE)
[![Docker Pulls](https://img.shields.io/docker/pulls/grafana/cloudcost-exporter)](https://hub.docker.com/r/grafana/cloudcost-exporter)

Cloud Cost Exporter is a Prometheus exporter that collects cost and pricing data from cloud providers (AWS, GCP, Azure) in real-time.

**Quick Links:**
- [Docker Hub](https://hub.docker.com/r/grafana/cloudcost-exporter) - Official Docker images
- [Helm Chart](https://github.com/grafana/helm-charts/tree/main/charts/cloudcost-exporter) - Helm Chart for Kubernetes deployment
- [Documentation](./docs/README.md) - Detailed guides and references
- [KubeCon Talk](https://youtu.be/8eiLXtL3oLk?si=wm-43ZQ9Fr51wS4a&t=1) on monitoring cloud costs at scale

## Overview

Cloud Cost Exporter provides near real-time cost visibility by exporting resource pricing rates as Prometheus metrics.

The cost data can be combined with usage metrics from tools like [Stackdriver](https://cloud.google.com/stackdriver), [YACE](https://github.com/nerdswords/yet-another-cloudwatch-exporter), [Grafana AWS CloudWatch Metric Streams](https://grafana.com/docs/grafana-cloud/monitor-infrastructure/monitor-cloud-provider/aws/cloudwatch-metrics/metric-streams/config-cw-metric-streams-cloudformation/), and [Promitor](https://promitor.io/) to calculate granular spending.

Cloud provider billing data takes hours to days to become fully accurate. This exporter bridges that gap by providing per-minute cost rate visibility for both Kubernetes and non-Kubernetes resources across multiple cloud providers.

### Primary Goals

- **Multi-cloud support** - Collect cost rates from AWS, GCP, and Azure through a consistent interface
- **Real-time rates** - Export per-minute pricing rates (e.g., $/cpu/hr) for cloud resources
- **Prometheus native** - Export metrics in Prometheus format for easy integration with monitoring stacks

### Non-Goals

- **Billing accuracy** - This is not a replacement for official cloud billing reports
- **Spend calculation** - Exports rates, not total spend (use Prometheus queries & recording rules to calculate spend)

## Project Maturity

This project is in active development and is subject to change. Grafana Labs builds and maintains this project as part of our commitment to the open source community but we do not provide support for it. The exporter exports rates for resources and not the total spend.

## Supported Cloud Providers

- **AWS** - Amazon Web Services
- **GCP** - Google Cloud Platform
- **Azure** - Microsoft Azure

## Installation

Cloud Cost Exporter can be used locally using its [image](#1-local-usage-via-image) or deployed via a [Helm Chart](#2-kubernetes-deployment-via-helm-chart).

### 1. Local Usage via Image

To deploy locally using its [image](https://hub.docker.com/r/grafana/cloudcost-exporter), Cloud Cost Exporter uses each provider's default authentication mechanisms:

| Provider | Authentication Method | Documentation |
|----------|----------------------|---------------|
| **AWS** | AWS credentials file, environment variables (`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`), or IAM role | [AWS credentials](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-files.html) |
| **GCP** | Application Default Credentials (ADC) | [GCP ADC](https://cloud.google.com/docs/authentication/application-default-credentials) |
| **Azure** | DefaultAzureCredential chain (environment variables: `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, `AZURE_CLIENT_SECRET`) | [Azure authentication](https://learn.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication?tabs=bash) |

After authenticating with a cloud provider, follow these steps to do a quick start using Docker:

```bash
# Run with Docker (example for AWS)
docker run -d \
  -p 8080:8080 \
  -e AWS_ACCESS_KEY_ID=your-key \
  -e AWS_SECRET_ACCESS_KEY=your-secret \
  -e AWS_REGION=us-east-1 \
  grafana/cloudcost-exporter:latest
```

Metrics will be available to be scraped at `http://localhost:8080/metrics`

### 2. Kubernetes Deployment via Helm chart

For Kubernetes deployments, use the official [Helm chart](https://github.com/grafana/helm-charts/tree/main/charts/cloudcost-exporter). See the chart repository for configuration options, values, and examples.

First, setup an IAM Role for Service Accounts (IRSA) for secure credential management:
- **AWS:** [IRSA Setup Guide](./docs/deploying/aws/README.md#1-setup-the-iam-role) and [Helm Configuration](./docs/deploying/aws/README.md#2-configure-the-helm-chart)
- **GCP:** Workload Identity (documentation in development - [contribute here](https://github.com/grafana/cloudcost-exporter/issues/456))
- **Azure:** Workload Identity (documentation in development - [contribute here](https://github.com/grafana/cloudcost-exporter/issues/458))

Then, install Cloud Cost Exporter via its Helm chart:

```bash
# Add Grafana Helm repository
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update

# Install cloudcost-exporter
helm install cloudcost-exporter grafana/cloudcost-exporter
```

**Chart source**: [charts/cloudcost-exporter](./charts/cloudcost-exporter/)
**Chart documentation**: [charts/cloudcost-exporter/README.md](./charts/cloudcost-exporter/README.md)

## Supported Services

Cloud Cost Exporter exposes Prometheus metrics for cost rates of cloud resources. Each service exports metrics specific to that resource type - see the [metrics documentation](./docs/metrics/README.md) for details on supported services.

## Contributing

We welcome contributions. See the [contributing guide](docs/contribute/README.md).

## License

Cloud Cost Exporter is licensed under the [Apache License 2.0](LICENSE).
