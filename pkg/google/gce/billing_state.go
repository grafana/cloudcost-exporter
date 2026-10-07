package gce

// cpuBilled reports whether a Compute Engine instance's CPU is currently
// billed, based on its lifecycle status. See
// https://docs.cloud.google.com/compute/docs/instances/instance-lifecycle
// for the per-status billing table.
func cpuBilled(status string) bool {
	return status == "RUNNING"
}

// memoryBilled reports whether a Compute Engine instance's memory is
// currently billed. Memory is billed while running, and while suspending or
// suspended it's billed as the storage cost of the preserved memory
// snapshot rather than the running rate. See cpuBilled's doc link.
func memoryBilled(status string) bool {
	return cpuBilled(status) || status == "SUSPENDING" || status == "SUSPENDED"
}
