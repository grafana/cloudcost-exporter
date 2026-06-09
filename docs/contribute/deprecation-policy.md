# Deprecation Policy

Backward compatibility and feature stability matter, especially since cloudcost-exporter is used in production systems and reached its first major (`x.0.0`) [semver](https://semver.org/#semantic-versioning-specification-semver) version. The project should follow a deprecation policy for removing existing features to minimize breaking changes. This applies to features like Prometheus metric names and labels and CLI flags.

## Steps

1. Mark a feature as deprecated - example: https://github.com/grafana/cloudcost-exporter/pull/467:
   1. Open a PR so that cloudcost-exporter logs a deprecation warning.
   2. Update any docs with a warning as well. For example, update the service's docs if this is a Prometheus metric or label change; if this is a CLI flag change, find any references to it in the docs and add a warning, including in the Helm chart and its docs.
   3. In the warning, recommend the feature that replaces it (if any), and inform the user about the major release (`x.0.0`) that is targeted for the feature's removal.
2. Track the feature to be deprecated in an issue, including the major release (`x.0.0`) that is targeted for this feature's removal. Example: https://github.com/grafana/cloudcost-exporter/issues/1000.
3. A deprecated feature must remain available for a minimum of 3 minor releases or months, whichever is longer.
4. When the team / maintainers decide that cloudcost-exporter is ready for a major release, open a PR to fully remove all features that were marked as deprecated, including any helper funcs, warning logs, docs, etc.
