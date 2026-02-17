# CloudCost Exporter Call Diagram

This diagram shows the architecture and call flow of the cloudcost-exporter application.

## Main Application Flow

```mermaid
graph TB
    Start([Application Start]) --> Main[main.go<br/>main]

    Main --> ProviderFlags[providerFlags<br/>Parse CLI flags]
    Main --> OperationalFlags[operationalFlags<br/>Parse operational flags]
    Main --> SetupLogger[setupLogger<br/>Initialize logger]
    Main --> SelectProvider[selectProvider<br/>Choose provider]
    Main --> RunServer[runServer<br/>Start HTTP server]

    SelectProvider --> |provider=aws| AWSNew[aws.New]
    SelectProvider --> |provider=gcp| GCPNew[google.New]
    SelectProvider --> |provider=azure| AzureNew[azure.New]

    RunServer --> CreateRegistry[createPromRegistryHandler<br/>Setup Prometheus]
    CreateRegistry --> RegisterCollectors[Provider.RegisterCollectors]

    RunServer --> HTTPServer[HTTP Server<br/>:8080/metrics]
    HTTPServer --> PrometheusHandler[Prometheus Handler]
    PrometheusHandler --> ProviderCollect[Provider.Collect]

    style Start fill:#90EE90
    style Main fill:#FFE4B5
    style HTTPServer fill:#87CEEB
```

## Provider Architecture

```mermaid
graph TB
    Provider[Provider Interface<br/>prometheus.Collector] --> AWSProvider[AWS Provider]
    Provider --> GCPProvider[GCP Provider]
    Provider --> AzureProvider[Azure Provider]

    subgraph "AWS Provider"
        AWSProvider --> AWSClient[AWS Client<br/>SDK Wrapper]
        AWSProvider --> AWSS3[S3 Collector]
        AWSProvider --> AWSEC2[EC2 Collector]
        AWSProvider --> AWSRDS[RDS Collector]
        AWSProvider --> AWSNAT[NAT Gateway Collector]
        AWSProvider --> AWSELB[ELB Collector]
        AWSProvider --> AWSVPC[VPC Collector]

        AWSClient --> EC2Service[EC2 Service]
        AWSClient --> PricingService[Pricing Service]
        AWSClient --> BillingService[Cost Explorer]
        AWSClient --> RDSService[RDS Service]
        AWSClient --> ELBService[ELB Service]
    end

    subgraph "GCP Provider"
        GCPProvider --> GCPClient[GCP Client<br/>SDK Wrapper]
        GCPProvider --> GCS[GCS Collector]
        GCPProvider --> GKE[GKE Collector]
        GCPProvider --> CLB[Cloud LB Collector]
        GCPProvider --> GCPVPC[VPC Collector]
        GCPProvider --> CloudSQL[Cloud SQL Collector]

        GCPClient --> BillingAPI[Billing API]
        GCPClient --> ComputeAPI[Compute API]
        GCPClient --> StorageAPI[Storage API]
    end

    subgraph "Azure Provider"
        AzureProvider --> AzureClient[Azure Client<br/>SDK Wrapper]
        AzureProvider --> AKS[AKS Collector]

        AzureClient --> AzureCredentials[Azure Credentials]
        AzureClient --> ContainerAPI[Container Service API]
    end

    style Provider fill:#FFD700
    style AWSProvider fill:#FF9900
    style GCPProvider fill:#4285F4
    style AzureProvider fill:#0078D4
```

## Collector Pattern

```mermaid
graph TB
    CollectorInterface[Collector Interface] --> Register[Register<br/>Register metrics]
    CollectorInterface --> Describe[Describe<br/>Describe metrics]
    CollectorInterface --> Collect[Collect<br/>Collect metrics]
    CollectorInterface --> CollectMetrics[CollectMetrics<br/>Collect with timeout]

    Collect --> |AWS| EC2Collect[EC2 Collector.Collect]
    Collect --> |AWS| S3Collect[S3 Collector.Collect]
    Collect --> |AWS| RDSCollect[RDS Collector.Collect]
    Collect --> |AWS| NATCollect[NAT Gateway Collector.Collect]
    Collect --> |AWS| ELBCollect[ELB Collector.Collect]
    Collect --> |AWS| VPCCollect[VPC Collector.Collect]

    Collect --> |GCP| GCSCollect[GCS Collector.Collect]
    Collect --> |GCP| GKECollect[GKE Collector.Collect]
    Collect --> |GCP| CLBCollect[CLB Collector.Collect]
    Collect --> |GCP| GCPVPCCollect[GCP VPC Collector.Collect]
    Collect --> |GCP| SQLCollect[Cloud SQL Collector.Collect]

    Collect --> |Azure| AKSCollect[AKS Collector.Collect]

    EC2Collect --> PrometheusMetrics[Prometheus Metrics Channel]
    S3Collect --> PrometheusMetrics
    RDSCollect --> PrometheusMetrics
    GCSCollect --> PrometheusMetrics
    GKECollect --> PrometheusMetrics
    AKSCollect --> PrometheusMetrics

    style CollectorInterface fill:#DDA0DD
    style PrometheusMetrics fill:#98FB98
```

