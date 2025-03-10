# Deploying Cloud Cost Exporter

There are multiple ways to deploy Cloud Cost Exporter:

* [Helm Chart](../../deploy/helm/cloudcost-exporter/README.md)
* [Image](#use-the-image)

## Use the image

Each tagged version will publish a docker image to https://hub.docker.com/r/grafana/cloudcost-exporter and a Helm chart with the version tag.
The image can be run locally or deployed to a kubernetes cluster.
Cloud Cost Exporter has an opinionated way of authenticating against each cloud provider.

| Provider | Notes |
|-|-|
| GCP | Depends on [default credentials](https://cloud.google.com/docs/authentication/application-default-credentials) |
| AWS | Uses profile names from your [credentials file](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-files.html) or `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_REGION` env variables |
| Azure | Uses the [default azure credential chain](https://learn.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication?tabs=bash), e.g. enviornment variables: `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, and `AZURE_CLIENT_SECRET` |

When running in a kubernetes cluster, it is recommended to use a service account with the necessary permissions for the cloud provider.

Documentation about about the necessary permissions for each cloud provider are under development and can be found in [docs/deploying/aws/](../docs/deploying/).

| Provider | Notes |
|-|-|
| GCP | Depends on [default credentials](https://cloud.google.com/docs/authentication/application-default-credentials) |
| AWS | Uses profile names from your [credentials file](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-files.html) or `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_REGION` env variables |
| Azure | Uses the [default azure credential chain](https://learn.microsoft.com/en-us/azure/developer/go/azure-sdk-authentication?tabs=bash), e.g. enviornment variables: `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`, and `AZURE_CLIENT_SECRET` |

When running in a kubernetes cluster, it is recommended to use a service account with the necessary permissions for the cloud provider.

Documentation about about the necessary permissions for each cloud provider are under development. AWS docs can be found in [docs/deploying/aws/](../../docs/deploying/aws/README.md).
