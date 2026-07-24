# Leader election

Each cloudcost-exporter replica collects pricing rates from the cloud provider
APIs independently. Running more than one replica multiplies the load on those
APIs without changing the exported rates, since every replica reports the same
numbers.

Leader election makes a single replica collect at a time. It is opt-in and off
by default, so single-replica deployments are unaffected.

When enabled:

- Replicas acquire a Kubernetes [Lease](https://kubernetes.io/docs/concepts/architecture/leases/) to elect one leader.
- The leader registers the provider collectors and calls the cloud provider APIs.
- Non-leaders serve `/metrics` with the base operational metrics (build info, Go
  runtime, process, and the leader gauge), but do not call the provider APIs.
- Losing the lease stops collection and shuts the replica down. The Deployment
  restarts it, and it rejoins as a candidate.

This keeps a single set of upstream API calls regardless of replica count.
Splitting collection across replicas (sharding) is out of scope.

## Enabling

Run with `-leader-election.enabled`:

```bash
cloudcost-exporter -provider aws -aws.region us-east-1 -aws.services EC2,S3 \
  -leader-election.enabled
```

Leader election requires running inside a Kubernetes cluster; it uses the
in-cluster service account to reach the API server.

## Flags

| Flag | Default | Description |
| --- | --- | --- |
| `-leader-election.enabled` | `false` | Enable lease-based leader election. |
| `-leader-election.lease-name` | `cloudcost-exporter` | Name of the Lease object. |
| `-leader-election.namespace` | service account namespace, then `default` | Namespace of the Lease object. |
| `-leader-election.id` | hostname | Unique identity for this replica. |
| `-leader-election.lease-duration` | `15s` | Duration a non-leader waits before it can acquire leadership. |
| `-leader-election.renew-deadline` | `10s` | Duration the leader retries refreshing the lease before giving up leadership. |
| `-leader-election.retry-period` | `2s` | Interval between leader-election attempts. |

## RBAC

The service account needs access to the Lease in its namespace:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: cloudcost-exporter-leader-election
rules:
  - apiGroups: ["coordination.k8s.io"]
    resources: ["leases"]
    verbs: ["get", "create", "update"]
```

Bind the Role to the service account the exporter runs as.

## Metrics

| Metric | Description |
| --- | --- |
| `cloudcost_exporter_leader_election_is_leader` | `1` on the replica holding the lease, `0` otherwise. |

Summing this gauge across replicas yields `1` while a leader is active, which
makes a missing or duplicated leader easy to alert on.
