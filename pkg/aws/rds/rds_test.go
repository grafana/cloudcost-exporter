package rds

import (
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
	"go.uber.org/mock/gomock"
)

const validPriceJSON = `{
	"terms": {
		"OnDemand": {
			"term1": {
				"priceDimensions": {
					"dim1": {
						"pricePerUnit": {"USD": "0.456"}
					}
				}
			}
		}
	}
}`

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

// TestCollector_Collect_ColdStart verifies that a scrape before the first
// populate finishes emits no metrics and does not error.
func TestCollector_Collect_ColdStart(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	regionClient := mock.NewMockClient(mockCtrl)
	// No AWS calls expected: the store has not populated, so Collect returns early.

	pm := newPricingMap()
	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	store := newTestStore(regions, regionMap, regionClient, pm)

	c := &Collector{
		regions:        regions,
		regionMap:      regionMap,
		pricingMap:     pm,
		store:          store,
		accountID:      "123456789012",
		populateErrors: store.populateErrors,
		logger:         slog.Default(),
	}

	ch := make(chan prometheus.Metric, 1)
	err := c.Collect(t.Context(), ch)
	assert.NoError(t, err)

	select {
	case <-ch:
		t.Fatal("expected no metric before the store is populated")
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
	pricingClient.EXPECT().GetRDSUnitData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(validPriceJSON, nil).
		Times(1)

	pm := newPricingMap()
	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	store := newTestStore(regions, regionMap, pricingClient, pm)
	store.Populate(t.Context())

	c := &Collector{
		regions:        regions,
		regionMap:      regionMap,
		pricingMap:     pm,
		store:          store,
		accountID:      "123456789012",
		populateErrors: store.populateErrors,
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

	// The pricing API returns no data, so the key never lands in the map.
	pricingClient := mock.NewMockClient(mockCtrl)
	pricingClient.EXPECT().GetRDSUnitData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return("", nil).
		Times(1)

	pm := newPricingMap()
	regions := []types.Region{{RegionName: aws.String("us-east-1")}}
	regionMap := map[string]client.Client{"us-east-1": regionClient}
	store := newTestStore(regions, regionMap, pricingClient, pm)
	store.Populate(t.Context())

	c := &Collector{
		regions:        regions,
		regionMap:      regionMap,
		pricingMap:     pm,
		store:          store,
		accountID:      "123456789012",
		populateErrors: store.populateErrors,
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
	pricingClient.EXPECT().GetRDSUnitData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(validPriceJSON, nil).
		Times(len(regionNames))

	pm := newPricingMap()
	store := newTestStore(regions, regionMap, pricingClient, pm)
	store.Populate(t.Context())

	c := &Collector{
		regions:        regions,
		regionMap:      regionMap,
		pricingMap:     pm,
		store:          store,
		accountID:      "123456789012",
		populateErrors: store.populateErrors,
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
