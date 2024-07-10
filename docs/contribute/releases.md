# Releases

There does not exist automation for releases _yet_. This is a future goal of the project.
We follow [semver](https://semver.org/) and generate releases manually. See https://github.com/grafana/cloudcost-exporter/issues/18 for progress on our path to automating releases.
There is a [github workflow](../../.github/workflow/docker.yml) that publishes images on changes to `main` and tags.

To cut a release, clone `cloudcost-exporter` locally and pull latest from main.
Determine if the next release is a major, minor, or hotfix. 
Then increment the relevant version label.

For instance, let's say we're on `v0.2.2` and determined the next release is a minor change.
The next version would then be `v0.3.0`. 
Execute the follow command and generate the tag and push it:

```sh
git tag v0.3.0
# Optionally, add a message on why the specific version label was updated: git tag v0.3.0 -m "Adds liveness probes with backwards compatibility"
git push origin tag v0.3.0
```
