package eventhubs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azmetrics"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/eventhub/armeventhub"
	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/azure/client"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	subsystem     = "azure_eventhubs"
	collectorName = "eventhubs"

	retailPriceAPIVersion = "2023-01-01-preview"

	eventHubsServiceName       = "Event Hubs"
	storageServiceName         = "Storage"
	blobStorageProductName     = "Blob Storage"
	generalBlockBlobProductV2  = "General Block Blob v2"
	generalBlockBlobProduct    = "General Block Blob"
	standardSKUName            = "Standard"
	standardThroughputMeter    = "Standard Throughput Unit"
	standardKafkaEndpointMeter = "Standard Kafka Endpoint"
	standardIngressMeter       = "Standard Ingress Events"
	hotLRSSKUName              = "Hot LRS"
	hotLRSDataStoredMeter      = "Hot LRS Data Stored"

	eventHubsMetricNamespace = "Microsoft.EventHub/namespaces"
	incomingBytesMetricName  = "IncomingBytes"
	incomingMessagesMetric   = "IncomingMessages"
	sizeMetricName           = "Size"

	metricsLookback = time.Hour
	metricsInterval = "PT1M"
	maxMetricsBatch = 50

	billableIngressEventBytes          = 64 * 1000
	includedStorageGBPerThroughputUnit = 84
	bytesPerGB                         = 1000 * 1000 * 1000
)

var (
	ComputeHourlyGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"compute_hourly_rate_usd_per_hour",
		"Hourly compute cost of an Azure Event Hubs namespace. Cost represented in USD/hour",
		[]string{"region", "namespace_name", "namespace", "sku"},
	)
	StorageHourlyGaugeDesc = utils.GenerateDesc(
		cloudcostexporter.MetricPrefix,
		subsystem,
		"storage_hourly_rate_usd_per_hour",
		"Hourly storage cost of an Azure Event Hubs namespace. Cost represented in USD/hour",
		[]string{"region", "namespace_name", "namespace", "sku"},
	)
)

type Config struct {
	Logger *slog.Logger
}

type Collector struct {
	logger      *slog.Logger
	azureClient client.AzureClient
	pricingMap  *pricingMap
}

type namespacePricingData struct {
	region        string
	namespaceName string
	namespace     string
	sku           string
	capacity      int32
}

type namespaceUsage struct {
	ingressBillableUnits float64
	averageSizeBytes     float64
}

type incomingBucket struct {
	bytes    float64
	messages float64
}

func New(_ context.Context, cfg *Config, azureClient client.AzureClient) (*Collector, error) {
	logger := slog.Default()
	if cfg != nil && cfg.Logger != nil {
		logger = cfg.Logger
	}

	logger = logger.With("collector", collectorName)

	return &Collector{
		logger:      logger,
		azureClient: azureClient,
		pricingMap:  newPricingMap(logger, azureClient),
	}, nil
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) error {
	ch <- ComputeHourlyGaugeDesc
	ch <- StorageHourlyGaugeDesc
	return nil
}

