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

## Usage

### Deployment

Cloud Cost Exporter can be used locally or on Kubernetes.

Checkout the [deployment docs](./docs/deploying/deploying.md)
for how to:
* Authenticate with CSPs
* Deploy it via the image alone or the Helm chart

### Metrics

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
