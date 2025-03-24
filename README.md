# Cloud Cost Exporter

[![Go Reference](https://pkg.go.dev/badge/github.com/grafana/cloudcost-exporter.svg)](https://pkg.go.dev/github.com/grafana/cloudcost-exporter)

Cloud Cost exporter is a tool designed to collect cost data from cloud providers and export the data in Prometheus format.
The cost data can then be combined with usage data from tools such as stackdriver, yace, and promitor to measure the spend of resources at a granular level.

## Goals

The goal of this project is to provide a consistent interface for collecting the rate of cost data from multiple cloud providers and exporting the data in Prometheus format.
There was a need to track the costs of both kubernetes and non-kubernetes resources across multiple cloud providers at a per minute interval.
Billing data for each cloud provider takes hours to days for it to be fully accurate, and we needed a way of having a more real-time view of costs.

Primary goals:
- Track the rate(IE, $/cpu/hr) for resources across
- Export the rate in Prometheus format
- Support the major cloud providers(AWS, GCP, Azure)

Non Goals:
- Billing level accuracy
- Measure the spend of resources
- Take into account CUDs/Discounts/Reservations pricing information

## Supported Cloud Providers

- AWS
- GCP
- Azure

## Installation

Each tagged version of the Cloud Cost Exporter will publish a Docker image to https://hub.docker.com/r/grafana/cloudcost-exporter and a Helm chart.

### Local usage

The image can be used to deploy Cloud Cost Exporter to a Kubernetes cluster or to run it locally.

#### Use the image

Cloud Cost Exporter has an opinionated way of authenticating against each cloud provider:

| Provider | Notes |
|-|-|
| GCP | Depends on [default credentials](https://cloud.google.com/docs/authentication/application-default-credentials) |
| AWS | Uses profile names from your [credentials file](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-files.html) or `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_REGION` env variables |
| Azure | Uses the [default azure credential chain](https://learn.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication?tabs=bash), e.g. enviornment variables: `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, and `AZURE_CLIENT_SECRET` |

### Deployment to Kubernetes

When running in a Kubernetes cluster, it is recommended to create an IAM role for a Service Account (IRSA) with the necessary permissions for the cloud provider.

Documentation about the necessary permission for AWS can be found [here](./docs/deploying/aws/README.md#1-setup-the-iam-role). Documentation for GCP and Azure are under development.

#### Use the Helm chart

When deploying to Kubernetes, it is recommended to use the Helm chart, which can be found here: https://github.com/grafana/helm-charts/tree/main/charts/cloudcost-exporter

Additional Helm chart configuration for AWS can be found [here](./docs/deploying/aws/README.md#2-configure-the-helm-chart)

## Metrics

Check out the follow docs for metrics:
- [provider level](docs/metrics/providers.md)
- gcp
  - [gke](docs/metrics/gcp/gke.md)
  - [gcs](docs/metrics/gcp/gcs.md)
- aws
  - [s3](docs/metrics/aws/s3.md)
  - [ec2](docs/metrics/aws/ec2.md)
- azure
  - [aks](docs/metrics/azure/aks.md)

## Maturity

This project is in the early stages of development and is subject to change.
Grafana Labs builds and maintains this project as part of our commitment to the open-source community, but we do not provide support for it.
In its current state, the exporter exports rates for resources and not the total spend.

For a better understanding of how we view measuring costs, view a talk given at [KubeCon NA 2023](https://youtu.be/8eiLXtL3oLk?si=wm-43ZQ9Fr51wS4a&t=1)

In the future, we intend to opensource recording rules we use internally to measure the spend of resources.

## Contributing

Grafana Labs is always looking to support new contributors!
Please take a look at our [contributing guide](CONTRIBUTING.md) for more information on how to get started.
