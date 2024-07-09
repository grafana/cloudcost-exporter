# Developer Guide

This guide will help you get started with the development environment and how to contribute to the project.

## Running Locally

Prior to running the exporter, you will need to ensure you have the appropriate credentials for the cloud provider you are trying to export data for.
- Setup AWS: https://github.com/grafana/deployment_tools/blob/fd5dbe933614259d55c2fac0b7e4d3bf284d5457/docs/infrastructure/aws.md#L121
    - `aws sso login --profile infra-prod`
- GCP: `gcloud auth application-default login` should be enough

> [!WARNING]
> :fire: AWS costexplorer costs $0.01 _per_ request! The default settings _should_ keep it to 1 request per hour :fire:
> :fire: Keep an eye on aws_costexplorer_requests_total metric to ensure you are not exceeding your budget :fire:

```shell
# Usage
go run cmd/exporter/exporter.go --help

# GCP 
go run cmd/exporter/exporter.go -provider gcp -project-id=$GCP_PROJECT_ID

# GCP - with custom bucket projects

go run cmd/exporter/exporter.go -provider gcp -project-id=$GCP_PROJECT_ID -gcp.bucket-projects=$GPC_PROJECT_ID -gcp.bucket-projects=$GPC_PROJECT_ID

# AWS - Prod
go run cmd/exporter/exporter.go -provider aws -aws.profile $AWS_PROFILE

# Azure
go run cmd/exporter/exporter.go -provider azure -azure.subscription-id $AZ_SUBSCRIPTION_ID
```

## Project Structure

The main entrypoint for the exporter is `cmd/exporter/exporter.go`. This file is responsible for setting up the exporter and starting the server.
When running the application, there is a `--provider` flag that is used to determine which cloud provider to use. 
Within `pkg/collector`, there are subdirectories for each cloud provider that contain the logic for collecting cost data from that provider.
Each provider is composed of a set of _collectors_.
Each collector represents a cloud resource such as `GKE` or `GCS`, and is responsible for collecting cost data for that resource.
