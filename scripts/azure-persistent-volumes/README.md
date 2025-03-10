# Azure Persistent Volumes Notes

Related issue: https://github.com/grafana/cloudcost-exporter/issues/236

This is a set of scripts to prove out the Azure Persistent Volumes cost feature.
Writing scratch examples provided an opportunity to explore Azure's api's without committing too much code to the main codebase.
The scripts are useful if you're looking to explore azure's data and responses, or even just pull data in locally.

## Problem

Programmatically provide the cost of Azure storage devices such as
- Solid State Disk Drives(SSD)
- Hard Disk Drives(HDD)

## Scripts

- `main.go`: Pull all persistent volumes for a subscription and save them to a file.
- `list-prices/download-all-list-prices.go`: Downloads all Azure Persistent Volumes prices from the Azure Retail Pricing API and saves them to a file.


## Underlying Data

There's two bits of information needed to calculate the cost of a storage device
- disk information
- cost for disks

Listing these out is pretty straight forward.
- listing [disks](https://github.com/grafana/cloudcost-exporter/blob/290303c6539453967e9693bde54873baf211f06c/scripts/azure-persistent-volumes/main.go#L52)
- listing [prices](https://github.com/grafana/cloudcost-exporter/blob/290303c6539453967e9693bde54873baf211f06c/pkg/azure/aks/volume_price_store.go#L303-L310)

Disks are presented with the following bits of metadata
- Sku
- Name
- Size in GiB
- Performance properties that impact pricing

Prices are presented with the following bits of metadata:
- ServiceName
- ServiceID
- ServiceFamily
- SkuName
- ArmSkuName
- MeterName
- ArmRegionName
- UnitPrice

A subtle problem is that the Price has 3 fields that could be used to represent the Sku.
None of these sku's align directly with the Disk's metadata.

The disks skuname returns stuff like
- `Premium_LRS`: Premium locally redundant storage (SSD)
- `Premium_ZRS`: Premium zone-redundant storage (SSD)
- `Standard_LRS`: Standard locally redundant storage (HDD)
- `StandardSSD_LRS`: Standard SSD locally redundant storage
- `StandardSSD_ZRS`: Standard SSD zone-redundant storage
- `UltraSSD_LRS`: Ultra SSD locally redundant storage

Pre V2 pricing for disks is tiered in powers of 2 and the price depends on the level the disk size falls into.
The following dimesions are used to figure out the cost of the disk:
- region
- size in GiB(tiered)
- local vs regional replication
- iops provisioned
- bandwidth
- snapshot or not

The cost per GiB is determined by the nearest(rounded up) tier. So a 200 GiB machine would be priced at the 256 GiB tier. [Source][pricing-source]

[pricing-source](https://learn.microsoft.com/en-us/azure/virtual-machines/disks-types#billing)

### Subtle Differences in skus

We primarily care about the costs of disks.
A subtle difference between the sku's is the difference between skuname, productname, and armskuname.
For persistent volumes(v1), armskuname is populated with the tier.
For standard volumes(hdd + ssd) armskuname is empty.
The challenge here is how to normalize this in such a way that disks of both classes can lookup their costs.

The current map is basically
- region => armskuname

This doesn't hold up. A potentially better solution would be
- region => productname => skuname

This allows the pricing map to account properly for the difference in prices between zrs and lrs disks.

What would lookup look like?
From disks, you get the following attributes
- Name(Standard_LRS, StandardSSD_LRS, Premium_LRS)
- Tier(Standard, Premium)
- Unreliable Tier(P seems to be the only one reliably added)

So again, what would lookup look like in this case?


### Sketch of an algorithm

From a disk, you get the following data points:
- region
- sku prefix
- disk size

With the disk size, you can figure out the tier the disk belongs in.
So with this you could the combine the two to find the monthly cost all together.
`go
sku := fmt.Sprintf("%s_%s", disk.SkuName, getTierForDisk(disk.Properties.SizeInGib))
price, err := priceMap[disk.Region][sku]
...
`

This is a simplistic example that ignores transaction costs.
If a disk exceeds a certain amount of transactions, then it's charged by transactions.

Special considerations
- snapshots are charged $/GiB
- transactions


### Transactions

https://learn.microsoft.com/en-us/azure/virtual-machines/disks-types#standard-hdd-transactions

For Standard HDDs, each I/O operation is considered as a single transaction, whatever the I/O size. These transactions have a billing impact.

## Findings

You can easily query Azure's retail price API with the sdk.
Filtering out specific services is simple in concept.

Finding documentation for filters is challenging.
Azure documentation does have a list of services explicitly declared: https://learn.microsoft.com/en-us/rest/api/cost-management/retail-prices/azure-retail-prices#supported-servicefamily-values

### How is pre V2 hard drives charged

This is the most perplexing problem.

