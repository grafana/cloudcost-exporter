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
Execute the follow command to generate the tag and push it:

```sh
git tag v0.3.0
# Optionally, add a message on why the specific version label was updated: git tag v0.3.0 -m "Adds liveness probes with backwards compatibility"
git push origin tag v0.3.0
```

## Releases

Creating and pushing a new tag will trigger the `goreleaser` workflow in [./.github/workflows/release.yml](https://github.com/grafana/cloudcost-exporter/tree/main/.github/workflows/release.yml).

The configuration for `goreleaser` itself can be found in [./.goreleaser.yaml](https://github.com/grafana/cloudcost-exporter/blob/main/.goreleaser.yaml).

See https://github.com/grafana/cloudcost-exporter/issues/18 for progress on our path to automating releases.
