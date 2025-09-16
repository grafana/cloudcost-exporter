package rds

import (
	"testing"
	"time"

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
			},
			want: "AWS Region",
		},
		{
			name: "non-outposts instance type: DBSubnetGroup empty",
			inst: rdsTypes.DBInstance{
				DBSubnetGroup: &rdsTypes.DBSubnetGroup{},
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

func TestCollector_Collect(t *testing.T) {
	const cacheKey = "us-east-1a-db.t3.medium-mysql-Single-AZ-AWS Outposts"
	tests := []struct {
		name             string
		ListRDSInstances []rdsTypes.DBInstance
		pricingKey       string
	}{
		{
			name: "instance without price in cache",
			ListRDSInstances: []rdsTypes.DBInstance{rdsTypes.DBInstance{
				DBSubnetGroup: &rdsTypes.DBSubnetGroup{
					Subnets: []rdsTypes.Subnet{
						{
							SubnetOutpost: &rdsTypes.Outpost{
								Arn: aws.String("some-outpost-arn"),
							},
						},
					},
				},
				AvailabilityZone:     aws.String("us-east-1a"),
				DBInstanceClass:      aws.String("db.t3.medium"),
				Engine:               aws.String("postgres"),
				DBInstanceIdentifier: aws.String("test-db"),
				MultiAZ:              aws.Bool(false),
			}},
			pricingKey: createPricingKey("us-east-1", "db.t3.medium", "postgres", "Single-AZ", "AWS Region"),
		},
		{
			name: "instance with price in cache",
			ListRDSInstances: []rdsTypes.DBInstance{rdsTypes.DBInstance{
				DBSubnetGroup: &rdsTypes.DBSubnetGroup{
					Subnets: []rdsTypes.Subnet{
						{
							SubnetOutpost: &rdsTypes.Outpost{
								Arn: aws.String("some-outpost-arn"),
							},
						},
					},
				},
				AvailabilityZone:     aws.String("us-east-1a"),
				DBInstanceClass:      aws.String("db.t3.medium"),
				Engine:               aws.String("mysql"),
				DBInstanceIdentifier: aws.String("test-db-2"),
				MultiAZ:              aws.Bool(false),
			}},
			pricingKey: cacheKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			mockClient := mock.NewMockClient(mockCtrl)
			mockClient.EXPECT().ListRDSInstances(gomock.Any()).
				Return(tt.ListRDSInstances, nil).
				Times(1)

			// if the pricing key is not empty, then we expect the GetRDSUnitData method to be called
			if tt.pricingKey != "cache-key" {
				mockClient.EXPECT().GetRDSUnitData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(`{
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
        }`, nil).
					AnyTimes()
			}

			c := &Collector{
				pricingMap:     map[string]float64{tt.pricingKey: 0.456, cacheKey: 0.123},
				regions:        []types.Region{{RegionName: aws.String("us-east-1")}},
				regionMap:      map[string]client.Client{"us-east-1": mockClient},
				scrapeInterval: time.Minute,
				Client:         mockClient,
			}

			ch := make(chan prometheus.Metric, 1)
			err := c.Collect(ch)
			assert.NoError(t, err)

			select {
			case metric := <-ch:
				metricResult := utils.ReadMetrics(metric)
				close(ch)
				assert.NoError(t, err)
				labels := metricResult.Labels
				assert.Equal(t, *tt.ListRDSInstances[0].DBInstanceClass, labels["tier"])
				assert.Equal(t, *tt.ListRDSInstances[0].DBInstanceIdentifier, labels["name"])
				assert.Equal(t, c.pricingMap[tt.pricingKey], metricResult.Value)
			default:
				t.Fatal("expected a metric to be collected")
			}
		})
	}
}