func (c *Collector) Collect(ctx context.Context, ch chan<- prometheus.Metric) error {
	namespaces, err := c.azureClient.ListEventHubNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("failed to list Event Hubs namespaces: %w", err)
	}

	standardNamespaces := make([]namespacePricingData, 0, len(namespaces))
	for _, namespace := range namespaces {
		namespaceData, err := buildNamespacePricingData(namespace)
		if err != nil {
			c.logger.Warn(
				"skipping unsupported or incomplete Event Hubs namespace",
				"namespace", armString(namespace.Name),
				"error", err,
			)
			continue
		}

		standardNamespaces = append(standardNamespaces, namespaceData)
	}

	if len(standardNamespaces) == 0 {
		return nil
	}

	if err := c.pricingMap.RefreshIfNeeded(ctx, uniqueRegions(standardNamespaces)); err != nil {
		return fmt.Errorf("failed to refresh Event Hubs pricing: %w", err)
	}
	snapshot := c.pricingMap.Snapshot()

	usageByNamespace, failedRegions := c.collectUsage(ctx, standardNamespaces)

	var collectErrs []error
	for _, namespace := range standardNamespaces {
		if regionErr, failed := failedRegions[namespace.region]; failed {
			c.logger.Warn(
				"skipping Event Hubs namespace due to usage query failure",
				"region", namespace.region,
				"namespace", namespace.namespace,
				"error", regionErr,
			)
			continue
		}

		throughputUnitHourly, ok := snapshot.Price(namespace.region, throughputUnitComponent)
		if !ok {
			c.logger.Warn(
				"skipping Event Hubs namespace with missing throughput unit pricing",
				"region", namespace.region,
				"namespace", namespace.namespace,
			)
			continue
		}
		kafkaEndpointHourly, ok := snapshot.Price(namespace.region, kafkaEndpointComponent)
		if !ok {
			c.logger.Warn(
				"skipping Event Hubs namespace with missing Kafka endpoint pricing",
				"region", namespace.region,
				"namespace", namespace.namespace,
			)
			continue
		}
		ingressPricePerMillion, ok := snapshot.Price(namespace.region, ingressComponent)
		if !ok {
			c.logger.Warn(
				"skipping Event Hubs namespace with missing ingress pricing",
				"region", namespace.region,
				"namespace", namespace.namespace,
			)
			continue
		}
		blobStoragePricePerGBMonth, ok := snapshot.Price(namespace.region, blobStorageComponent)
		if !ok {
			c.logger.Warn(
				"skipping Event Hubs namespace with missing Blob Storage pricing",
				"region", namespace.region,
				"namespace", namespace.namespace,
			)
			continue
		}

		usage := usageByNamespace[usageKey(namespace.namespace)]

		computeHourlyRate := throughputUnitHourly * float64(namespace.capacity)
		computeHourlyRate += kafkaEndpointHourly
		// TODO: Consider splitting ingress into its own metric so the collector as flattening
		//       ingress events pricing into "compute" metric might be a little confusing.
		computeHourlyRate += (usage.ingressBillableUnits * ingressPricePerMillion) / 1_000_000

		allowanceGB := float64(namespace.capacity) * includedStorageGBPerThroughputUnit
		averageStoredGB := usage.averageSizeBytes / bytesPerGB
		storageOverageGB := math.Max(0, averageStoredGB-allowanceGB)
		storageHourlyRate := (storageOverageGB * blobStoragePricePerGBMonth) / utils.HoursInMonth

		ch <- prometheus.MustNewConstMetric(
			ComputeHourlyGaugeDesc,
			prometheus.GaugeValue,
			computeHourlyRate,
			namespace.region,
			namespace.namespaceName,
			namespace.namespace,
			namespace.sku,
		)
		ch <- prometheus.MustNewConstMetric(
			StorageHourlyGaugeDesc,
			prometheus.GaugeValue,
			storageHourlyRate,
			namespace.region,
			namespace.namespaceName,
			namespace.namespace,
			namespace.sku,
		)
	}

	for region, err := range failedRegions {
		collectErrs = append(collectErrs, fmt.Errorf("failed to collect Event Hubs metrics for region %s: %w", region, err))
	}

	if len(collectErrs) > 0 {
		return errors.Join(collectErrs...)
	}

	return nil
}

func (c *Collector) Name() string {
	return collectorName
}

func (c *Collector) Register(provider.Registry) error {
	return nil
}

func buildNamespacePricingData(namespace *armeventhub.EHNamespace) (namespacePricingData, error) {
	if namespace == nil {
		return namespacePricingData{}, fmt.Errorf("namespace is nil")
	}

	namespaceID := armString(namespace.ID)
	if namespaceID == "" {
		return namespacePricingData{}, fmt.Errorf("namespace ID is missing")
	}

	namespaceName := armString(namespace.Name)
	if namespaceName == "" {
		return namespacePricingData{}, fmt.Errorf("namespace name is missing")
	}

	region := strings.ToLower(strings.TrimSpace(armString(namespace.Location)))
	if region == "" {
		return namespacePricingData{}, fmt.Errorf("namespace location is missing")
	}

	if namespace.SKU == nil || namespace.SKU.Name == nil {
		return namespacePricingData{}, fmt.Errorf("namespace SKU is missing")
	}

	sku := string(*namespace.SKU.Name)
	if !strings.EqualFold(sku, standardSKUName) {
		return namespacePricingData{}, fmt.Errorf("SKU %q is not supported", sku)
	}

	if namespace.SKU.Capacity == nil || *namespace.SKU.Capacity <= 0 {
		return namespacePricingData{}, fmt.Errorf("namespace capacity is missing")
	}

	if namespace.Properties != nil {
		if namespace.Properties.KafkaEnabled != nil && !*namespace.Properties.KafkaEnabled {
			return namespacePricingData{}, fmt.Errorf("namespace is not Kafka-enabled")
		}

		if namespace.Properties.IsAutoInflateEnabled != nil && *namespace.Properties.IsAutoInflateEnabled {
			return namespacePricingData{}, fmt.Errorf("auto-inflate namespaces are not supported")
		}
	}

	return namespacePricingData{
		region:        region,
		namespaceName: namespaceName,
		namespace:     namespaceID,
		sku:           sku,
		capacity:      *namespace.SKU.Capacity,
	}, nil
}

