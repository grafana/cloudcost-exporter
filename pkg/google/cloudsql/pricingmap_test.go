package cloudsql

import (
	"log/slog"
	"os"
	"testing"

	"cloud.google.com/go/billing/apiv1/billingpb"
	"github.com/grafana/cloudcost-exporter/pkg/google/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
	"google.golang.org/genproto/googleapis/type/money"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))

func TestPriceForSKU(t *testing.T) {
	tests := []struct {
		name      string
		sku       *billingpb.Sku
		wantPrice float64
		wantOk    bool
	}{
		{
			name: "valid SKU with units and nanos",
			sku: &billingpb.Sku{
				PricingInfo: []*billingpb.PricingInfo{
					{
						PricingExpression: &billingpb.PricingExpression{
							TieredRates: []*billingpb.PricingExpression_TierRate{
								{
									UnitPrice: &money.Money{
										Units: 1,
										Nanos: 250000000, // 0.25
									},
								},
							},
						},
					},
				},
			},
			wantPrice: 1.25,
			wantOk:    true,
		},
		{
			name: "valid SKU with only nanos",
			sku: &billingpb.Sku{
				PricingInfo: []*billingpb.PricingInfo{
					{
						PricingExpression: &billingpb.PricingExpression{
							TieredRates: []*billingpb.PricingExpression_TierRate{
								{
									UnitPrice: &money.Money{
										Units: 0,
										Nanos: 50000000, // 0.05
									},
								},
							},
						},
					},
				},
			},
			wantPrice: 0.05,
			wantOk:    true,
		},
		{
			name: "SKU with empty PricingInfo",
			sku: &billingpb.Sku{
				PricingInfo: []*billingpb.PricingInfo{},
			},
			wantPrice: 0,
			wantOk:    false,
		},
		{
			name: "SKU with empty TieredRates",
			sku: &billingpb.Sku{
				PricingInfo: []*billingpb.PricingInfo{
					{
						PricingExpression: &billingpb.PricingExpression{
							TieredRates: []*billingpb.PricingExpression_TierRate{},
						},
					},
				},
			},
			wantPrice: 0,
			wantOk:    false,
		},
		{
			name:      "nil SKU",
			sku:       nil,
			wantPrice: 0,
			wantOk:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			price, ok := priceForSKU(tt.sku)
			assert.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				assert.InDelta(t, tt.wantPrice, price, 0.0001)
			}
		})
	}
}

