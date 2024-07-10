# Releases

There does not exist automation for releases _yet_. This is a future goal of the project.
For now, we implicitly follow [semver](https://semver.org/) and generate releases manually.
There is a [github workflow](../../.github/workflow/docker.yml) that publishes images on changes to `main` and tags.

To cut a release, close `cloudcost-exporter` locally and pull latest from main.
Determine if the next release is a major, minor, or hotfix. 
Then increment the relevant version label.

For instance, let's say we're on `v0.2.2` and determined the next release is a minor change.
The next version would then be `v0.3.0`. 
Execute the follow command and generate the tag and push it:

```sh
git tag v0.3.0
git push origin tag v0.3.0
```
