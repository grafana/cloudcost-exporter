# Release Process

## Building images

There is a [github workflow](../../.github/workflows/docker.yml) that publishes images on changes to `main` and new tags.

## Versioning

We follow [semver](https://semver.org/) and generate new tags manually.

To cut a release, clone `cloudcost-exporter` locally and pull latest from main.
Determine if the next release is a major, minor, or hotfix.
Then increment the relevant version label.

For instance, let's say we're on `v0.2.2` and determined the next release is a minor change.
The next version would then be `v0.3.0`.
Execute the following command to generate the tag and push it:

```sh
git tag v0.3.0
# Optionally, add a message on why the specific version label was updated: git tag v0.3.0 -m "Adds liveness probes with backwards compatibility"
git push origin tag v0.3.0
```

## Releases

Creating and pushing a new tag that starts with `v` will trigger the `goreleaser` workflow in [./.github/workflows/release.yml](https://github.com/grafana/cloudcost-exporter/tree/main/.github/workflows/release.yml).

The configuration for `goreleaser` itself can be found in [./.goreleaser.yaml](https://github.com/grafana/cloudcost-exporter/blob/main/.goreleaser.yaml).

See https://github.com/grafana/cloudcost-exporter/issues/18 for progress on our path to automating releases.

## GitHub Actions

When adding or upgrading a GitHub Actions `actions`, please set the full length commit SHA instead of the version:

```
jobs:
  myjob:
    runs-on: ubuntu-latest
    steps:
      - uses: foo/baraction@abcdef1234567890abcdef1234567890abcdef12 # v1.2.3
```

Granular control of the version helps with security since commit SHAs are immutable.

## Helm chart

### Contribute to Helm chart

The `cloudcost-exporter`'s Helm chart can be found in the repo's root path at [./deploy/helm/cloudcost-exporter](../../deploy/helm/cloudcost-exporter/README.md)

To contribute to the Helm chart, make changes to the Helm chart [templates](../../deploy/helm/cloudcost-exporter/templates/) and the [values.yaml](../../deploy/helm/cloudcost-exporter/values.yaml).

The Helm chart's [README](../../deploy/helm/cloudcost-exporter/README.md) is generated using [helm-docs](https://github.com/norwoodj/helm-docs) and a [gotemplate](../../deploy/helm/cloudcost-exporter/README.md.gotmpl) that goes with it.

Run `make helm` to check that the Helm chart compiles and that its README is up-to-date.

### Create a release for the Helm chart

Once the Helm chart has been released via the GitHub Actions workflow, follow these steps:

1. Update `deploy/helm/Chart.yaml` with the new Helm `version`, `appVersion`, and `home` path.
