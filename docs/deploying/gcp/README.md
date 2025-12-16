# GCP CloudCost Exporter Deployment

## Setup Workload Identity authentication

### 1. Create a custom IAM role

cloudcost-exporter uses the [Google Cloud SDK for Go](https://cloud.google.com/go/docs/reference). It supports providing authentication via [Application Default Credentials (ADC)](https://cloud.google.com/docs/authentication/application-default-credentials).

When deployed on GKE, the recommended approach is to use [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/concepts/workload-identity) to authenticate the cloudcost-exporter Service Account.

First, create a custom IAM role with the minimum required permissions for cloudcost-exporter.

>[!NOTE]
> Check the docs for each service to see which permission(s) it requires.

[Create a custom role](https://cloud.google.com/iam/docs/creating-custom-roles) at the organization level with the required permissions.

### 2. Create a GCP Service Account

Next, [create a GCP Service Account](https://cloud.google.com/iam/docs/service-accounts-create) that cloudcost-exporter will use.

### 3. Bind the custom role to the Service Account

[Bind the custom role to the Service Account](https://cloud.google.com/iam/docs/grant-role-console) at the organization or folder level.

### 4. Configure Workload Identity

Next, [configure Workload Identity for the cloudcost-exporter Kubernetes Service Account](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity#kubernetes-sa-to-iam).

The GCP Service Account will be passed as an [annotation to the Kubernetes Service Account](#serviceaccountannotations-required) in the values file of the Helm chart.

### 5. Configure the Helm chart

The Helm chart can be deployed after creating the necessary role and service account binding described above.

<!-- TODO: Add example values file with the additional GCP-specific values here: https://github.com/grafana/helm-charts/blob/main/charts/cloudcost-exporter/values.gcp.yaml -->

The GCP-specific values can also be set like this:
```console
helm install my-release grafana/cloudcost-exporter \
--set 'containerArgs[0]=--provider=gcp' \
--set 'containerArgs[1]=--project-id=my-project-id' \
--set 'containerArgs[2]=--gcp.services=gke\,gcs' \
--set-string serviceAccount.annotations."iam\.gke\.io/gcp-service-account"="cloudcost-exporter@my-project-id.iam.gserviceaccount.com" \
--namespace cloudcost-exporter --create-namespace
```

### `containerArgs` (required)

Set GCP as the provider:
```
  - "--provider=gcp"
```

Set the project ID that the client should authenticate with:
```
  - "--project-id=my-project-id"
```

Set which GCP services to collect metrics from:
```
  - "--gcp.services=gke,gcs"
```

Optionally, set specific projects to target for bucket metrics:
```
  - "--gcp.bucket-projects=project-1,project-2"
```

### `serviceAccount.annotations` (required)

Annotate the Kubernetes `serviceAccount` with the GCP Service Account email.
This should look like the following:

```yaml
serviceAccount:
  annotations:
    iam.gke.io/gcp-service-account: cloudcost-exporter@my-project-id.iam.gserviceaccount.com
```
