# Release Process

## Building images

There is a [github workflow](../../.github/workflows/build-and-deploy.yml) that publishes images on changes to `main` and new tags.

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

Creating and pushing a new tag will trigger the `goreleaser` workflow in [./.github/workflows/release.yml](https://github.com/grafana/cloudcost-exporter/tree/main/.github/workflows/release.yml).

The configuration for `goreleaser` itself can be found in [./.goreleaser.yaml](https://github.com/grafana/cloudcost-exporter/blob/main/.goreleaser.yaml).

See https://github.com/grafana/cloudcost-exporter/issues/18 for progress on our path to automating releases.

### Automated Deployment

When a new release is published (by pushing a tag), the workflows automatically:
1. **[release.yml](../../.github/workflows/release.yml)**: Creates the GitHub release with binaries (via GoReleaser)
2. **[build-and-deploy.yml](../../.github/workflows/build-and-deploy.yml)**: Builds and pushes the Docker image to Docker Hub
3. **build-and-deploy.yml deploy job**: Immediately after the image is pushed, triggers an Argo Workflow deployment to platform-monitoring-cd
4. **Argo Workflow**: Creates deployment PRs for each rollout wave

The deployment happens as a follow-up job in the build-and-deploy workflow, running immediately after the image is successfully built and pushed.

The workflow uses the [grafana/shared-workflows/actions/trigger-argo-workflow](https://github.com/grafana/shared-workflows) action, which handles authentication and workflow submission automatically.

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

The `cloudcost-exporter`'s Helm chart can be found here: https://github.com/grafana/helm-charts/tree/main/charts/cloudcost-exporter

### Helm chart release process

If making changes to the Chart template/values (optional):
1. Make changes to the Helm chart [templates](https://github.com/grafana/helm-charts/tree/main/charts/cloudcost-exporter/templates/) if needed
1. Update the [values.yaml](https://github.com/grafana/helm-charts/tree/main/charts/cloudcost-exporter/values.yaml) if needed

Once changes have been made to the Chart itself (see above) and/or **there is a new release of cloudcost-exporter** (required):
1. Update the [Chart.yaml](https://github.com/grafana/helm-charts/tree/main/charts/cloudcost-exporter/Chart.yaml):
    * Make sure that the `appVersion` matches the new cloudcost-explorer release version
    * Bump the Helm chart's `version` too
1. [Generate the Helm chart's README](https://github.com/grafana/helm-charts/blob/main/CONTRIBUTING.md#generate-readme)
