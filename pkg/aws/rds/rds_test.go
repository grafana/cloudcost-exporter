package rds

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	rdsTypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/grafana/cloudcost-exporter/pkg/aws/client"
	mock "github.com/grafana/cloudcost-exporter/pkg/aws/client/mocks"
	"github.com/grafana/cloudcost-exporter/pkg/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// rdsPriceProduct builds a Pricing API product JSON for the given attributes.
func rdsPriceProduct(region, instanceType, databaseEngine, deploymentOption, locationType, usd string) string {
	return fmt.Sprintf(`{"product":{"attributes":{"instanceType":%q,"regionCode":%q,"databaseEngine":%q,"deploymentOption":%q,"locationType":%q}},"terms":{"OnDemand":{"t":{"priceDimensions":{"d":{"pricePerUnit":{"USD":%q}}}}}}}`,
		instanceType, region, databaseEngine, deploymentOption, locationType, usd)
}

// postgresPrice builds the price product that matches instanceFor's shape.
func postgresPrice(region, usd string) string {
	return rdsPriceProduct(region, "db.t3.medium", "PostgreSQL", "Single-AZ", "AWS Region", usd)
}

// expectPricing stubs ListRDSPrices to return a matching postgres price per
// region, so a populate warms every instanceFor instance.
func expectPricing(c *mock.MockClient, usd string) {
	c.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, region string) ([]string, error) {
			return []string{postgresPrice(region, usd)}, nil
		}).AnyTimes()
}

