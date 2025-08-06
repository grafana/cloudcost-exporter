# Developer Guide

This guide will help you get started with the development environment and how to contribute to the project.

## External Dependencies

There are two tools that we use to generate mocks for testing that need to be installed independently:
1. [mockery](https://vektra.github.io/mockery/latest/installation/): See the note about _not_ using go tools to install
2. [mockgen](https://github.com/uber-go/mock?tab=readme-ov-file#installation)

Use the latest version available for both. 
The tools are used by `make generate-mocks` in the [Makefile](https://github.com/grafana/cloudcost-exporter/blob/66b83baacf9ad4408f0ad7c7b1738ac3b2c179b2/Makefile#L28)

## Running Locally

Prior to running the exporter, you will need to ensure you have the appropriate credentials for the cloud provider you are trying to export data for.
- AWS
  - `aws sso login --profile $AWS_PROFILE`
- GCP
  - `gcloud auth application-default login` 
- Azure
  - `az login`


> [!WARNING]
> AWS costexplorer costs $0.01 _per_ request! 
> The default settings will keep it to 1 request per hour.
> Each restart of the exporter will trigger a new request. 

```shell
# Usage
go run cmd/exporter/exporter.go --help

# GCP 
go run cmd/exporter/exporter.go -provider gcp -project-id=$GCP_PROJECT_ID

# GCP - with custom bucket projects

go run cmd/exporter/exporter.go -provider gcp -project-id=$GCP_PROJECT_ID -gcp.projects=$GPC_PROJECT_ID -gcp.projects=$GPC_PROJECT_ID

# AWS - Prod
go run cmd/exporter/exporter.go -provider aws -aws.profile $AWS_PROFILE

# Azure
go run cmd/exporter/exporter.go -provider azure -azure.subscription-id $AZ_SUBSCRIPTION_ID
```

## Project Structure

The main entrypoint for the cloudcost exporter is [exporter.go](../../cmd/exporter/exporter.go). 
When running the application, there is a flag that is used to determine which cloud service provider(csp) to use. 
`cloudcost-exporter` currently supports three csp's:
- `gcp`
- `aws`
- `azure`

Each csp has an entrypoint in `./pkg/{aws,azure,gcp}/{aws,azure,gcp}.go` that is responsible for initializing the provider and a set of collectors.
A collector is a modules for a single CSP that collects cost data for a specific service and emits the data as a set of Prometheus metrics.
A provider can run multiple collectors at once. 





