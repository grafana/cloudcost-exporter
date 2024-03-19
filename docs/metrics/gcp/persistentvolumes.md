# Persistent Volumes

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