func TestIsOutpostsInstance(t *testing.T) {
	tests := []struct {
		name string
		inst rdsTypes.DBInstance
		want string
	}{
		{
			name: "outposts instance type",
			inst: rdsTypes.DBInstance{
				DBSubnetGroup: &rdsTypes.DBSubnetGroup{
					Subnets: []rdsTypes.Subnet{
						{
							SubnetOutpost: &rdsTypes.Outpost{
								Arn: aws.String("some-outpost-arn"),
							},
						},
					},
				},
				DBInstanceArn: aws.String("some-arn"),
			},
			want: "AWS Outposts",
		},
		{
			name: "non-outposts instance type",
			inst: rdsTypes.DBInstance{
				DBSubnetGroup: &rdsTypes.DBSubnetGroup{
					Subnets: []rdsTypes.Subnet{
						{
							SubnetOutpost: nil,
						},
					},
				},
				DBInstanceArn: aws.String("some-arn"),
			},
			want: "AWS Region",
		},
		{
			name: "non-outposts instance type: DBSubnetGroup empty",
			inst: rdsTypes.DBInstance{
				DBSubnetGroup: &rdsTypes.DBSubnetGroup{},
				DBInstanceArn: aws.String("some-arn"),
			},
			want: "AWS Region",
		},
		{
			name: "non-outposts instance type: arn empty",
			inst: rdsTypes.DBInstance{
				DBSubnetGroup: &rdsTypes.DBSubnetGroup{
					Subnets: []rdsTypes.Subnet{
						{
							SubnetOutpost: &rdsTypes.Outpost{},
						},
					},
				},
				DBInstanceArn: aws.String("some-other-arn"),
			},
			want: "AWS Region",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOutpostsInstance(tt.inst)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMultiOrSingleAZ(t *testing.T) {
	tests := []struct {
		name    string
		multiAZ bool
		want    string
	}{
		{
			name:    "Multi-AZ",
			multiAZ: true,
			want:    "Multi-AZ",
		},
		{
			name:    "Single-AZ",
			multiAZ: false,
			want:    "Single-AZ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := multiOrSingleAZ(tt.multiAZ)
			assert.Equal(t, tt.want, got)
		})
	}
}

func instanceFor(region, id string) rdsTypes.DBInstance {
	return rdsTypes.DBInstance{
		DBSubnetGroup:        &rdsTypes.DBSubnetGroup{},
		AvailabilityZone:     aws.String(region + "a"),
		DBInstanceClass:      aws.String("db.t3.medium"),
		Engine:               aws.String("postgres"),
		DBInstanceIdentifier: aws.String(id),
		MultiAZ:              aws.Bool(false),
		DbiResourceId:        aws.String(id),
		DBInstanceArn:        aws.String("arn-" + id),
	}
}

// TestPricingKeyFor_AuroraStorageMode verifies Aurora engines disambiguate
// Standard vs I/O-Optimized storage in the pricing key, non-Aurora engines are
// unaffected, and a missing or unrecognized Aurora StorageType is skipped
// rather than silently mis-keyed.
func TestPricingKeyFor_AuroraStorageMode(t *testing.T) {
	auroraInstance := func(storageType *string) rdsTypes.DBInstance {
		inst := instanceFor("us-east-1", "aurora-1")
		inst.Engine = aws.String("aurora-mysql")
		inst.StorageType = storageType
		return inst
	}

	tests := []struct {
		name   string
		inst   rdsTypes.DBInstance
		wantOK bool
	}{
		{
			name:   "non-aurora engine ignores storage type",
			inst:   instanceFor("us-east-1", "db-1"),
			wantOK: true,
		},
		{
			name:   "aurora standard storage",
			inst:   auroraInstance(aws.String("aurora")),
			wantOK: true,
		},
		{
			name:   "aurora I/O-optimized storage",
			inst:   auroraInstance(aws.String("aurora-iopt1")),
			wantOK: true,
		},
		{
			name:   "aurora with nil storage type is skipped",
			inst:   auroraInstance(nil),
			wantOK: false,
		},
		{
			name:   "aurora with unrecognized storage type is skipped",
			inst:   auroraInstance(aws.String("some-future-mode")),
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, ok := pricingKeyFor(tt.inst)
			assert.Equal(t, tt.wantOK, ok)
		})
	}

	t.Run("standard and I/O-optimized keys are distinct and match the pricing side", func(t *testing.T) {
		standardKey, _, ok := pricingKeyFor(auroraInstance(aws.String("aurora")))
		assert.True(t, ok)
		ioOptimizedKey, _, ok := pricingKeyFor(auroraInstance(aws.String("aurora-iopt1")))
		assert.True(t, ok)

		assert.NotEqual(t, standardKey, ioOptimizedKey)
		assert.Equal(t, createPricingKey("us-east-1", "db.t3.medium", "Aurora MySQL", "", "Single-AZ", "No license required", "AWS Region", auroraStorageModeStandard), standardKey)
		assert.Equal(t, createPricingKey("us-east-1", "db.t3.medium", "Aurora MySQL", "", "Single-AZ", "No license required", "AWS Region", auroraStorageModeIOOptimized), ioOptimizedKey)
	})
}

// TestEngineAttributes_KeysMatchPricing pins every engineAttributes entry to a
// real AmazonRDS price-list row: the key built from the instance must equal the
// key built from that row's attributes, or the instance is never priced. The
// price-list attribute values come verbatim from the us-east-1 AmazonRDS offer
// file.
func TestEngineAttributes_KeysMatchPricing(t *testing.T) {
	type pricingRow struct {
		instanceType, databaseEngine, databaseEdition, licenseModel, storage string
	}
	tests := []struct {
		name         string
		engine       string
		licenseModel string
		storageType  string
		row          pricingRow
	}{
		{name: "mysql", engine: "mysql", licenseModel: "general-public-license",
			row: pricingRow{"db.m5.large", "MySQL", "", "No license required", "EBS Only"}},
		{name: "mariadb", engine: "mariadb", licenseModel: "general-public-license",
			row: pricingRow{"db.m5.large", "MariaDB", "", "No license required", "EBS Only"}},
		{name: "postgres", engine: "postgres", licenseModel: "postgresql-license",
			row: pricingRow{"db.m5.large", "PostgreSQL", "", "No license required", "EBS Only"}},
		{name: "aurora-mysql standard", engine: "aurora-mysql", licenseModel: "general-public-license", storageType: "aurora",
			row: pricingRow{"db.r5.large", "Aurora MySQL", "", "No license required", "EBS Only"}},
		{name: "aurora-mysql I/O-optimized", engine: "aurora-mysql", licenseModel: "general-public-license", storageType: "aurora-iopt1",
			row: pricingRow{"db.r5.large", "Aurora MySQL", "", "No license required", "Aurora IO Optimization Mode"}},
		{name: "aurora-postgresql standard", engine: "aurora-postgresql", licenseModel: "postgresql-license", storageType: "aurora",
			row: pricingRow{"db.r5.large", "Aurora PostgreSQL", "", "No license required", "EBS Only"}},
		{name: "aurora-postgresql I/O-optimized", engine: "aurora-postgresql", licenseModel: "postgresql-license", storageType: "aurora-iopt1",
			row: pricingRow{"db.r5.large", "Aurora PostgreSQL", "", "No license required", "Aurora IO Optimization Mode"}},
		{name: "aurora-postgresql optimized reads standard", engine: "aurora-postgresql", licenseModel: "postgresql-license", storageType: "aurora",
			row: pricingRow{"db.r6gd.xlarge", "Aurora PostgreSQL", "", "No license required", "1 x 237 NVMe SSD"}},
		{name: "oracle-ee bring your own license", engine: "oracle-ee", licenseModel: "bring-your-own-license",
			row: pricingRow{"db.m5.large", "Oracle", "Enterprise", "Bring your own license", "EBS Only"}},
		{name: "oracle-ee-cdb bring your own license", engine: "oracle-ee-cdb", licenseModel: "bring-your-own-license",
			row: pricingRow{"db.m5.large", "Oracle", "Enterprise", "Bring your own license", "EBS Only"}},
		{name: "oracle-se2 license included", engine: "oracle-se2", licenseModel: "license-included",
			row: pricingRow{"db.m5.large", "Oracle", "Standard Two", "License included", "EBS Only"}},
		{name: "oracle-se2 bring your own license", engine: "oracle-se2", licenseModel: "bring-your-own-license",
			row: pricingRow{"db.m5.large", "Oracle", "Standard Two", "Bring your own license", "EBS Only"}},
		{name: "oracle-se2-cdb license included", engine: "oracle-se2-cdb", licenseModel: "license-included",
			row: pricingRow{"db.m5.large", "Oracle", "Standard Two", "License included", "EBS Only"}},
		{name: "sqlserver-ee license included", engine: "sqlserver-ee", licenseModel: "license-included",
			row: pricingRow{"db.m4.10xlarge", "SQL Server", "Enterprise", "License included", "EBS Only"}},
		{name: "sqlserver-se license included", engine: "sqlserver-se", licenseModel: "license-included",
			row: pricingRow{"db.m5.large", "SQL Server", "Standard", "License included", "EBS Only"}},
		{name: "sqlserver-web license included", engine: "sqlserver-web", licenseModel: "license-included",
			row: pricingRow{"db.m5.large", "SQL Server", "Web", "License included", "EBS Only"}},
		{name: "sqlserver-ex license included", engine: "sqlserver-ex", licenseModel: "license-included",
			row: pricingRow{"db.t3.micro", "SQL Server", "Express", "License included", "EBS Only"}},
	}

	covered := map[string]bool{}
	for _, tt := range tests {
		covered[tt.engine] = true
		t.Run(tt.name, func(t *testing.T) {
			inst := instanceFor("us-east-1", "db-1")
			inst.Engine = aws.String(tt.engine)
			inst.DBInstanceClass = aws.String(tt.row.instanceType)
			inst.LicenseModel = aws.String(tt.licenseModel)
			if tt.storageType != "" {
				inst.StorageType = aws.String(tt.storageType)
			}

			instanceKey, _, ok := pricingKeyFor(inst)
			require.True(t, ok, "instance side failed to build a key")

			pricingKey, ok := priceKeyFromAttributes(&AWSProduct{Attributes: &AWSProductAttributes{
				InstanceType:     tt.row.instanceType,
				RegionCode:       "us-east-1",
				DatabaseEngine:   tt.row.databaseEngine,
				DatabaseEdition:  tt.row.databaseEdition,
				DeploymentOption: "Single-AZ",
				LicenseModel:     tt.row.licenseModel,
				LocationType:     "AWS Region",
				Storage:          tt.row.storage,
			}})
			require.True(t, ok, "pricing side failed to build a key")

			assert.Equal(t, pricingKey, instanceKey)
		})
	}

	for engine := range engineAttributes {
		assert.True(t, covered[engine], "engineAttributes entry %q has no price-list case", engine)
	}
}

// TestAuroraStorageModeFromPricing covers every Aurora "storage" attribute
// shape in the AmazonRDS price list, including the instance-store values of
// Optimized Reads classes, and that anything else is rejected.
func TestAuroraStorageModeFromPricing(t *testing.T) {
	tests := []struct {
		storage  string
		wantMode string
		wantOK   bool
	}{
		{storage: "EBS Only", wantMode: auroraStorageModeStandard, wantOK: true},
		{storage: "Aurora IO Optimization Mode", wantMode: auroraStorageModeIOOptimized, wantOK: true},
		{storage: "1 x 237 NVMe SSD", wantMode: auroraStorageModeStandard, wantOK: true},
		{storage: "3 X 1900 NVMe SSD", wantMode: auroraStorageModeStandard, wantOK: true},
		{storage: "2 x 320 SSD", wantMode: auroraStorageModeStandard, wantOK: true},
		{storage: "", wantOK: false},
		{storage: "Some New Storage Mode", wantOK: false},
		{storage: "1 x 237 NVMe SSD (preview)", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.storage, func(t *testing.T) {
			mode, ok := auroraStorageModeFromPricing(tt.storage)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantMode, mode)
		})
	}
}

