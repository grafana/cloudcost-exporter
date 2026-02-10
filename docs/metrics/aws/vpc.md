# AWS VPC Metrics

| Metric name                                                     | Metric type | Description                                                                  | Labels                                                                       |
|-----------------------------------------------------------------|-------------|------------------------------------------------------------------------------|------------------------------------------------------------------------------|
| cloudcost_aws_vpc_endpoint_hourly_rate_usd_per_hour             | Gauge       | Hourly cost of standard VPC endpoints. Cost represented in USD/hour          | `region`=&lt;AWS region&gt; <br/> `endpoint_type`=&lt;endpoint type&gt;     |
| cloudcost_aws_vpc_endpoint_service_hourly_rate_usd_per_hour     | Gauge       | Hourly cost of service-specific VPC endpoints. Cost represented in USD/hour  | `region`=&lt;AWS region&gt;                                                  |
| cloudcost_aws_vpc_transit_gateway_hourly_rate_usd_per_hour      | Gauge       | Hourly cost of Transit Gateway attachments. Cost represented in USD/hour     | `region`=&lt;AWS region&gt;                                                  |
| cloudcost_aws_vpc_elastic_ip_in_use_hourly_rate_usd_per_hour    | Gauge       | Hourly cost of in-use Elastic IP addresses. Cost represented in USD/hour     | `region`=&lt;AWS region&gt;                                                  |
| cloudcost_aws_vpc_elastic_ip_idle_hourly_rate_usd_per_hour      | Gauge       | Hourly cost of idle Elastic IP addresses. Cost represented in USD/hour       | `region`=&lt;AWS region&gt;                                                  |

## Overview

The VPC collector exports pricing metrics for AWS Virtual Private Cloud services, including VPC endpoints, Transit Gateway attachments, and Elastic IP addresses.

## Supported Services

### VPC Endpoints

The module exports two separate metrics for VPC endpoints to reflect AWS's dual pricing structure:

- **Standard endpoints** (`endpoint_type="standard"`): Basic interface endpoints for AWS services (~$0.01/hour)
- **Service endpoints**: Enhanced service-specific endpoints with additional features (~$0.05/hour)

### Transit Gateway

Network transit hub attachments that allow you to connect multiple VPCs and on-premises networks.

### Elastic IP Addresses

Both in-use and idle Elastic IP addresses are tracked separately:
- **In-use**: Elastic IPs attached to running instances
- **Idle**: Elastic IPs not currently attached to any instance (typically more expensive to encourage efficient resource use)

## Configuration

Enable the VPC collector by adding `vpc` to your AWS services configuration:

```yaml
aws:
  services: ["ec2", "s3", "vpc"]
  regions: ["us-east-1", "us-west-2"]
```

Or via command line:
```bash
--aws.services=ec2,s3,vpc
```

## Default Rates

If pricing data cannot be fetched from the AWS Pricing API, the following default rates are used:

- VPC Endpoints (Standard): $0.01/hour
- VPC Endpoints (Service): $0.05/hour
- Transit Gateway: $0.05/hour
- Elastic IP (In Use): $0.005/hour
- Elastic IP (Idle): $0.005/hour

## Notes

- Pricing data is fetched from the AWS Pricing API using the `AmazonVPC` service code
- All pricing API calls use the `us-east-1` region (AWS Pricing API requirement)
- Metrics are automatically refreshed every 24 hours
- All costs are represented in USD per hour

## IAM Permissions

Required permissions for VPC metrics collection:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "ec2:DescribeVpcEndpoints",
                "ec2:DescribeTransitGateways",
                "ec2:DescribeAddresses"
            ],
            "Resource": "*"
        },
        {
            "Effect": "Allow",
            "Action": [
                "pricing:GetProducts"
            ],
            "Resource": "*"
        }
    ]
}
```

**Note:** These permissions allow the exporter to discover VPC endpoints, Transit Gateway attachments, and Elastic IP addresses in your AWS account and correlate them with pricing data.
