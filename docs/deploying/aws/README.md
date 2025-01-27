# AWS CloudCost Exporter Deployment

## Authentication

cloudcost-exporter uses [AWS SDK for Go V2](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/getting-started.html) and supports providing authentication via the [AWS SDK's default credential provider chain](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/security_iam_service-with-iam.html).
This means that the CloudCost Exporter can be deployed on an EC2 instance, ECS, EKS, or any other AWS service that supports IAM roles for service accounts.
The following role needs to be created:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "ce:*",
                "ec2:DescribeRegions",
                "ec2:DescribeInstances",
                "ec2:DescribeSpotPriceHistory",
                "ec2:DescribeVolumes",
                "pricing:GetProducts"
            ],
            "Resource": "*"
        }
    ]
}
```

This role needs to be attached to the EC2 instance, ECS task, or EKS pod that the CloudCost Exporter is running on.
