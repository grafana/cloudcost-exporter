# GKE Module

Collects and exports costs associated with GKE instances.
It's built on top of main of the same primitives as the GCP module.
Specifically we share
- PricingMap
- MachineSpec
- ListInstances

What differs between the two is that the module will filter out instances that are not GKE instances.
This is done by checking the `labels` field of the instance and looking for the cluster name.
If no cluster name is found, the instance is not considered a GKE instance and is filtered out.

The primary motivation for this module was to ensure we could support the following cases with ease:
1. Collecting costs for GKE instances
2. Collecting costs for Compute instances that _may not_ be a GKE instance
3. Collecting costs for Persistent Volumes that may be attached to a GKE instance

See the [Design Doc](https://docs.google.com/document/d/1nCU1SVsuJ4HpV6R-N-AFBaDI5AJmSS3q9jH8_h-_Y8s/edit) for the rationale for a separate module.
TL;DR; We do not want to emit metrics with a `exporter_cluster` label that is empty or make the setup process more complex needed.
