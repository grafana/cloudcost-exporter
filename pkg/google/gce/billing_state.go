package gce

// instanceBilled reports whether a Compute Engine instance is currently
// billed for CPU and memory, based on its lifecycle status. See
// https://docs.cloud.google.com/compute/docs/instances/instance-lifecycle
// for the per-status billing table.
func instanceBilled(status string) bool {
	return status == "RUNNING"
}
