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

When you open or update a PR, the [suggest-release-label.yml](../../.github/workflows/suggest-release-label.yml) workflow will post a comment with a label recommendation based on all commits since the last release. This is informational — the final label choice is yours.

### Release Label Guidelines

| Change Type | Label | Example |
|------------|-------|---------|
| Breaking changes, API removals | `release:major` | `v1.2.3` → `v2.0.0` |
| New features, backwards-compatible additions | `release:minor` | `v1.2.3` → `v1.3.0` |
| Bug fixes, dependency updates, documentation | `release:patch` | `v1.2.3` → `v1.2.4` |

**Important**: Only use **ONE** release label per PR. If multiple labels are present, the workflow will fail with an error.

### Commit types and release labels

This repo enforces [Conventional Commits](https://www.conventionalcommits.org/) on PR titles (since the repo uses squash merges, the PR title becomes the commit message). Use the following table to choose the right release label based on your commit type:

| Commit type | Affects binary? | Suggested label |
|-------------|-----------------|-----------------|
| `feat`      | yes             | `release:minor` |
| `fix`       | yes             | `release:patch` |
| `refactor`  | yes             | `release:minor` |
| `perf`      | yes             | `release:minor` |
| `docs`      | no              | no label needed |
| `ci`        | no              | no label needed |
| `test`      | no              | no label needed |
| `build`     | no              | no label needed |
| `style`     | no              | no label needed |
| `chore`     | no              | no label needed |

For breaking changes, append `!` to the type (e.g. `feat!:`) and use `release:major`.

### Checking the minimum required bump

To see what label the commits since the last release would require, run [git-cliff](https://git-cliff.org) locally:

```sh
git cliff --bumped-version
```

This uses `cliff.toml` at the repo root and computes the minimum next version based on Conventional Commit types since the last tag.

### Dependency updates

Renovate manages dependency updates automatically and uses the correct commit type by convention:

- **Go module updates** use `fix(deps)` — these affect the compiled binary and should use `release:patch`
- **Non-Go updates** (GitHub Actions, linters, etc.) use `chore(deps)` — these don't affect the binary and need no release label

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
- Open a PR to bump the Helm chart `appVersion` (see [Helm chart](#helm-chart))

## Release Workflows

The configuration for `goreleaser` itself can be found in [.goreleaser.yaml](../../.goreleaser.yaml).

### Automated Deployment

When a new release is published (by merging a PR with a release label or pushing a tag manually), the workflows automatically trigger an Argo Workflow deployment to `platform-monitoring-cd`.

The deployment happens in the `deploy` job, which runs after the Docker images are successfully built and pushed. The workflow uses the [grafana/shared-workflows/actions/trigger-argo-workflow](https://github.com/grafana/shared-workflows) action, which handles authentication and workflow submission automatically.

## GitHub Actions

When adding or upgrading a GitHub Actions `actions`, please set the full length commit SHA instead of the short version:

```
jobs:
  myjob:
    runs-on: ubuntu-latest
    steps:
      - uses: foo/baraction@abcdef1234567890abcdef1234567890abcdef12 # v1.2.3
```

Granular control of the version helps with security since commit SHAs are immutable.

## Helm chart

The `cloudcost-exporter`'s Helm chart is maintained in this repository at `charts/cloudcost-exporter/`.

### Automated chart bump on release

Every cloudcost-exporter release bumps the chart automatically. When a release is cut, the `update-helm-chart` job opens a PR that sets the chart `appVersion` to the released version and patch-bumps the chart `version`. This happens on both release paths:

- a labeled PR merge to `main` ([release-on-pr-merge.yml](../../.github/workflows/release-on-pr-merge.yml))
- a manual tag push ([release-on-tag-push.yml](../../.github/workflows/release-on-tag-push.yml))

Both call the shared [update-helm-chart](../../.github/actions/update-helm-chart/action.yml) composite action, so the generated PR is identical regardless of path: title `chore: update Helm chart to appVersion X.Y.Z`, touching only `charts/cloudcost-exporter/Chart.yaml`.

Review and merge that PR to publish the chart. Merging any change to `charts/cloudcost-exporter/Chart.yaml` on `main` triggers [release-helm-chart.yaml](../../.github/workflows/release-helm-chart.yaml), which:

- creates a `cloudcost-exporter-X.Y.Z` tag,
- publishes the packaged chart to grafana/helm-charts (index and ghcr.io OCI image),
- triggers the `chartfile-version-update` Argo workflow in `platform-monitoring-cd` to bump the chart pin in `deployment_tools` (`ksonnet/lib/cloudcost-exporter/chartfile.yaml`).

### Chart template or values changes

The automated bump covers `appVersion` and the chart `version` only. For changes to the chart itself:

1. **Update the chart**:
   - Modify templates in `charts/cloudcost-exporter/templates/`
   - Update `values.yaml` or `values.aws.yaml` if needed
   - Bump the chart `version` in `Chart.yaml` according to semver:
     - **Minor**: new features, new values, backward-compatible changes
     - **Major**: breaking changes to chart structure or values

2. **Generate README**:
   ```bash
   # Install helm-docs if not already installed
   go install github.com/norwoodj/helm-docs/cmd/helm-docs@latest

   # Generate README from README.md.gotmpl
   helm-docs charts/cloudcost-exporter
   ```

3. **Open a PR**. Merging it triggers the same [release-helm-chart.yaml](../../.github/workflows/release-helm-chart.yaml) workflow described above. Use the workflow's manual `workflow_dispatch` trigger only as a fallback if the path-based trigger does not fire.

4. **Verify the release**:
   - Check https://github.com/grafana/helm-charts/releases for the new tag
   - Verify the chart is available:
     ```bash
     helm repo add grafana https://grafana.github.io/helm-charts
     helm repo update
     helm search repo grafana/cloudcost-exporter
     ```

### Historical context

Prior to chart version 1.0.7, the Helm chart was maintained in the centralized [grafana/helm-charts](https://github.com/grafana/helm-charts/tree/main/charts/cloudcost-exporter) repository. It was moved back to the source repository to improve maintainability and release coordination.
