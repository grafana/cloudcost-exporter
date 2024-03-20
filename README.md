# Cloud Cost Exporter

Cloud Cost exporter is a metrics exporter designed to collect cost data from cloud providers and export the data in Prometheus format.
This data can then be combined with usage data from tools such as stackdriver, yace, and promitor to provide a better picture of cloud costs.

## Roadmap

The roadmap is as follows:
- [x] GCP Cloud Storage
- [x] AWS S3
- [ ] Azure Blob Storage
- [ ] GCP Cloud SQL
- [ ] AWS RDS

* We don't take into account currencies for now and assume all costs are in USD.

## Running

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

## Architecture

### AWS

AWS will export four metrics:
- `aws_s3_operations_cost`
- `aws_s3_storage_hourly_cost`
- `aws_cost_exporter_requests_total`
- `aws_cost_exporter_next_scrape`

The AWS exporter is built upon [aws-sdk-go-v2](https://github.com/aws/aws-sdk-go-v2).
aws-sdk exposes pricing information in two ways:
- [costexplore api](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/costexplorer#Client)
- [pricing api](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/pricing#Client)

We opted to use the costexplore api because AWS has a tiered pricing structure for storage costs.
The majority of our buckets are _over_ a size threshold that makes it cost less $/GiB.
Using the blended costs for cost explorer provides a more accurate $/GiB then if we were to use the pricing api.
The shortcoming for this is that we only export metrics for regions we operate in _and_ we need to maintain a [mapping](https://github.com/grafana/deployment_tools/blob/f94b56492b0b4e3bd0c8641200629e2480c49f24/docker/cloudcost-exporter/pkg/aws/aws.go#L27-L54) of the way `costexplore` represents regions.

We craft a [cost explore](https://us-east-1.console.aws.amazon.com/cost-management/home#/cost-explorer?chartStyle=STACK&costAggregate=unBlendedCost&endDate=2023-06-30&excludeForecasting=false&filter=%5B%5D&futureRelativeRange=CUSTOM&granularity=Monthly&groupBy=%5B%22Service%22%5D&historicalRelativeRange=LAST_6_MONTHS&isDefault=true&reportName=New%20cost%20and%20usage%20report&showOnlyUncategorized=false&showOnlyUntagged=false&startDate=2023-01-01&usageAggregate=undefined&useNormalizedUnits=false) query to fetch the previous months worth of billing data.
See https://github.com/grafana/deployment_tools/blob/f94b56492b0b4e3bd0c8641200629e2480c49f24/docker/cloudcost-exporter/pkg/aws/aws.go#L219-L240 for the specific section of code that crafts the query.
Here is a cost explore query that we generate in code: [cost explore](https://us-east-1.console.aws.amazon.com/cost-management/home#/cost-explorer?chartStyle=STACK&costAggregate=unBlendedCost&endDate=2023-07-16&excludeForecasting=false&filter=%5B%7B%22dimension%22:%7B%22id%22:%22Service%22,%22displayValue%22:%22Service%22%7D,%22operator%22:%22INCLUDES%22,%22values%22:%5B%7B%22value%22:%22Amazon%20Simple%20Storage%20Service%22,%22displayValue%22:%22S3%20(Simple%20Storage%20Service)%22%7D%5D%7D%5D&futureRelativeRange=CUSTOM&granularity=Daily&groupBy=%5B%22UsageType%22%5D&historicalRelativeRange=LAST_6_MONTHS&isDefault=true&reportName=New%20cost%20and%20usage%20report&showOnlyUncategorized=false&showOnlyUntagged=false&startDate=2023-06-16&usageAggregate=undefined&useNormalizedUnits=false)


### GCP

- TODO