func (c *Collector) collectUsage(ctx context.Context, namespaces []namespacePricingData) (map[string]namespaceUsage, map[string]error) {
	usageByNamespace := make(map[string]namespaceUsage, len(namespaces))
	failedRegions := make(map[string]error)

	grouped := make(map[string][]string)
	for _, namespace := range namespaces {
		grouped[namespace.region] = append(grouped[namespace.region], namespace.namespace)
	}

	for region, resourceIDs := range grouped {
		incomingUsage, err := c.collectIncomingUsage(ctx, region, resourceIDs)
		if err != nil {
			failedRegions[region] = err
			continue
		}

		sizeUsage, err := c.collectSizeUsage(ctx, region, resourceIDs)
		if err != nil {
			failedRegions[region] = err
			continue
		}

		for key, usage := range incomingUsage {
			current := usageByNamespace[key]
			current.ingressBillableUnits = usage.ingressBillableUnits
			usageByNamespace[key] = current
		}

		for key, averageSizeBytes := range sizeUsage {
			current := usageByNamespace[key]
			current.averageSizeBytes = averageSizeBytes
			usageByNamespace[key] = current
		}
	}

	return usageByNamespace, failedRegions
}

func (c *Collector) collectIncomingUsage(ctx context.Context, region string, resourceIDs []string) (map[string]namespaceUsage, error) {
	usage := make(map[string]namespaceUsage, len(resourceIDs))

	for _, batch := range chunkStrings(resourceIDs, maxMetricsBatch) {
		response, err := c.azureClient.QueryResourceMetrics(
			ctx,
			region,
			eventHubsMetricNamespace,
			[]string{incomingBytesMetricName, incomingMessagesMetric},
			batch,
			metricQueryOptions("total"),
		)
		if err != nil {
			return nil, err
		}

		batchUsage, err := parseIncomingUsage(response)
		if err != nil {
			return nil, err
		}

		for key, namespaceUsage := range batchUsage {
			usage[key] = namespaceUsage
		}
	}

	return usage, nil
}

func (c *Collector) collectSizeUsage(ctx context.Context, region string, resourceIDs []string) (map[string]float64, error) {
	usage := make(map[string]float64, len(resourceIDs))

	for _, batch := range chunkStrings(resourceIDs, maxMetricsBatch) {
		response, err := c.azureClient.QueryResourceMetrics(
			ctx,
			region,
			eventHubsMetricNamespace,
			[]string{sizeMetricName},
			batch,
			metricQueryOptions("average"),
		)
		if err != nil {
			return nil, err
		}

		batchUsage, err := parseSizeUsage(response)
		if err != nil {
			return nil, err
		}

		for key, averageSizeBytes := range batchUsage {
			usage[key] = averageSizeBytes
		}
	}

	return usage, nil
}

func parseIncomingUsage(response azmetrics.QueryResourcesResponse) (map[string]namespaceUsage, error) {
	usageByNamespace := make(map[string]namespaceUsage, len(response.Values))

	for _, metricData := range response.Values {
		if metricData.ResourceID == nil || *metricData.ResourceID == "" {
			return nil, fmt.Errorf("metrics response is missing resource ID")
		}

		buckets := make(map[time.Time]incomingBucket)
		for _, metric := range metricData.Values {
			metricName := localizableValue(metric.Name)
			switch metricName {
			case incomingBytesMetricName:
				accumulateIncomingTotals(metric.TimeSeries, buckets, true)
			case incomingMessagesMetric:
				accumulateIncomingTotals(metric.TimeSeries, buckets, false)
			}
		}

		namespaceUsage := namespaceUsage{}
		for _, bucket := range buckets {
			billableByBytes := math.Ceil(bucket.bytes / billableIngressEventBytes)
			namespaceUsage.ingressBillableUnits += math.Max(bucket.messages, billableByBytes)
		}

		usageByNamespace[usageKey(*metricData.ResourceID)] = namespaceUsage
	}

	return usageByNamespace, nil
}