// collectRegions drains ch and returns the set of region labels seen.
func collectRegions(t *testing.T, ch chan prometheus.Metric) map[string]bool {
	t.Helper()
	close(ch)
	got := map[string]bool{}
	for metric := range ch {
		got[utils.ReadMetrics(metric).Labels["region"]] = true
	}
	return got
}

// TestCollector_Collect_ColdStart_NeitherReady verifies that a scrape before
// either store has populated emits no metrics and does not error.
func TestCollector_Collect_ColdStart_NeitherReady(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	// No AWS calls expected: neither store has populated, so Collect returns early.

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	instanceStore := newTestInstanceStore(regions, regionMap)
	pricingStore := newTestPricingStore(regions, regionClient)

	c := &Collector{
		regions:        regions,
		regionMap:      regionMap,
		instanceStore:  instanceStore,
		pricingStore:   pricingStore,
		accountID:      "123456789012",
		populateErrors: instanceStore.populateErrors,
		logger:         slog.Default(),
	}

	ch := make(chan prometheus.Metric, 1)
	err := c.Collect(t.Context(), ch)
	assert.NoError(t, err)

	select {
	case <-ch:
		t.Fatal("expected no metric before either store is populated")
	default:
	}
}

// TestCollector_Collect_ColdStart_InstancesReadyPricesNot verifies that a
// scrape with a warm instance store but a cold pricing store still emits no
// metrics and does not error.
func TestCollector_Collect_ColdStart_InstancesReadyPricesNot(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	regionClient.EXPECT().ListRDSInstances(gomock.Any()).
		Return([]rdsTypes.DBInstance{instanceFor("us-east-1", "db-1")}, nil).
		Times(1)
	// No ListRDSPrices call expected: the pricing store never populates.
	pricingClient := mock.NewMockClient(mockCtrl)

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	instanceStore := newTestInstanceStore(regions, regionMap)
	instanceStore.Populate(t.Context())
	pricingStore := newTestPricingStore(regions, pricingClient)

	c := &Collector{
		regions:        regions,
		regionMap:      regionMap,
		instanceStore:  instanceStore,
		pricingStore:   pricingStore,
		accountID:      "123456789012",
		populateErrors: instanceStore.populateErrors,
		logger:         slog.Default(),
	}

	ch := make(chan prometheus.Metric, 1)
	err := c.Collect(t.Context(), ch)
	assert.NoError(t, err)

	select {
	case <-ch:
		t.Fatal("expected no metric while the pricing store is cold")
	default:
	}
}

