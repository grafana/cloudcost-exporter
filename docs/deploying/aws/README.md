# AWS CloudCost Exporter Deployment

## Authentication

cloudcost-exporter uses [AWS SDK for Go V2](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/getting-started.html) and supports providing authentication via the [AWS SDK's default credential provider chain](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/security_iam_service-with-iam.html).
This means that the CloudCost Exporter can be deployed on an EC2 instance, ECS, EKS, or any other AWS service that supports IAM roles for service accounts.

You will need to create a [service role](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/security_iam_service-with-iam.html#security_iam_service-with-iam-roles-service) and its policy.
Below are some options for how to create one.

### Option 1: AWS Console

First, create a policy with the following JSON (see: [./permissions-policy.json](./permissions-policy.json)):
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

To create a role, for `Trusted entity type`, select `AWS service`.
For `Use case`, select `EC2`.
For the policy, select the one that was created in the earlier step.
The trust policy should look like this (see: [./role-trust-policy.json](./role-trust-policy.json)):
```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "sts:AssumeRole"
            ],
            "Principal": {
                "Service": [
                    "ec2.amazonaws.com"
                ]
            }
        }
    ]
}
```

### Option 2: AWS CLI

Follow the AWS docs for [how to create a role for a service using the AWS CLI](https://docs.aws.amazon.com/IAM/latest/UserGuide/id_roles_create_for-service.html#roles-creatingrole-service-cli).

The trust policy can be found in [./role-trust-policy.json](./role-trust-policy.json).
The permissions policy can be found in [./permissions-policy.json](./permissions-policy.json).

## Helm chart

The Helm chart can be deployed after creating the necessary role and policy described above in [Authentication](#authentication).
In the [values.yaml](../../../deploy/helm/cloudcost-exporter/values.yaml) file, annotate the `serviceAccount` with the ARN of the role created above.
This should look like the following:

```yaml
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/CloudCostExporterRole
```

An example values file is provided [here](../../.././deploy/helm/cloudcost-exporter/values.aws.yaml).
The AWS-specific values can be used along the main values like this:
```console
helm install my-release ./deploy/helm/cloudcost-exporter \
--values ./deploy/helm/cloudcost-exporter/values.yaml \
--values ./deploy/helm/cloudcost-exporter/values.aws.yaml
```
