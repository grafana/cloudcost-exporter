# Supported Services

Each bullet shows the human-readable name, the flag value to pass via `-{provider}.services` (in backticks), and a short description. Run `cloudcost-exporter -list-services` to see this list from the CLI.

## AWS Services

- **[EC2](./aws/ec2.md)** (`EC2`): Elastic Compute Cloud instances (spot and on-demand pricing)
- **[S3](./aws/s3.md)** (`S3`): Simple Storage Service buckets
- **[RDS](./aws/rds.md)** (`RDS`): Relational Database Service instances
- **[MSK](./aws/msk.md)** (`MSK`): Managed Service for Apache Kafka clusters
- **[ELB](./aws/elb.md)** (`ELB`): Elastic Load Balancers (ALB, NLB)
- **[NAT Gateway](./aws/natgateway.md)** (`NATGATEWAY`): Network Address Translation gateways
- **[VPC](./aws/vpc.md)** (`VPC`): VPC endpoints and services
- **[Bedrock](./aws/bedrock.md)** (`BEDROCK`): Foundation model token and search-unit pricing (first-party and Marketplace)

## GCP Services

- **[GKE](./gcp/gke.md)** (`GKE`): Google Kubernetes Engine clusters
- **[GCS](./gcp/gcs.md)** (`GCS`): Google Cloud Storage buckets
- **[Cloud SQL](./gcp/cloudsql.md)** (`SQL`): Managed database instances
- **[Managed Kafka](./gcp/managedkafka.md)** (`MANAGEDKAFKA`, alias: `KAFKA`): Managed Service for Apache Kafka clusters
- **[CLB](./gcp/clb.md)** (`CLB`): Cloud Load Balancers via forwarding rules
- **[VPC](./gcp/vpc.md)** (`VPC`): Cloud NAT Gateway, VPN Gateway, Private Service Connect
- **[Vertex AI](./gcp/vertex.md)** (`VERTEX`): Vertex AI model token, character, compute, and reranking pricing

## Azure Services

- **[AKS](./azure/aks.md)** (`AKS`): Azure Kubernetes Service VM instances and managed disks
- **[Blob](./azure/blob.md)** (`blob`): Azure Blob Storage (cost metrics registered; no series until Cost Management)
- **[Event Hubs](./azure/eventhubs.md)** (`EVENTHUBS`, alias: `EVENTHUB`): Kafka-compatible Azure Event Hubs namespaces
