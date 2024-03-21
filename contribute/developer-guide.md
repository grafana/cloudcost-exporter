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

# GCP - prod
go run cmd/exporter/exporter.go -provider gcp -project-id=ops-tools-1203

# GCP - prod - with custom bucket projects

go run cmd/exporter/exporter.go -provider gcp -project-id=ops-tools-1203 -gcp.bucket-projects=grafanalabs-global -gcp.bucket-projects=ops-tools-1203

# GCP - dev
go run cmd/exporter/exporter.go -provider gcp -project-id=grafanalabs-dev

# AWS - dev
go run cmd/exporter/exporter.go -provider aws -aws.region=us-east-1 -aws.profile workloads-dev

# AWS - Prod
go run cmd/exporter/exporter.go -provider aws -aws.profile workloads-prod
```

> [!Note]
> GCP Only: you can specify the services to collect cost metrics on.
> To collect GKE, append any of the gcp commands with `-gcp.services=gke -gcp.services=gcs`