func parseSizeUsage(response azmetrics.QueryResourcesResponse) (map[string]float64, error) {
	usageByNamespace := make(map[string]float64, len(response.Values))

	for _, metricData := range response.Values {
		if metricData.ResourceID == nil || *metricData.ResourceID == "" {
			return nil, fmt.Errorf("metrics response is missing resource ID")
		}

		sizeByMinute := make(map[time.Time]float64)
		for _, metric := range metricData.Values {
			if localizableValue(metric.Name) != sizeMetricName {
				continue
			}

			for _, series := range metric.TimeSeries {
				for _, datapoint := range series.Data {
					if datapoint.TimeStamp == nil || datapoint.Average == nil {
						continue
					}

					sizeByMinute[datapoint.TimeStamp.UTC()] += *datapoint.Average
				}
			}
		}

		if len(sizeByMinute) == 0 {
			usageByNamespace[usageKey(*metricData.ResourceID)] = 0
			continue
		}

		var totalSize float64
		for _, size := range sizeByMinute {
			totalSize += size
		}

		usageByNamespace[usageKey(*metricData.ResourceID)] = totalSize / float64(len(sizeByMinute))
	}

	return usageByNamespace, nil
}

func accumulateIncomingTotals(series []azmetrics.TimeSeriesElement, buckets map[time.Time]incomingBucket, isBytes bool) {
	for _, timeSeries := range series {
		for _, datapoint := range timeSeries.Data {
			if datapoint.TimeStamp == nil || datapoint.Total == nil {
				continue
			}

			bucket := buckets[datapoint.TimeStamp.UTC()]
			if isBytes {
				bucket.bytes += *datapoint.Total
			} else {
				bucket.messages += *datapoint.Total
			}
			buckets[datapoint.TimeStamp.UTC()] = bucket
		}
	}
}

func metricQueryOptions(aggregation string) *azmetrics.QueryResourcesOptions {
	end := time.Now().UTC()
	start := end.Add(-metricsLookback)

	return &azmetrics.QueryResourcesOptions{
		Aggregation: to.Ptr(aggregation),
		StartTime:   to.Ptr(formatMetricTime(start)),
		EndTime:     to.Ptr(formatMetricTime(end)),
		Interval:    to.Ptr(metricsInterval),
	}
}

func formatMetricTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

func blobProductPreference(productName string) (int, bool) {
	switch productName {
	case blobStorageProductName:
		return 0, true
	case generalBlockBlobProductV2:
		return 1, true
	case generalBlockBlobProduct:
		return 2, true
	default:
		return 0, false
	}
}

func armRegionFilter(regions []string, includeGlobal bool) string {
	clauses := make([]string, 0, len(regions)+1)
	for _, region := range uniqueStrings(regions) {
		clauses = append(clauses, fmt.Sprintf("armRegionName eq '%s'", region))
	}
	if includeGlobal {
		clauses = append(clauses, "armRegionName eq 'Global'")
	}
	return strings.Join(clauses, " or ")
}

func uniqueRegions(namespaces []namespacePricingData) []string {
	regions := make([]string, 0, len(namespaces))
	for _, namespace := range namespaces {
		regions = append(regions, namespace.region)
	}
	return uniqueStrings(regions)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func chunkStrings(values []string, size int) [][]string {
	if len(values) == 0 {
		return nil
	}
	if size <= 0 || len(values) <= size {
		return [][]string{values}
	}

	chunks := make([][]string, 0, (len(values)+size-1)/size)
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[start:end])
	}
	return chunks
}

func localizableValue(value *azmetrics.LocalizableString) string {
	if value == nil || value.Value == nil {
		return ""
	}
	return *value.Value
}

func usageKey(resourceID string) string {
	return strings.ToLower(strings.TrimSpace(resourceID))
}

func armString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func maxFloat(current, candidate float64) float64 {
	if candidate > current {
		return candidate
	}
	return current
}
