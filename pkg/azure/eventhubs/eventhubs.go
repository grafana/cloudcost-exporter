package eventhubs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/monitor/query/azmetrics"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/eventhub/armeventhub"
	cloudcostexporter "github.com/grafana/cloudcost-exporter"
	"github.com/grafana/cloudcost-exporter/pkg/azure/client"
	"github.com/grafana/cloudcost-exporter/pkg/provider"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	retailPriceSdk "gomodules.xyz/azure-retail-prices-sdk-for-go/sdk"
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

	metricsLookback  = time.Hour
	metricsInterval  = "PT1M"
	priceRefreshTTL  = 24 * time.Hour
	maxMetricsBatch  = 50
	globalRegionName = "global"

	billableIngressEventBytes         = 64 * 1000
	includedStorageGBPerThroughputUnit = 84
	bytesPerGB                        = 1000 * 1000 * 1000
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
	logger       *slog.Logger
	azureClient  client.AzureClient
	pricingStore *pricingStore
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

type regionPricing struct {
	throughputUnitHourly     float64
	kafkaEndpointHourly      float64
	ingressPricePerMillion   float64
	blobStoragePricePerGBMonth float64
}

type pricingStore struct {
	logger      *slog.Logger
	azureClient client.AzureClient

	mu         sync.RWMutex
	byRegion   map[string]regionPricing
	nextRefresh time.Time
}

