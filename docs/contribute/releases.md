# Release Process

## Building images

There is a [github workflow](../../.github/workflows/build-and-deploy.yml) that publishes images on changes to `main` and new tags.

## Versioning

We follow [semver](https://semver.org/) and use **automated releases** based on PR labels.

## Automated Releases (Recommended)

Releases are now automated! When you merge a PR with a release label, the workflow will:

1. Calculate the next version based on the label
2. Create and push the tag automatically
3. Create a GitHub release with binaries via GoReleaser

### How to Create a Release

1. Create your PR with your changes targeting `main`
2. Add a release label to the PR:
   - `release:major` - For breaking changes (e.g., `v1.2.3` → `v2.0.0`)
   - `release:minor` - For new features (e.g., `v1.2.3` → `v1.3.0`)
   - `release:patch` - For bug fixes and dependency updates (e.g., `v1.2.3` → `v1.2.4`)
3. Merge the PR - The release workflow will run automatically

### Release Label Guidelines

| Change Type | Label | Example |
|------------|-------|---------|
| Breaking changes, API removals | `release:major` | `v1.2.3` → `v2.0.0` |
| New features, backwards-compatible additions | `release:minor` | `v1.2.3` → `v1.3.0` |
| Bug fixes, dependency updates, documentation | `release:patch` | `v1.2.3` → `v1.2.4` |

**Important**: Only use **ONE** release label per PR. If multiple labels are present, the workflow will fail with an error.

## Manual Releases (Fallback)

If you need to create a release manually (e.g., for hotfixes or special cases):

1. Clone `cloudcost-exporter` locally and pull latest from main
2. Determine if the next release is a major, minor, or patch
3. Create and push the tag:

```sh
git tag v0.3.0
# Optionally, add a message: git tag v0.3.0 -m "Adds liveness probes with backwards compatibility"
git push origin tag v0.3.0
```

Pushing a tag manually will also trigger the release workflow.

## Releases

The [release workflow](../../.github/workflows/auto-release.yml) handles both automated (PR-based) and manual (tag push) releases through `goreleaser`.

The configuration for `goreleaser` itself can be found in [./.goreleaser.yaml](../../.goreleaser.yaml).

### Automated Deployment

When a new release is published (by merging a PR with a release label or pushing a tag manually), the workflows automatically:

1. **[auto-release.yml](../../.github/workflows/auto-release.yml)**: 
   - Creates the tag (if triggered by PR)
   - Creates the GitHub release with binaries (via GoReleaser)
   - Generates changelog from git commits
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