func TestGetInstanceSpecFromTier(t *testing.T) {
	tests := []struct {
		name      string
		tier      string
		wantSpec  *instanceSpec
		wantError bool
	}{
		{
			name: "custom tier",
			tier: "db-custom-4-8192",
			wantSpec: &instanceSpec{
				cpu:      4,
				ram:      8192,
				tier:     "db-custom-4-8192",
				isCustom: true,
			},
			wantError: false,
		},
		{
			name: "micro tier",
			tier: "db-f1-micro",
			wantSpec: &instanceSpec{
				cpu:      0,
				ram:      0,
				tier:     "db-f1-micro",
				tierType: "f1-micro",
				isCustom: false,
			},
			wantError: false,
		},
		{
			name: "small tier",
			tier: "db-g1-small",
			wantSpec: &instanceSpec{
				cpu:      0,
				ram:      0,
				tier:     "db-g1-small",
				tierType: "g1-small",
				isCustom: false,
			},
			wantError: false,
		},
		{
			name: "standard tier",
			tier: "db-n1-standard-1",
			wantSpec: &instanceSpec{
				cpu:      1,
				ram:      0,
				tier:     "db-n1-standard-1",
				tierType: "standard",
				isCustom: false,
			},
			wantError: false,
		},
		{
			name: "highmem tier",
			tier: "db-n1-highmem-4",
			wantSpec: &instanceSpec{
				cpu:      4,
				ram:      0,
				tier:     "db-n1-highmem-4",
				tierType: "highmem",
				isCustom: false,
			},
			wantError: false,
		},
		{
			name: "perf-optimized tier with non-numeric vCPU",
			tier: "db-perf-optimized-N-8",
			wantSpec: &instanceSpec{
				cpu:      0,
				ram:      0,
				tier:     "db-perf-optimized-N-8",
				tierType: "perf-optimized",
				isCustom: false,
			},
			wantError: false,
		},
		{
			name:      "invalid tier",
			tier:      "invalid-tier",
			wantSpec:  nil,
			wantError: true,
		},
		{
			name:      "empty tier",
			tier:      "",
			wantSpec:  nil,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, err := getInstanceSpecFromTier(tt.tier)
			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, spec)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, spec)
				assert.Equal(t, tt.wantSpec.cpu, spec.cpu)
				assert.Equal(t, tt.wantSpec.ram, spec.ram)
				assert.Equal(t, tt.wantSpec.tier, spec.tier)
				assert.Equal(t, tt.wantSpec.tierType, spec.tierType)
				assert.Equal(t, tt.wantSpec.isCustom, spec.isCustom)
			}
		})
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{
			name:  "valid number",
			input: "42",
			want:  42,
		},
		{
			name:  "zero",
			input: "0",
			want:  0,
		},
		{
			name:  "large number",
			input: "8192",
			want:  8192,
		},
		{
			name:  "invalid string",
			input: "abc",
			want:  0,
		},
		{
			name:  "empty string",
			input: "",
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseInt(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractSpecsFromDescription(t *testing.T) {
	tests := []struct {
		name      string
		desc      string
		wantCPU   int
		wantRAMMB int
		wantOk    bool
	}{
		{
			name:      "valid description with GB RAM",
			desc:      "Cloud SQL: 4 vCPU 10 GB RAM instance",
			wantCPU:   4,
			wantRAMMB: 10240,
			wantOk:    true,
		},
		{
			name:      "valid description with MB RAM",
			desc:      "Cloud SQL: 2 vCPU 512 MB RAM instance",
			wantCPU:   2,
			wantRAMMB: 512,
			wantOk:    true,
		},
		{
			name:      "valid description with decimal GB RAM",
			desc:      "Cloud SQL: 1 vCPU 3.75 GB RAM instance",
			wantCPU:   1,
			wantRAMMB: 3840, // 3.75 * 1024
			wantOk:    true,
		},
		{
			name:      "description without CPU",
			desc:      "Cloud SQL: 10 GB RAM instance",
			wantCPU:   0,
			wantRAMMB: 0,
			wantOk:    false,
		},
		{
			name:      "description without RAM",
			desc:      "Cloud SQL: 4 vCPU instance",
			wantCPU:   0,
			wantRAMMB: 0,
			wantOk:    false,
		},
		{
			name:      "empty description",
			desc:      "",
			wantCPU:   0,
			wantRAMMB: 0,
			wantOk:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cpu, ramMB, ok := extractSpecsFromDescription(tt.desc)
			assert.Equal(t, tt.wantOk, ok)
			if tt.wantOk {
				assert.Equal(t, tt.wantCPU, cpu)
				assert.Equal(t, tt.wantRAMMB, ramMB)
			}
		})
	}
}

func TestGetDatabaseType(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{
			name:    "MySQL version",
			version: "MYSQL_8_0",
			want:    "MYSQL",
		},
		{
			name:    "MySQL lowercase",
			version: "mysql_8_0",
			want:    "MYSQL",
		},
		{
			name:    "PostgreSQL version",
			version: "POSTGRES_14",
			want:    "POSTGRES",
		},
		{
			name:    "PostgreSQL lowercase",
			version: "postgres_14",
			want:    "POSTGRES",
		},
		{
			name:    "unknown version",
			version: "SQL_SERVER_2019",
			want:    "",
		},
		{
			name:    "empty version",
			version: "",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getDatabaseType(tt.version)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetAvailabilityType(t *testing.T) {
	tests := []struct {
		name      string
		availType string
		want      string
	}{
		{
			name:      "zonal",
			availType: "ZONAL",
			want:      "ZONAL",
		},
		{
			name:      "zonal lowercase",
			availType: "zonal",
			want:      "ZONAL",
		},
		{
			name:      "regional",
			availType: "REGIONAL",
			want:      "REGIONAL",
		},
		{
			name:      "regional lowercase",
			availType: "regional",
			want:      "REGIONAL",
		},
		{
			name:      "unknown type",
			availType: "UNKNOWN",
			want:      "UNKNOWN",
		},
		{
			name:      "empty type",
			availType: "",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getAvailabilityType(tt.availType)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsCustomPricingSku(t *testing.T) {
	tests := []struct {
		name string
		sku  *billingpb.Sku
		want bool
	}{
		{
			name: "CPU component SKU with vCPU in description",
			sku: &billingpb.Sku{
				Description: "Cloud SQL for PostgreSQL: Zonal - vCPU in Netherlands",
				PricingInfo: []*billingpb.PricingInfo{
					{
						PricingExpression: &billingpb.PricingExpression{
							UsageUnit: "h",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "RAM component SKU with GiBy",
			sku: &billingpb.Sku{
				Description: "Cloud SQL for MySQL: Regional - RAM in Phoenix",
				PricingInfo: []*billingpb.PricingInfo{
					{
						PricingExpression: &billingpb.PricingExpression{
							UsageUnit: "GiBy.h",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "standard instance SKU with both vCPU and RAM",
			sku: &billingpb.Sku{
				Description: "Cloud SQL for MySQL: Zonal - 96 vCPU + 360GB RAM in Paris",
				PricingInfo: []*billingpb.PricingInfo{
					{
						PricingExpression: &billingpb.PricingExpression{
							UsageUnit: "h",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "standard instance SKU without vCPU/RAM specs",
			sku: &billingpb.Sku{
				Description: "Cloud SQL: MYSQL db-f1-micro instance",
				PricingInfo: []*billingpb.PricingInfo{
					{
						PricingExpression: &billingpb.PricingExpression{
							UsageUnit: "h",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "SKU without PricingInfo",
			sku: &billingpb.Sku{
				Description: "Cloud SQL for PostgreSQL: Zonal - vCPU in Netherlands",
				PricingInfo: []*billingpb.PricingInfo{},
			},
			want: false,
		},
		{
			name: "nil SKU",
			sku:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCustomPricingSku(tt.sku)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsSkuInRegion(t *testing.T) {
	tests := []struct {
		name   string
		sku    *billingpb.Sku
		region string
		want   bool
	}{
		{
			name: "SKU in region",
			sku: &billingpb.Sku{
				GeoTaxonomy: &billingpb.GeoTaxonomy{
					Regions: []string{"us-central1", "us-east1"},
				},
			},
			region: "us-central1",
			want:   true,
		},
		{
			name: "SKU not in region",
			sku: &billingpb.Sku{
				GeoTaxonomy: &billingpb.GeoTaxonomy{
					Regions: []string{"us-central1", "us-east1"},
				},
			},
			region: "europe-west1",
			want:   false,
		},
		{
			name: "case insensitive match",
			sku: &billingpb.Sku{
				GeoTaxonomy: &billingpb.GeoTaxonomy{
					Regions: []string{"US-CENTRAL1"},
				},
			},
			region: "us-central1",
			want:   true,
		},
		{
			name: "SKU with empty regions",
			sku: &billingpb.Sku{
				GeoTaxonomy: &billingpb.GeoTaxonomy{
					Regions: []string{},
				},
			},
			region: "us-central1",
			want:   false,
		},
		{
			name: "SKU with nil GeoTaxonomy",
			sku: &billingpb.Sku{
				GeoTaxonomy: nil,
			},
			region: "us-central1",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSkuInRegion(tt.sku, tt.region)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDbTypeFromDescription(t *testing.T) {
	tests := []struct {
		name        string
		description string
		want        string
	}{
		{
			name:        "MySQL in description",
			description: "Cloud SQL: MYSQL db-f1-micro instance",
			want:        "MYSQL",
		},
		{
			name:        "MySQL lowercase",
			description: "Cloud SQL: mysql db-f1-micro instance",
			want:        "MYSQL",
		},
		{
			name:        "PostgreSQL in description",
			description: "Cloud SQL: POSTGRES db-f1-micro instance",
			want:        "POSTGRES",
		},
		{
			name:        "PostgreSQL lowercase",
			description: "Cloud SQL: postgres db-f1-micro instance",
			want:        "POSTGRES",
		},
		{
			name:        "no database type",
			description: "Cloud SQL: db-f1-micro instance",
			want:        "",
		},
		{
			name:        "empty description",
			description: "",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dbTypeFromDescription(tt.description)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAvailabilityFromDescription(t *testing.T) {
	tests := []struct {
		name        string
		description string
		want        string
	}{
		{
			name:        "zonal in description",
			description: "Cloud SQL: MYSQL db-f1-micro ZONAL instance",
			want:        "ZONAL",
		},
		{
			name:        "zonal lowercase",
			description: "Cloud SQL: MYSQL db-f1-micro zonal instance",
			want:        "ZONAL",
		},
		{
			name:        "regional in description",
			description: "Cloud SQL: MYSQL db-f1-micro REGIONAL instance",
			want:        "REGIONAL",
		},
		{
			name:        "regional lowercase",
			description: "Cloud SQL: MYSQL db-f1-micro regional instance",
			want:        "REGIONAL",
		},
		{
			name:        "no availability type",
			description: "Cloud SQL: MYSQL db-f1-micro instance",
			want:        "",
		},
		{
			name:        "empty description",
			description: "",
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := availabilityFromDescription(tt.description)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewPricingMap(t *testing.T) {
	logger := testLogger
	mockClient := &client.Mock{}

	pm := newPricingMap(logger, mockClient)

	assert.NotNil(t, pm)
	assert.Equal(t, logger, pm.logger)
	assert.Equal(t, mockClient, pm.gcpClient)
	assert.NotNil(t, pm.skus)
	assert.Len(t, pm.skus, 0)
}

func TestExtractInstanceInfo(t *testing.T) {
	pm := newPricingMap(testLogger, &client.Mock{})

	tests := []struct {
		name      string
		instance  *sqladmin.DatabaseInstance
		wantError bool
		checkFn   func(*testing.T, instanceTraits)
	}{
		{
			name: "valid instance",
			instance: &sqladmin.DatabaseInstance{
				Name:   "test-instance",
				Region: "us-central1",
				Settings: &sqladmin.Settings{
					Tier:             "db-f1-micro",
					AvailabilityType: "ZONAL",
				},
				DatabaseVersion: "MYSQL_8_0",
			},
			wantError: false,
			checkFn: func(t *testing.T, it instanceTraits) {
				assert.Equal(t, "us-central1", it.region)
				assert.Equal(t, "MYSQL", it.dbType)
				assert.Equal(t, "ZONAL", it.availability)
				assert.NotNil(t, it.spec)
				assert.Equal(t, "db-f1-micro", it.spec.tier)
			},
		},
		{
			name: "instance without tier",
			instance: &sqladmin.DatabaseInstance{
				Name:   "test-instance",
				Region: "us-central1",
				Settings: &sqladmin.Settings{
					AvailabilityType: "ZONAL",
				},
				DatabaseVersion: "MYSQL_8_0",
			},
			wantError: true,
		},
		{
			name: "instance without region",
			instance: &sqladmin.DatabaseInstance{
				Name: "test-instance",
				Settings: &sqladmin.Settings{
					Tier:             "db-f1-micro",
					AvailabilityType: "ZONAL",
				},
				DatabaseVersion: "MYSQL_8_0",
			},
			wantError: true,
		},
		{
			name: "instance without database version",
			instance: &sqladmin.DatabaseInstance{
				Name:   "test-instance",
				Region: "us-central1",
				Settings: &sqladmin.Settings{
					Tier:             "db-f1-micro",
					AvailabilityType: "ZONAL",
				},
				DatabaseVersion: "",
			},
			wantError: true,
		},
		{
			name: "instance without settings",
			instance: &sqladmin.DatabaseInstance{
				Name:            "test-instance",
				Region:          "us-central1",
				DatabaseVersion: "MYSQL_8_0",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			it, err := pm.extractInstanceInfo(tt.instance)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.checkFn != nil {
					tt.checkFn(t, it)
				}
			}
		})
	}
}

func TestMatchByTierType(t *testing.T) {
	tests := []struct {
		name   string
		sku    *billingpb.Sku
		traits instanceTraits
		want   bool
	}{
		{
			name: "matching tier type",
			sku: &billingpb.Sku{
				Description: "Cloud SQL: MYSQL f1-micro ZONAL instance",
			},
			traits: instanceTraits{
				spec: &instanceSpec{
					tierType: "f1-micro",
				},
			},
			want: true,
		},
		{
			name: "matching tier type case insensitive",
			sku: &billingpb.Sku{
				Description: "Cloud SQL: MYSQL G1-SMALL ZONAL instance",
			},
			traits: instanceTraits{
				spec: &instanceSpec{
					tierType: "g1-small",
				},
			},
			want: true,
		},
		{
			name: "no tier type in spec",
			sku: &billingpb.Sku{
				Description: "Cloud SQL: MYSQL f1-micro ZONAL instance",
			},
			traits: instanceTraits{
				spec: &instanceSpec{
					tierType: "",
				},
			},
			want: false,
		},
		{
			name: "tier type not in description",
			sku: &billingpb.Sku{
				Description: "Cloud SQL: MYSQL standard instance",
			},
			traits: instanceTraits{
				spec: &instanceSpec{
					tierType: "f1-micro",
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchByTierType(tt.sku, tt.traits)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsStandardSKU(t *testing.T) {
	validSKU := &billingpb.Sku{
		SkuId: "test-sku",
		Category: &billingpb.Category{
			ServiceDisplayName: "Cloud SQL",
		},
		Description: "Cloud SQL: MYSQL f1-micro ZONAL instance",
		GeoTaxonomy: &billingpb.GeoTaxonomy{
			Regions: []string{"us-central1"},
		},
		PricingInfo: []*billingpb.PricingInfo{
			{
				PricingExpression: &billingpb.PricingExpression{
					UsageUnit: "h",
				},
			},
		},
	}

	customSKU := &billingpb.Sku{
		SkuId: "custom-sku",
		Category: &billingpb.Category{
			ServiceDisplayName: "Cloud SQL",
		},
		Description: "Cloud SQL for PostgreSQL: Zonal - vCPU in Netherlands",
		GeoTaxonomy: &billingpb.GeoTaxonomy{
			Regions: []string{"us-central1"},
		},
		PricingInfo: []*billingpb.PricingInfo{
			{
				PricingExpression: &billingpb.PricingExpression{
					UsageUnit: "h",
				},
			},
		},
	}

	tests := []struct {
		name   string
		sku    *billingpb.Sku
		traits instanceTraits
		want   bool
	}{
		{
			name: "valid standard SKU for micro tier",
			sku:  validSKU,
			traits: instanceTraits{
				region: "us-central1",
				spec: &instanceSpec{
					cpu:      0,
					tierType: "f1-micro",
				},
			},
			want: true,
		},
		{
			name: "wrong service name",
			sku: &billingpb.Sku{
				Category: &billingpb.Category{
					ServiceDisplayName: "Compute Engine",
				},
			},
			traits: instanceTraits{
				region: "us-central1",
				spec:   &instanceSpec{cpu: 0, tierType: "f1-micro"},
			},
			want: false,
		},
		{
			name: "custom pricing SKU",
			sku:  customSKU,
			traits: instanceTraits{
				region: "us-central1",
				spec:   &instanceSpec{cpu: 0, tierType: "f1-micro"},
			},
			want: false,
		},
		{
			name: "wrong region",
			sku:  validSKU,
			traits: instanceTraits{
				region: "europe-west1",
				spec:   &instanceSpec{cpu: 0, tierType: "f1-micro"},
			},
			want: false,
		},
		{
			name: "tier type mismatch",
			sku:  validSKU,
			traits: instanceTraits{
				region: "us-central1",
				spec: &instanceSpec{
					cpu:      0,
					tierType: "g1-small",
				},
			},
			want: false,
		},
		{
			name: "nil SKU",
			sku:  nil,
			traits: instanceTraits{
				region: "us-central1",
				spec:   &instanceSpec{cpu: 0, tierType: "f1-micro"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStandardSKU(tt.sku, tt.traits)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFindStandardInstancePrice(t *testing.T) {
	pm := newPricingMap(testLogger, &client.Mock{})

	validSKU := &billingpb.Sku{
		SkuId: "test-sku",
		Category: &billingpb.Category{
			ServiceDisplayName: "Cloud SQL",
		},
		Description: "Cloud SQL: MYSQL f1-micro ZONAL instance running in us-central1",
		GeoTaxonomy: &billingpb.GeoTaxonomy{
			Regions: []string{"us-central1"},
		},
		PricingInfo: []*billingpb.PricingInfo{
			{
				PricingExpression: &billingpb.PricingExpression{
					TieredRates: []*billingpb.PricingExpression_TierRate{
						{
							UnitPrice: &money.Money{
								Units: 0,
								Nanos: 25000000, // $0.025
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name      string
		skus      []*billingpb.Sku
		traits    instanceTraits
		wantError bool
		wantPrice float64
	}{
		{
			name: "matching SKU found for micro tier",
			skus: []*billingpb.Sku{validSKU},
			traits: instanceTraits{
				region:       "us-central1",
				dbType:       "MYSQL",
				availability: "ZONAL",
				spec: &instanceSpec{
					cpu:      0,
					ram:      0,
					tier:     "db-f1-micro",
					tierType: "f1-micro",
					isCustom: false,
				},
			},
			wantError: false,
			wantPrice: 0.025,
		},
		{
			name: "no matching SKU - wrong region",
			skus: []*billingpb.Sku{validSKU},
			traits: instanceTraits{
				region:       "europe-west1",
				dbType:       "MYSQL",
				availability: "ZONAL",
				spec: &instanceSpec{
					cpu:      0,
					ram:      0,
					tier:     "db-f1-micro",
					tierType: "f1-micro",
					isCustom: false,
				},
			},
			wantError: true,
		},
		{
			name: "no matching SKU - wrong tier type",
			skus: []*billingpb.Sku{validSKU},
			traits: instanceTraits{
				region:       "us-central1",
				dbType:       "MYSQL",
				availability: "ZONAL",
				spec: &instanceSpec{
					cpu:      0,
					ram:      0,
					tier:     "db-g1-small",
					tierType: "g1-small",
					isCustom: false,
				},
			},
			wantError: true,
		},
		{
			name: "empty SKUs",
			skus: []*billingpb.Sku{},
			traits: instanceTraits{
				region:       "us-central1",
				dbType:       "MYSQL",
				availability: "ZONAL",
				spec: &instanceSpec{
					cpu:      0,
					ram:      0,
					tier:     "db-f1-micro",
					tierType: "f1-micro",
					isCustom: false,
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm.mu.Lock()
			pm.skus = tt.skus
			pm.mu.Unlock()

			result, err := pm.findStandardInstancePrice(tt.traits)
			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, result)
				assert.InDelta(t, tt.wantPrice, result.pricePerHour, 0.0001)
				assert.False(t, result.isCustom)
			}
		})
	}
}

func TestCalculateCustomPrice(t *testing.T) {
	pm := newPricingMap(testLogger, &client.Mock{})

	cpuSKU := &billingpb.Sku{
		SkuId: "cpu-sku",
		Category: &billingpb.Category{
			ServiceDisplayName: "Cloud SQL",
		},
		Description: "Cloud SQL for PostgreSQL: Zonal - vCPU in Netherlands",
		GeoTaxonomy: &billingpb.GeoTaxonomy{
			Regions: []string{"us-central1"},
		},
		PricingInfo: []*billingpb.PricingInfo{
			{
				PricingExpression: &billingpb.PricingExpression{
					UsageUnit: "h",
					TieredRates: []*billingpb.PricingExpression_TierRate{
						{
							UnitPrice: &money.Money{
								Units: 0,
								Nanos: 50000000, // $0.05 per vCPU per hour
							},
						},
					},
				},
			},
		},
	}

	ramSKU := &billingpb.Sku{
		SkuId: "ram-sku",
		Category: &billingpb.Category{
			ServiceDisplayName: "Cloud SQL",
		},
		Description: "Cloud SQL for MySQL: Regional - RAM in Phoenix",
		GeoTaxonomy: &billingpb.GeoTaxonomy{
			Regions: []string{"us-central1"},
		},
		PricingInfo: []*billingpb.PricingInfo{
			{
				PricingExpression: &billingpb.PricingExpression{
					UsageUnit: "GiBy.h",
					TieredRates: []*billingpb.PricingExpression_TierRate{
						{
							UnitPrice: &money.Money{
								Units: 0,
								Nanos: 10000000, // $0.01 per GB per hour
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name      string
		skus      []*billingpb.Sku
		region    string
		spec      *instanceSpec
		wantError bool
		wantPrice float64
	}{
		{
			name:   "valid custom pricing",
			skus:   []*billingpb.Sku{cpuSKU, ramSKU},
			region: "us-central1",
			spec: &instanceSpec{
				cpu:      4,
				ram:      8192, // 8 GB
				isCustom: true,
			},
			wantError: false,
			wantPrice: 0.28, // 4 * 0.05 + 8 * 0.01
		},
		{
			name:   "missing CPU SKU",
			skus:   []*billingpb.Sku{ramSKU},
			region: "us-central1",
			spec: &instanceSpec{
				cpu:      4,
				ram:      8192,
				isCustom: true,
			},
			wantError: true,
		},
		{
			name:   "missing RAM SKU",
			skus:   []*billingpb.Sku{cpuSKU},
			region: "us-central1",
			spec: &instanceSpec{
				cpu:      4,
				ram:      8192,
				isCustom: true,
			},
			wantError: true,
		},
		{
			name:   "wrong region",
			skus:   []*billingpb.Sku{cpuSKU, ramSKU},
			region: "europe-west1",
			spec: &instanceSpec{
				cpu:      4,
				ram:      8192,
				isCustom: true,
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm.mu.Lock()
			pm.skus = tt.skus
			pm.mu.Unlock()

			result, err := pm.calculateCustomPrice(tt.region, tt.spec)
			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, result)
				assert.InDelta(t, tt.wantPrice, result.pricePerHour, 0.0001)
				assert.True(t, result.isCustom)
			}
		})
	}
}

func TestMatchInstancePrice(t *testing.T) {
	pm := newPricingMap(testLogger, &client.Mock{})

	standardSKU := &billingpb.Sku{
		SkuId: "standard-sku",
		Category: &billingpb.Category{
			ServiceDisplayName: "Cloud SQL",
		},
		Description: "Cloud SQL: MYSQL f1-micro ZONAL instance running in us-central1",
		GeoTaxonomy: &billingpb.GeoTaxonomy{
			Regions: []string{"us-central1"},
		},
		PricingInfo: []*billingpb.PricingInfo{
			{
				PricingExpression: &billingpb.PricingExpression{
					TieredRates: []*billingpb.PricingExpression_TierRate{
						{
							UnitPrice: &money.Money{
								Units: 0,
								Nanos: 25000000,
							},
						},
					},
				},
			},
		},
	}

	cpuSKU := &billingpb.Sku{
		SkuId: "cpu-sku",
		Category: &billingpb.Category{
			ServiceDisplayName: "Cloud SQL",
		},
		Description: "Cloud SQL for PostgreSQL: Zonal - vCPU in Netherlands",
		GeoTaxonomy: &billingpb.GeoTaxonomy{
			Regions: []string{"us-central1"},
		},
		PricingInfo: []*billingpb.PricingInfo{
			{
				PricingExpression: &billingpb.PricingExpression{
					UsageUnit: "h",
					TieredRates: []*billingpb.PricingExpression_TierRate{
						{
							UnitPrice: &money.Money{
								Units: 0,
								Nanos: 50000000,
							},
						},
					},
				},
			},
		},
	}

	ramSKU := &billingpb.Sku{
		SkuId: "ram-sku",
		Category: &billingpb.Category{
			ServiceDisplayName: "Cloud SQL",
		},
		Description: "Cloud SQL for MySQL: Regional - RAM in Phoenix",
		GeoTaxonomy: &billingpb.GeoTaxonomy{
			Regions: []string{"us-central1"},
		},
		PricingInfo: []*billingpb.PricingInfo{
			{
				PricingExpression: &billingpb.PricingExpression{
					UsageUnit: "GiBy.h",
					TieredRates: []*billingpb.PricingExpression_TierRate{
						{
							UnitPrice: &money.Money{
								Units: 0,
								Nanos: 10000000,
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name       string
		skus       []*billingpb.Sku
		instance   *sqladmin.DatabaseInstance
		wantError  bool
		wantCustom bool
	}{
		{
			name: "standard instance pricing",
			skus: []*billingpb.Sku{standardSKU},
			instance: &sqladmin.DatabaseInstance{
				Name:   "test-instance",
				Region: "us-central1",
				Settings: &sqladmin.Settings{
					Tier:             "db-f1-micro",
					AvailabilityType: "ZONAL",
				},
				DatabaseVersion: "MYSQL_8_0",
			},
			wantError:  false,
			wantCustom: false,
		},
		{
			name: "custom instance pricing",
			skus: []*billingpb.Sku{cpuSKU, ramSKU},
			instance: &sqladmin.DatabaseInstance{
				Name:   "test-instance",
				Region: "us-central1",
				Settings: &sqladmin.Settings{
					Tier:             "db-custom-4-8192",
					AvailabilityType: "ZONAL",
				},
				DatabaseVersion: "MYSQL_8_0",
			},
			wantError:  false,
			wantCustom: true,
		},
		{
			name: "instance with invalid tier",
			skus: []*billingpb.Sku{standardSKU},
			instance: &sqladmin.DatabaseInstance{
				Name:   "test-instance",
				Region: "us-central1",
				Settings: &sqladmin.Settings{
					Tier:             "invalid-tier",
					AvailabilityType: "ZONAL",
				},
				DatabaseVersion: "MYSQL_8_0",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm.mu.Lock()
			pm.skus = tt.skus
			pm.mu.Unlock()

			result, err := pm.matchInstancePrice(tt.instance)
			if tt.wantError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.wantCustom, result.isCustom)
			}
		})
	}
}
