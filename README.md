# Cloud Cost Exporter

Cloud Cost exporter is a designed to collect cost data from cloud providers and export the data in Prometheus format.
The cost data can then be combined with usage data from tools such as stackdriver, yace, and promitor to measure the spend of resources at a granular level.

> [!WARNING]
> This project is in the early stages of development and is subject to change.
> Grafana Labs builds and maintains this project as part of our commitment to support the open-source community, but we do not provide support for it.
> In its current state, the exporter exports rates for resources and not the actual cost.
> We intend to opensource recording rules we use internally to measure the cost of resources.
> For a better understanding of how we view measuring costs, view a talk given at [KubeCon NA 2023](https://www.youtube.com/watch?v=8eiLXtL3oLk&t=1364s)

## Goals

The goal of this project is to provide a consistent interface for collecting the rate of cost data from multiple cloud providers and exporting the data in Prometheus format.
There was a need to track the costs of both kubernetes and non-kubernetes resources across multiple cloud providers at a per minute interval.

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

Azure support is planned but not yet implemented.

## Usage

Each tagged version will publish a docker image to https://hub.docker.com/r/grafana/cloudcost-exporter with the version tag.
The image can be run locally or deployed to a kubernetes cluster.
Cloud Cost Exporter has an opinionated way of authenticating against each cloud provider.

| Provider | Notes |
|-|-|
| GCP | Depends on [default credentials](https://cloud.google.com/docs/authentication/application-default-credentials) |
| AWS | Uses profile names from your [credentials file](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-files.html) or `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_REGION` env variables |

When running in a kubernetes cluster, it is recommended to use a service account with the necessary permissions for the cloud provider.
- [ ] TODO: Document the necessary permissions for each cloud provider.

There is no helm chart available at this time, but one is planned.

Check out the follow docs for metrics:
- [provider level](docs/metrics/providers.md)
- gcp
  - [compute](docs/metrics/gcp/compute.md)
  - [gke](docs/metrics/gcp/gke.md)
  - [gcs](docs/metrics/gcp/gcs.md)
- aws
  - [s3](docs/metrics/aws/s3.md)

## Contributing

Grafana Labs is always looking to support new contributors!
Please take a look at our [contributing guide](CONTRIBUTING.md) for more information on how to get started.
