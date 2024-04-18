# Creating a New Module

This document outlines the process for creating a new module for the Cloud Cost Exporter.
The current architecture of the exporter is designed to be modular and extensible.

Steps:
1. Create a new module in the `pkg/${CLOUD_SERVICE_PROVIDER}/${MODULE_NAME}` directory
   1. For example: `pkg/aws/eks/eks.go`
1. Implement the `Collector` [interface](https://github.com/grafana/cloudcost-exporter/blob/ff3267af66034dabb489bb30c76e115fcc24055f/pkg/provider/provider.go#L15-L21) in the new module
1. Create a `PricingMap` for the new module
   1. `PricingMap` should be a `map[string]Pricing`  where key is the region and Pricing is the cost of the resource in that region
   2. Gather pricing information from the cloud provider's pricing API
      3. For example: `pkg/aws/eks/pricing.go`
   3. Implement a cache for the `PricingMap` since pricing typically stays static for ~24 hours
1. Implement a `List{Resource}` function that will list all resources of the type
   1. For example: `pkg/aws/eks/instances.go`
   2. The function should return a list of `Resource` structs
3. Implement a `GetCost` function that will calculate the cost of the resource
   1. For example: `pkg/aws/eks/cost.go`
   2. The function should return the cost of the resource
3. Implement a `GetLabels` function that will return the labels for the resource
   1. For example: `pkg/aws/eks/labels.go`
   2. The function should return the labels for the resource