// TestCollector_Collect_ColdStart_PricesReadyInstancesNot verifies that a
// scrape with a warm pricing store but a cold instance store still emits no
// metrics and does not error.
func TestCollector_Collect_ColdStart_PricesReadyInstancesNot(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	// No ListRDSInstances call expected: the instance store never populates.
	regionClient := mock.NewMockClient(mockCtrl)
	pricingClient := mock.NewMockClient(mockCtrl)
	pricingClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
		Return([]string{postgresPrice("us-east-1", "0.456")}, nil).
		Times(1)

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	instanceStore := newTestInstanceStore(regions, regionMap)
	pricingStore := newTestPricingStore(regions, pricingClient)
	pricingStore.Populate(t.Context())

	c := &Collector{
		regions:        regions,
		regionMap:      regionMap,
		instanceStore:  instanceStore,
		pricingStore:   pricingStore,
		accountID:      "123456789012",
		populateErrors: instanceStore.populateErrors,
		logger:         slog.Default(),
	}

	ch := make(chan prometheus.Metric, 1)
	err := c.Collect(t.Context(), ch)
	assert.NoError(t, err)

	select {
	case <-ch:
		t.Fatal("expected no metric while the instance store is cold")
	default:
	}
}

// TestCollector_Collect_ServesFromWarmMap verifies that once the store is
// populated, Collect serves instances and prices from memory and makes zero
// AWS calls. The gomock Times(1) expectations are consumed entirely by the
// background populate; any call from Collect would exceed them and fail.
func TestCollector_Collect_ServesFromWarmMap(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	regionClient.EXPECT().ListRDSInstances(gomock.Any()).
		Return([]rdsTypes.DBInstance{instanceFor("us-east-1", "db-1")}, nil).
		Times(1)

	pricingClient := mock.NewMockClient(mockCtrl)
	pricingClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
		Return([]string{postgresPrice("us-east-1", "0.456")}, nil).
		Times(1)

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	instanceStore := newTestInstanceStore(regions, regionMap)
	instanceStore.Populate(t.Context())
	pricingStore := newTestPricingStore(regions, pricingClient)
	pricingStore.Populate(t.Context())

	c := &Collector{
		regions:        regions,
		regionMap:      regionMap,
		instanceStore:  instanceStore,
		pricingStore:   pricingStore,
		accountID:      "123456789012",
		populateErrors: instanceStore.populateErrors,
		logger:         slog.Default(),
	}

	ch := make(chan prometheus.Metric, 1)
	err := c.Collect(t.Context(), ch)
	assert.NoError(t, err)

	select {
	case metric := <-ch:
		result := utils.ReadMetrics(metric)
		assert.Equal(t, "db.t3.medium", result.Labels["tier"])
		assert.Equal(t, "db-1", result.Labels["id"])
		assert.Equal(t, 0.456, result.Value)
	default:
		t.Fatal("expected a metric to be collected from the warm map")
	}
}

