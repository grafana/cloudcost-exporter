# Release Process

## Building images

Docker images are automatically built and published when:
- A PR is merged to `main` (with or without release labels)
- A version tag is pushed manually

The workflows that handle this are:
- **[release-on-pr-merge.yml](../../.github/workflows/release-on-pr-merge.yml)**: Handles PR merges to `main`
- **[release-on-tag-push.yml](../../.github/workflows/release-on-tag-push.yml)**: Handles manual tag pushes

## Versioning

We follow [semver](https://semver.org/) and use **automated releases** based on PR labels.

## Automated Releases (Recommended)

Releases are now automated! When you merge a PR with a release label, the workflow will:

1. Calculate the next version based on the label
2. Create and push the tag automatically
3. Create a GitHub release with binaries via GoReleaser
4. Build and push Docker images with versioned tags
5. Trigger Argo Workflow deployment

### How to Create a Release

1. Create your PR with your changes targeting `main`
2. Add a release label to the PR:
   - `release:major` - For breaking changes (e.g., `v1.2.3` → `v2.0.0`)
   - `release:minor` - For new features (e.g., `v1.2.3` → `v1.3.0`)
   - `release:patch` - For bug fixes and dependency updates (e.g., `v1.2.3` → `v1.2.4`)
3. Merge the PR - The [release-on-pr-merge.yml](../../.github/workflows/release-on-pr-merge.yml) workflow will run automatically

### Release Label Guidelines

| Change Type | Label | Example |
|------------|-------|---------|
| Breaking changes, API removals | `release:major` | `v1.2.3` → `v2.0.0` |
| New features, backwards-compatible additions | `release:minor` | `v1.2.3` → `v1.3.0` |
| Bug fixes, dependency updates, documentation | `release:patch` | `v1.2.3` → `v1.2.4` |

**Important**: Only use **ONE** release label per PR. If multiple labels are present, the workflow will fail with an error.

### What Happens When You Merge a PR

**With a release label:**
- Version tag is created and pushed (e.g., `v1.2.3`)
- GitHub release is created with binaries via GoReleaser
- Docker images are built and pushed with tags:
  - `latest` (always updated)
  - `main-{sha}` (commit SHA, always updated)
  - `v{version}` (versioned tag, only for releases)
- Argo Workflow deployment is triggered

**Without a release label:**
- Docker images are built and pushed with tags:
  - `latest` (always updated)
  - `main-{sha}` (commit SHA, always updated)
- No version tag, GitHub release, or deployment

## Manual Releases (Fallback)

If you need to create a release manually (e.g., for hotfixes or special cases):

1. Clone `cloudcost-exporter` locally and pull latest from main
2. Determine the version number (must follow semver: `vMAJOR.MINOR.PATCH`)
3. Create and push the tag:

```sh
git tag v0.3.0
# Optionally, add a message: git tag v0.3.0 -m "Adds liveness probes with backwards compatibility"
git push origin tag v0.3.0
```

Pushing a tag manually will trigger the [release-on-tag-push.yml](../../.github/workflows/release-on-tag-push.yml) workflow, which will:
- Create a GitHub release with binaries via GoReleaser
- Build and push Docker images with the versioned tag
- Trigger Argo Workflow deployment

## Release Workflows

The configuration for `goreleaser` itself can be found in [.goreleaser.yaml](../../.goreleaser.yaml).

### Automated Deployment

When a new release is published (by merging a PR with a release label or pushing a tag manually), the workflows automatically trigger an Argo Workflow deployment to `platform-monitoring-cd`.

The deployment happens in the `deploy` job, which runs after the Docker images are successfully built and pushed. The workflow uses the [grafana/shared-workflows/actions/trigger-argo-workflow](https://github.com/grafana/shared-workflows) action, which handles authentication and workflow submission automatically.

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