## AWS Collector Detail

```mermaid
graph TB
    AWS[AWS Provider] --> |New| CreateAWSConfig[createAWSConfig<br/>Setup AWS SDK]
    AWS --> |New| CreateClient[NewAWSClient<br/>Initialize services]
    AWS --> |New| DescribeRegions[DescribeRegions<br/>Get available regions]
    AWS --> |New| CreateRegionClients[newRegionClientMap<br/>Per-region clients]

    CreateAWSConfig --> |RoleARN?| AssumeRole[assumeRole<br/>STS assume role]

    CreateClient --> EC2SDK[EC2 SDK Client]
    CreateClient --> PricingSDK[Pricing SDK Client]
    CreateClient --> CostExplorerSDK[Cost Explorer SDK]
    CreateClient --> RDSSDK[RDS SDK Client]
    CreateClient --> ELBSDK[ELB SDK Client]

    AWS --> RegisterAWS[RegisterCollectors]
    AWS --> DescribeAWS[Describe]
    AWS --> CollectAWS[Collect<br/>Parallel collection]

    CollectAWS --> |goroutine| EC2Goroutine[EC2 Collector]
    CollectAWS --> |goroutine| S3Goroutine[S3 Collector]
    CollectAWS --> |goroutine| RDSGoroutine[RDS Collector]
    CollectAWS --> |goroutine| NATGoroutine[NAT Gateway Collector]
    CollectAWS --> |goroutine| ELBGoroutine[ELB Collector]
    CollectAWS --> |goroutine| VPCGoroutine[VPC Collector]

    EC2Goroutine --> MetricsChannel[chan prometheus.Metric]
    S3Goroutine --> MetricsChannel
    RDSGoroutine --> MetricsChannel
    NATGoroutine --> MetricsChannel
    ELBGoroutine --> MetricsChannel
    VPCGoroutine --> MetricsChannel

    style AWS fill:#FF9900
    style MetricsChannel fill:#98FB98
```

## GCP Collector Detail

```mermaid
graph TB
    GCP[GCP Provider] --> |New| CreateGCPClient[NewGCPClient<br/>Initialize GCP SDK]

    CreateGCPClient --> BillingClient[Billing Client]
    CreateGCPClient --> ComputeClient[Compute Client]
    CreateGCPClient --> StorageClient[Storage Client]
    CreateGCPClient --> ContainerClient[Container Client]

    GCP --> RegisterGCP[RegisterCollectors]
    GCP --> DescribeGCP[Describe]
    GCP --> CollectGCP[Collect<br/>Parallel collection]

    CollectGCP --> |goroutine| GCSGoroutine[GCS Collector]
    CollectGCP --> |goroutine| GKEGoroutine[GKE Collector]
    CollectGCP --> |goroutine| CLBGoroutine[CLB/Networking Collector]
    CollectGCP --> |goroutine| VPCGoroutine[VPC Collector]
    CollectGCP --> |goroutine| SQLGoroutine[Cloud SQL Collector]

    GCSGoroutine --> GCPMetrics[chan prometheus.Metric]
    GKEGoroutine --> GCPMetrics
    CLBGoroutine --> GCPMetrics
    VPCGoroutine --> GCPMetrics
    SQLGoroutine --> GCPMetrics

    GKEGoroutine --> PricingMap[Pricing Map<br/>Disk/Machine pricing]
    GCSGoroutine --> BucketCache[Bucket Cache]

    style GCP fill:#4285F4
    style GCPMetrics fill:#98FB98
```

## Azure Collector Detail

