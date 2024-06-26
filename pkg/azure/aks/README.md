# AKS Module

This module is responsible for collecting pricing information for AKS clusters.


## Pricing Map

Because Azure has not yet implemented the pricing API into it's SDK :shame:, this packages uses the 3rd party [Azure Retail Prices SDK](https://github.com/gomodules/azure-retail-prices-sdk-for-go) to grab the prices.

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

# Future Work 

- (Pricing Map) - implement background job to populate pricing map every 24 hours
- (Pricing Map) - clear and repopulate Price Map and cache with backgroud job
- (Pricing Map) - implement retry mechanism to pricing map, crash program if it doesn't populate after 5 tries
- (Pricing Map) - implement VM lookup by machine ID
- (VMs) - implement VM list
- connect VM list with Pricing Map 
- Prometheus metrics
