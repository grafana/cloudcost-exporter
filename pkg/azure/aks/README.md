# AKS Module

This module is responsible for collecting pricing information for AKS clusters.


## Pricing Map

Because Azure has not yet implemented the pricing API into it's SDK :shame:, this package uses the 3rd party [Azure Retail Prices SDK](https://github.com/gomodules/azure-retail-prices-sdk-for-go) to grab the prices.

This is based on [Azure's pricing model](https://azure.microsoft.com/en-us/pricing/details/virtual-machines/windows/), where different prices are determined by a combination of those factors.

### Price Stratification

The PricingMap is built out with the following structure:

```
root -> {
  regionName -> {
    machinePriority -> {
      operatingSystem -> {
        skuName -> information
      }
    }
  }
}
```

That way, in order to uniquely identify a price, we will have to have the following attributes of any VM:
- the region it is deployed into
- it's priority (spot or on-demand)
- the operating system it is running
- it's SKU (e.g. `E8-4as_v4`)

## Machine Map

In order to collect the VMs that are relevant for AKS, this package grabs a list of relevant machines in the following way:

- A list of AKS clusters for the subscription are obtained
- Each VMSS (Virtual Machine Scale Set) that creates worker nodes is collected for the resource groups that Azure uses to provision VMs
- Each VM for the VMSS is collected
- VMSS and their metadata (namely their pricing SKU) is stored in a map with the following structure:

```
root -> {
  vmUniqueName -> information
}
```

In parallel, machine types and their relevant info are collected, stored in a map with the following structure:

```
root -> {
  region -> {
    sizeIdentifier -> sizingInformation
  }
}


```

The information contained on the VM Information is enough to uniquely identify both the machine itself and the price that accompanies it.  The sizing information allows CPU and Memory price calculation.