```mermaid
graph TB
    Azure[Azure Provider] --> |New| CreateAzureCreds[NewDefaultAzureCredential<br/>Initialize credentials]
    Azure --> |New| CreateAzureClient[NewAzureClientWrapper<br/>SDK wrapper]

    CreateAzureCreds --> DefaultCreds[Azure Default Credentials]
    CreateAzureClient --> SubscriptionID[Subscription ID]

    Azure --> RegisterAzure[RegisterCollectors]
    Azure --> DescribeAzure[Describe]
    Azure --> CollectAzure[Collect<br/>Parallel collection]

    CollectAzure --> |goroutine| AKSGoroutine[AKS Collector]

    AKSGoroutine --> AzureMetrics[chan prometheus.Metric]
    AKSGoroutine --> MachineStore[Machine Store<br/>VM pricing]
    AKSGoroutine --> DiskStore[Disk Store<br/>Disk pricing]
    AKSGoroutine --> PriceStore[Price Store<br/>AKS pricing]

    style Azure fill:#0078D4
    style AzureMetrics fill:#98FB98
```

## HTTP Server Flow

```mermaid
sequenceDiagram
    participant Client
    participant HTTPServer
    participant PrometheusHandler
    participant Provider
    participant Collectors
    participant Metrics

    Client->>HTTPServer: GET /metrics
    HTTPServer->>PrometheusHandler: Handle request
    PrometheusHandler->>Provider: Collect()

    activate Provider
    Provider->>Provider: Create timeout context

    par Parallel Collection
        Provider->>Collectors: EC2.Collect()
        Provider->>Collectors: S3.Collect()
        Provider->>Collectors: RDS.Collect()
        Provider->>Collectors: GKE.Collect()
        Provider->>Collectors: AKS.Collect()
    end

    Collectors->>Metrics: Send metrics to channel
    Metrics->>Provider: Aggregate metrics
    deactivate Provider

    Provider->>PrometheusHandler: Return metrics
    PrometheusHandler->>HTTPServer: Format as Prometheus output
    HTTPServer->>Client: 200 OK with metrics
```

## Configuration Flow

```mermaid
graph LR
    CLI[CLI Flags] --> Config[Config Struct]

    Config --> ProviderType[Provider Type<br/>aws/gcp/azure]
    Config --> Services[Services List<br/>EC2,S3,RDS,etc]
    Config --> Region[Region/Location]
    Config --> Credentials[Credentials<br/>Profile/RoleARN/SubscriptionID]
    Config --> Operational[Operational Settings<br/>Intervals/Timeouts]
    Config --> Logger[Logger Config<br/>Level/Output/Type]

    ProviderType --> ProviderFactory[Provider Factory<br/>selectProvider]
    Services --> CollectorFactory[Collector Factory<br/>Per service]
    Credentials --> SDKConfig[SDK Configuration]

    ProviderFactory --> ProviderInstance[Provider Instance]
    CollectorFactory --> CollectorInstances[Collector Instances]

    ProviderInstance --> RegisterToPrometheus[Register to Prometheus]
    CollectorInstances --> RegisterToPrometheus

    style Config fill:#FFE4B5
    style RegisterToPrometheus fill:#90EE90
```

## Key Components Summary

### Entry Points
- `cmd/exporter/exporter.go` - Main application entry
- `main()` - Orchestrates setup and server start

### Core Packages
- `pkg/provider` - Provider interface definition
- `pkg/aws` - AWS provider implementation
- `pkg/google` - GCP provider implementation
- `pkg/azure` - Azure provider implementation
- `pkg/logger` - Logging utilities
- `pkg/utils` - Shared utilities

### Service Collectors
#### AWS
- `pkg/aws/s3` - S3 bucket metrics
- `pkg/aws/ec2` - EC2 instance metrics
- `pkg/aws/rds` - RDS database metrics
- `pkg/aws/natgateway` - NAT Gateway metrics
- `pkg/aws/elb` - Elastic Load Balancer metrics
- `pkg/aws/vpc` - VPC metrics

#### GCP
- `pkg/google/gcs` - Cloud Storage metrics
- `pkg/google/gke` - Kubernetes Engine metrics
- `pkg/google/networking` - Cloud Load Balancer metrics
- `pkg/google/vpc` - VPC metrics
- `pkg/google/cloudsql` - Cloud SQL metrics

#### Azure
- `pkg/azure/aks` - Azure Kubernetes Service metrics

### Client Wrappers
- `pkg/aws/client` - AWS SDK wrapper
- `pkg/google/client` - GCP SDK wrapper
- `pkg/azure/client` - Azure SDK wrapper

### Support Components
- `cmd/exporter/config` - Configuration management
- `cmd/exporter/web` - Web handlers
- Prometheus integration via `prometheus/client_golang`