func New(_ context.Context, cfg *Config, azureClient client.AzureClient) (*Collector, error) {
	logger := slog.Default()
	if cfg != nil && cfg.Logger != nil {
		logger = cfg.Logger
	}

	logger = logger.With("collector", collectorName)

	return &Collector{
		logger:       logger,
		azureClient:  azureClient,
		pricingStore: newPricingStore(logger, azureClient),
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

	if err := c.pricingStore.RefreshIfNeeded(ctx, uniqueRegions(standardNamespaces)); err != nil {
		return fmt.Errorf("failed to refresh Event Hubs pricing: %w", err)
	}

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

		prices, ok := c.pricingStore.PricesFor(namespace.region)
		if !ok {
			c.logger.Warn(
				"skipping Event Hubs namespace with missing pricing",
				"region", namespace.region,
				"namespace", namespace.namespace,
			)
			continue
		}

		usage := usageByNamespace[usageKey(namespace.namespace)]

		computeHourlyRate := prices.throughputUnitHourly * float64(namespace.capacity)
		computeHourlyRate += prices.kafkaEndpointHourly
		// TODO: split ingress into its own metric when the collector can expose a
		// third Event Hubs pricing dimension without breaking current dashboards.
		computeHourlyRate += (usage.ingressBillableUnits * prices.ingressPricePerMillion) / 1_000_000

		allowanceGB := float64(namespace.capacity) * includedStorageGBPerThroughputUnit
		averageStoredGB := usage.averageSizeBytes / bytesPerGB
		storageOverageGB := math.Max(0, averageStoredGB-allowanceGB)
		storageHourlyRate := (storageOverageGB * prices.blobStoragePricePerGBMonth) / utils.HoursInMonth

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

func newPricingStore(logger *slog.Logger, azureClient client.AzureClient) *pricingStore {
	return &pricingStore{
		logger:      logger.With("store", "pricing"),
		azureClient: azureClient,
		byRegion:    make(map[string]regionPricing),
	}
}

func (p *pricingStore) RefreshIfNeeded(ctx context.Context, regions []string) error {
	regions = uniqueStrings(regions)
	if len(regions) == 0 {
		return nil
	}

	p.mu.RLock()
	currentlyFresh := time.Now().Before(p.nextRefresh) && p.hasAllRegionsLocked(regions)
	p.mu.RUnlock()
	if currentlyFresh {
		return nil
	}

	eventHubsPricing, err := p.fetchEventHubsPricing(ctx, regions)
	if err != nil {
		return err
	}

	blobStoragePricing, err := p.fetchBlobStoragePricing(ctx, regions)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	next := make(map[string]regionPricing, len(p.byRegion))
	for region, pricing := range p.byRegion {
		next[region] = pricing
	}

	for region, pricing := range eventHubsPricing {
		current := next[region]
		current.throughputUnitHourly = pricing.throughputUnitHourly
		current.kafkaEndpointHourly = pricing.kafkaEndpointHourly
		current.ingressPricePerMillion = pricing.ingressPricePerMillion
		next[region] = current
	}

	for region, price := range blobStoragePricing {
		current := next[region]
		current.blobStoragePricePerGBMonth = price
		next[region] = current
	}

	p.byRegion = next
	p.nextRefresh = time.Now().Add(priceRefreshTTL)

	return nil
}

func (p *pricingStore) PricesFor(region string) (regionPricing, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pricesForRegionLocked(strings.ToLower(region))
}

func (p *pricingStore) hasAllRegionsLocked(regions []string) bool {
	for _, region := range regions {
		if _, ok := p.pricesForRegionLocked(region); !ok {
			return false
		}
	}

	return true
}

func (p *pricingStore) pricesForRegionLocked(region string) (regionPricing, bool) {
	region = strings.ToLower(region)

	pricing := p.byRegion[region]
	globalPricing := p.byRegion[globalRegionName]

	if pricing.throughputUnitHourly == 0 {
		pricing.throughputUnitHourly = globalPricing.throughputUnitHourly
	}
	if pricing.kafkaEndpointHourly == 0 {
		pricing.kafkaEndpointHourly = globalPricing.kafkaEndpointHourly
	}
	if pricing.ingressPricePerMillion == 0 {
		pricing.ingressPricePerMillion = globalPricing.ingressPricePerMillion
	}

	if pricing.throughputUnitHourly == 0 ||
		pricing.kafkaEndpointHourly == 0 ||
		pricing.ingressPricePerMillion == 0 ||
		pricing.blobStoragePricePerGBMonth == 0 {
		return regionPricing{}, false
	}

	return pricing, true
}

func (p *pricingStore) fetchEventHubsPricing(ctx context.Context, regions []string) (map[string]regionPricing, error) {
	filter := fmt.Sprintf(
		"serviceName eq '%s' and priceType eq 'Consumption' and (%s)",
		eventHubsServiceName,
		armRegionFilter(regions, true),
	)

	prices, err := p.azureClient.ListPrices(ctx, &retailPriceSdk.RetailPricesClientListOptions{
		APIVersion: to.Ptr(retailPriceAPIVersion),
		Filter:     to.Ptr(filter),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Event Hubs retail prices: %w", err)
	}

	pricingByRegion := make(map[string]regionPricing)
	for _, price := range prices {
		if price == nil {
			continue
		}

		region := strings.ToLower(strings.TrimSpace(price.ArmRegionName))
		if region == "" {
			continue
		}

		current := pricingByRegion[region]

		switch {
		case price.ProductName == eventHubsServiceName &&
			price.SkuName == standardSKUName &&
			price.MeterName == standardThroughputMeter &&
			strings.EqualFold(price.UnitOfMeasure, "1 Hour"):
			current.throughputUnitHourly = maxFloat(current.throughputUnitHourly, price.RetailPrice)
		case price.ProductName == eventHubsServiceName &&
			price.SkuName == standardSKUName &&
			price.MeterName == standardKafkaEndpointMeter &&
			strings.EqualFold(price.UnitOfMeasure, "1 Hour"):
			current.kafkaEndpointHourly = maxFloat(current.kafkaEndpointHourly, price.RetailPrice)
		case price.ProductName == eventHubsServiceName &&
			price.SkuName == standardSKUName &&
			price.MeterName == standardIngressMeter &&
			strings.EqualFold(price.UnitOfMeasure, "1M"):
			current.ingressPricePerMillion = maxFloat(current.ingressPricePerMillion, price.RetailPrice)
		default:
			continue
		}

		pricingByRegion[region] = current
	}

	return pricingByRegion, nil
}

func (p *pricingStore) fetchBlobStoragePricing(ctx context.Context, regions []string) (map[string]float64, error) {
	filter := fmt.Sprintf(
		"serviceName eq '%s' and priceType eq 'Consumption' and skuName eq '%s' and meterName eq '%s' and (%s) and (productName eq '%s' or productName eq '%s' or productName eq '%s')",
		storageServiceName,
		hotLRSSKUName,
		hotLRSDataStoredMeter,
		armRegionFilter(regions, false),
		blobStorageProductName,
		generalBlockBlobProductV2,
		generalBlockBlobProduct,
	)

	prices, err := p.azureClient.ListPrices(ctx, &retailPriceSdk.RetailPricesClientListOptions{
		APIVersion: to.Ptr(retailPriceAPIVersion),
		Filter:     to.Ptr(filter),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list Blob Storage retail prices: %w", err)
	}

	type candidate struct {
		productPreference int
		price             float64
	}

	selected := make(map[string]candidate)
	for _, price := range prices {
		if price == nil || !strings.EqualFold(price.UnitOfMeasure, "1 GB/Month") {
			continue
		}

		region := strings.ToLower(strings.TrimSpace(price.ArmRegionName))
		if region == "" {
			continue
		}

		preference, ok := blobProductPreference(price.ProductName)
		if !ok {
			continue
		}

		current, exists := selected[region]
		if !exists || preference < current.productPreference || (preference == current.productPreference && price.RetailPrice > current.price) {
			selected[region] = candidate{
				productPreference: preference,
				price:             price.RetailPrice,
			}
		}
	}

	pricingByRegion := make(map[string]float64, len(selected))
	for region, candidate := range selected {
		pricingByRegion[region] = candidate.price
	}

	return pricingByRegion, nil
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
