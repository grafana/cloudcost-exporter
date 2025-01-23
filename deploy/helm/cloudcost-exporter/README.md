# Cloud Cost Exporter Helm chart

## Usage

[Helm](https://helm.sh/) must be installed in order to deploy the `cloudcost-exporter` Helm chart.

### Setup the Grafana chart repository

```console
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update
```

### Install the chart

To install the chart with the release name my-release:

```console
helm install my-release grafana/cloudcost-exporter
```

## Configuration

The following table lists the configurable parameters of the cloudcost-explorer Helm chart and their default values (in alphabetical order).

Parameter | Description | Default
--- | --- | ---
`affinity` | node/pod affinities | `{}`
`fullnameOverride` | optional full name override | `""`
`image.pullPolicy` | image pull policy | `IfNotPresent`
`image.repository` | image repository | `grafana/cloudcost-exporter`
`image.tag` | overrides the image tag whose default is the chart appVersion | `""`
`imagePullSecrets` | specify image pull secrets | `[]`
`minReadySeconds` |  seconds a pod should be ready to be considered available  | `10`
`nameOverride` | optional name override | `""`
`nodeSelector` | node labels for pod assignment  | `{}`
`podAnnotations` | annotations to add to each pod | `{}`
`podSecurityContext.fsGroup` | filesystem group to associate for each pod | `10001`
`replicaCount` | desired number of pods | `1` |
`resources` | pod resource requests and limits | `{}`
`revisionHistoryLimit` | number of old versions to retain to allow rollback | `10`
`serviceAccount.annotations` | annotations to add to the service account | `{}`
`serviceAccount.create` | specifies whether a service account should be created | `true`
`serviceAccount.name` | name of service account to use - generated | `""`
`service.port` | port number for the service | `80`
`service.protocol` | protocol for the serivce | `TCP`
`service.type` | type of service | `ClusterIP`
`tolerations` | list of node taints to tolerate | `[]`

## Contribute

Check out the [docs](../../../docs/contribute/releases.md#helm-chart) for more information on how to contribute to the `cloudcost-explorer`'s Helm chart.
