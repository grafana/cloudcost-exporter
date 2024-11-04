# GKE Compute Metrics

| Metric name                                                | Metric type | Description                                                                                 | Labels                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
|------------------------------------------------------------|-------------|---------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| cloudcost_gcp_gke_instance_cpu_usd_per_core_hour           | Gauge       | The processing cost of a GCP Compute Instance, associated to a GKE cluster, in USD/(core*h) | `cluster_name`=&lt;name of the cluster the instance is associated with&gt; <br/> `instance`=&lt;name of the compute instance&gt; <br/> `region`=&lt;GCP region code&gt; <br/> `family`=&lt;broader compute family (n1, n2, c3 ...) &gt; <br/> `machine_type`=&lt;specific machine type, e.g.: n2-standard-2&gt; <br/> `project`=&lt;GCP project, where the instance is provisioned&gt; <br/> `price_tier`=&lt;spot\|ondemand&gt;                                            |
| cloudcost_gcp_gke_compute_instance_memory_usd_per_gib_hour | Gauge       | The memory cost of a GCP Compute Instance, associated to a GKE cluster, in USD/(GiB*h)      | `cluster_name`=&lt;name of the cluster the instance is associated with&gt; <br/> `instance`=&lt;name of the compute instance&gt; <br/> `region`=&lt;GCP region code&gt; <br/> `family`=&lt;broader compute family (n1, n2, c3 ...) &gt; <br/> `machine_type`=&lt;specific machine type, e.g.: n2-standard-2&gt; <br/> `project`=&lt;GCP project, where the instance is provisioned&gt; <br/> `price_tier`=&lt;spot\|ondemand&gt;                                            |
| cloudcost_gcp_gke_persistent_volume_usd_per_hour       | Gauge       | The cost of a GKE Persistent Volume in USD/(GiB*h)                                          | `cluster_name`=&lt;name of the cluster the instance is associated with&gt; <br/> `namespace`=&lt;The namespace the pvc was created for&gt; <br/> `persistentvolume`=&lt;Name of the persistent volume&gt; <br/> `region`=&lt;The region the pvc was created in&gt; <br/> `project`=&lt;GCP project, where the instance is provisioned&gt; <br/> `storage_class`=&lt;pd-standard\|pd-ssd\|pd-balanced\|pd-extreme&gt; <br/> `disk_type`=&lt;boot_disk\|persistent_volume&gt; <br/> `use_status`=&lt;in-use\|idle&gt; |

## Persistent Volumes

There's two sources of data for persistent volumes:
- Skus from the Billing API
- Disk metadata from compute API

There's a bit of a disconnect between the two.
Sku's descriptions have the following format:
```
Balanced PD Capacity in <Region>
Commitment v1: Local SSD In <Region>
Extreme PD Capacity in <Region>
Extreme PD IOPS in <Region>
Hyperdisk Balanced Capacity in <Region>
Hyperdisk Balanced IOPS in <Region>
Hyperdisk Balanced Throughput in <Region>
Hyperdisk Extreme Capacity in <Region>
Hyperdisk Extreme IOPS in <Region>
Hyperdisk Throughput Capacity in <Region>
Hyperdisk Throughput Throughput Capacity in <Region>
Regional Balanced PD Capacity in <Region>
Regional SSD backed PD Capacity in <Region>
Regional Storage PD Capacity in <Region>
SSD backed Local Storage attached to Spot Preemptible VM in <Region>
SSD backed Local Storage in <Region
SSD backed PD Capacity in <Region>
Storage PD Capacity in <Region>
```

Generically, the sku descriptions have the following format:
```
<sku-type> PD Capacity in <Region>
```

Disk metadata has the following format:
```
projects/<project>/zones/<zone>/disks/<disk-type>
```

To map the sku to the disk type, we can use the following mapping:

- Storage PD Capacity -> pd-standard
- SSD backed PD Capacity -> pd-ssd
- Balanced PD Capacity -> pd-balanced
- Extreme PD Capacity -> pd-extreme
- Hyperdisk Balanced -> hyperdisk-balanced

> [!WARNING]
> The following storage classes are experimental

## Experimental Storage Costs

Cloudcost Exporter needs to support the following hyperdisk pricing dimensions:
- [x] provisioned space
- [ ] Network throughput
- [ ] IOps
- [ ] high availability

[#344](https://github.com/grafana/cloudcost-exporter/pull/344) introduced experimental support for provisioned space for [hyperdisk class](https://cloud.google.com/compute/disks-image-pricing#persistentdisk) 