// TestCollector_Collect_PricingMiss verifies that an instance whose price never
// warmed is skipped with a log and does not fail the scrape.
func TestCollector_Collect_PricingMiss(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	regionClient.EXPECT().ListRDSInstances(gomock.Any()).
		Return([]rdsTypes.DBInstance{instanceFor("us-east-1", "db-1")}, nil).
		Times(1)

	// The pricing API returns no products, so the key never lands in the map.
	pricingClient := mock.NewMockClient(mockCtrl)
	pricingClient.EXPECT().ListRDSPrices(gomock.Any(), gomock.Any()).
		Return([]string{}, nil).
		Times(1)

	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	instanceStore := newTestInstanceStore(regions, regionMap)
	instanceStore.Populate(t.Context())
	pricingStore := newTestPricingStore(regions, pricingClient)
	pricingStore.Populate(t.Context())

	c := &Collector{
		regions:        regions,
		regionMap:      regionMap,
		instanceStore:  instanceStore,
		pricingStore:   pricingStore,
		accountID:      "123456789012",
		populateErrors: instanceStore.populateErrors,
		logger:         slog.Default(),
	}

	ch := make(chan prometheus.Metric, 1)
	err := c.Collect(t.Context(), ch)
	assert.NoError(t, err)

	select {
	case <-ch:
		t.Fatal("expected no metric when pricing is missing")
	default:
	}
}

// TestCollector_Collect_MultiRegion verifies instances from every region are
// served from the warm store.
func TestCollector_Collect_MultiRegion(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionNames := []string{"us-east-1", "eu-west-1", "ap-southeast-7"}
	regions := make([]types.Region, 0, len(regionNames))
	regionMap := make(map[string]client.Client, len(regionNames))
	for _, region := range regionNames {
		regions = append(regions, types.Region{RegionName: aws.String(region)})
		regionClient := mock.NewMockClient(mockCtrl)
		regionClient.EXPECT().ListRDSInstances(gomock.Any()).
			Return([]rdsTypes.DBInstance{instanceFor(region, "db-"+region)}, nil).
			Times(1)
		regionMap[region] = regionClient
	}

	// One distinct pricing key per region.
	pricingClient := mock.NewMockClient(mockCtrl)
	expectPricing(pricingClient, "0.456")

	instanceStore := newTestInstanceStore(regions, regionMap)
	instanceStore.Populate(t.Context())
	pricingStore := newTestPricingStore(regions, pricingClient)
	pricingStore.Populate(t.Context())

	c := &Collector{
		regions:        regions,
		regionMap:      regionMap,
		instanceStore:  instanceStore,
		pricingStore:   pricingStore,
		accountID:      "123456789012",
		populateErrors: instanceStore.populateErrors,
		logger:         slog.Default(),
	}

	ch := make(chan prometheus.Metric, len(regionNames))
	err := c.Collect(t.Context(), ch)
	assert.NoError(t, err)

	gotRegions := collectRegions(t, ch)
	for _, region := range regionNames {
		assert.True(t, gotRegions[region], "expected a metric for region %s", region)
	}
}
