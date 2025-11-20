# Creating a New Module

This document outlines the process for creating a new module for the Cloud Cost Exporter.
The current architecture of the exporter is designed to be modular and extensible.

This guide is based on the NAT Gateway implementation (`pkg/aws/natgateway/`) which serves as a reference implementation.

## Steps:

### 1. Create Module Package Structure
Create a new module in the `pkg/${CLOUD_SERVICE_PROVIDER}/${MODULE_NAME}` directory with the following files:
- `${MODULE_NAME}.go` - Main collector implementation
- `${MODULE_NAME}_test.go` - Comprehensive tests
- For AWS modules: `${MODULE_NAME}_usage.go` - Information about the usage type and any additional filters for the Pricing API

For example: `pkg/aws/natgateway/natgateway.go`

### 2. Implement the Prometheus Collector

Implement the Prometheus `Collector` [interface](https://github.com/grafana/cloudcost-exporter/blob/main/pkg/provider/provider.go) in your main module file

Define the Prometheus metric descriptors for the service.

**Important: Context Handling**

The `Collect` method must accept `context.Context` as the first parameter:
```go
func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
    // Use ctx for all operations (API calls, data fetching, etc.)
    // This context has a timeout set by the provider and should be respected
    return nil
}
```

**Context Usage Pattern:**

1. **Collectors with background goroutines** (periodic pricing refresh, data population):
   - Must receive `ctx context.Context` as the first parameter in `New()`
   - Store the context in the struct for background operations
   - Use struct `ctx` for background goroutines (lifecycle management)
   - Use `Collect()` `ctx` parameter for collection work (timeout enforcement)
   - Example: `pkg/aws/vpc/vpc.go`, `pkg/google/vpc/vpc.go`, `pkg/azure/aks/aks.go`

2. **Collectors without background goroutines**:
   - Do NOT receive `ctx` in `New()` (not needed)
   - Only use the `ctx` parameter passed to `Collect()` method
   - Example: `pkg/aws/ec2/ec2.go`, `pkg/aws/s3/s3.go`, `pkg/aws/rds/rds.go`

**Why this matters:**
- Background goroutines need a lifecycle context to be cancelled when the provider shuts down
- Collection work needs a timeout context to prevent runaway external requests
- This ensures proper resource cleanup and prevents leaks

### 3. Implement the Pricing Integration

#### AWS

For AWS, the pricing information is fetched from the Pricing API for most services. S3 is an exception to this rule, where it uses the Billing API.
The Pricing API is the preferred method for gathering pricing information since it is [free of charge](https://aws.amazon.com/blogs/aws/aws-price-list-api-update-regional-price-lists/).
The Billing API incurs a charge of $0.01 per call.

To see what information is available via the Pricing API, download the offer index as specified [here](https://aws.amazon.com/blogs/aws/aws-price-list-api-update-regional-price-lists/).
Find the `productFamily` of the module to add.
There, you will find the different `usageType`s available for that Product Family.
Implement a Prometheus metric for each relevant `usageType`.
To understand how the Pricing Store works, copy the `sku` and search for the matching price for that unit.

To implement the pricing integration for AWS, define the Service Type and any additional filters to fetch pricing information for the new module.
This is usually the Product Family of the new module.
The module should use the shared `pricingmap.PricingStore`.
Implement a cache for this since pricing typically stays static for ~24 hours.

### 4. Optional: Implement the Machine Store Integration

Some services only need the price-per-region unit costs. One of these services is the NAT Gateway service.
For other services (the majority of services), we need to fetch the instance ID as an additional label.

### 5. Testing

Generate the mocks: `make generate`.

Create table-driven tests using these mocks.
