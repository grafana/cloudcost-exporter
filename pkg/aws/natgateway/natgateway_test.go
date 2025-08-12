package natgateway

import (
	"context"
	"strings"
	"testing"
	"time"

	awscostexplorer "github.com/aws/aws-sdk-go-v2/service/costexplorer"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	ce_mocks "github.com/grafana/cloudcost-exporter/pkg/aws/services/mocks"
	mock_provider "github.com/grafana/cloudcost-exporter/pkg/provider/mocks"
)

func TestNewCollector(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	c := ce_mocks.NewMockCostExplorer(ctrl)
	col := New(1*time.Hour, c)
	assert.NotNil(t, col)
	assert.Equal(t, 1*time.Hour, col.interval)
}

func TestCollector_Name(t *testing.T) {
	c := &Collector{}
	require.Equal(t, "NATGATEWAY", c.Name())
}

func TestCollector_Register(t *testing.T) {
	ctrl := gomock.NewController(t)
	r := mock_provider.NewMockRegistry(ctrl)
	// HourlyGauge, DataProcessingGauge, RequestCount, RequestErrorsCount, NextScrapeGauge
	r.EXPECT().MustRegister(gomock.Any()).Times(5)

	c := &Collector{}
	err := c.Register(r)
	require.NoError(t, err)
}

func TestCollector_Collect_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ce := ce_mocks.NewMockCostExplorer(ctrl)
	ce.EXPECT().
		GetCostAndUsage(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
			return nil, assert.AnError
		}).Times(1)

	c := &Collector{client: ce, interval: 1 * time.Hour, nextScrape: time.Now().Add(-2 * time.Hour), metrics: NewMetrics()}
	up := c.CollectMetrics(nil)
	require.Equal(t, 0.0, up)
}

func TestCollector_Collect_EmptyOutput(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ce := ce_mocks.NewMockCostExplorer(ctrl)
	ce.EXPECT().
		GetCostAndUsage(gomock.Any(), gomock.Any()).
		Return(&awscostexplorer.GetCostAndUsageOutput{}, nil).
		Times(1)

	c := &Collector{client: ce, interval: 1 * time.Hour, nextScrape: time.Now().Add(-2 * time.Hour), metrics: NewMetrics()}
	up := c.CollectMetrics(nil)
	require.Equal(t, 1.0, up)

	r := prometheus.NewPedanticRegistry()
	require.NoError(t, c.Register(r))
	exposition := `
# HELP cloudcost_exporter_aws_natgateway_cost_api_requests_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_natgateway_cost_api_requests_errors_total counter
cloudcost_exporter_aws_natgateway_cost_api_requests_errors_total 0
# HELP cloudcost_exporter_aws_natgateway_cost_api_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_natgateway_cost_api_requests_total counter
cloudcost_exporter_aws_natgateway_cost_api_requests_total 1
`
	require.NoError(t, testutil.CollectAndCompare(r, strings.NewReader(exposition),
		"cloudcost_exporter_aws_natgateway_cost_api_requests_total",
		"cloudcost_exporter_aws_natgateway_cost_api_requests_errors_total",
	))
}

func TestCollector_Collect_NATGatewayMetrics(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	ce := ce_mocks.NewMockCostExplorer(ctrl)
	ce.EXPECT().
		GetCostAndUsage(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, params *awscostexplorer.GetCostAndUsageInput, optFns ...func(*awscostexplorer.Options)) (*awscostexplorer.GetCostAndUsageOutput, error) {
			a := "10"
			u := "GB"
			out := &awscostexplorer.GetCostAndUsageOutput{
				ResultsByTime: []types.ResultByTime{
					{
						Groups: []types.Group{
							{
								Keys: []string{"Amazon VPC", "USE1-NatGateway-Hours"},
								Metrics: map[string]types.MetricValue{
									"UsageQuantity": {Amount: &a, Unit: &u},
									"UnblendedCost": {Amount: &a, Unit: &u},
								},
							},
							{
								Keys: []string{"Amazon VPC", "USE1-NatGateway-Bytes"},
								Metrics: map[string]types.MetricValue{
									"UsageQuantity": {Amount: &a, Unit: &u},
									"UnblendedCost": {Amount: &a, Unit: &u},
								},
							},
						},
					},
				},
			}
			return out, nil
		}).Times(1)

	c := &Collector{client: ce, interval: 1 * time.Hour, nextScrape: time.Now().Add(-2 * time.Hour), metrics: NewMetrics()}
	up := c.CollectMetrics(nil)
	require.Equal(t, 1.0, up)

	r := prometheus.NewPedanticRegistry()
	require.NoError(t, c.Register(r))
	exposition := `
# HELP cloudcost_aws_natgateway_data_processing_usd_per_gb Data processing cost of NAT Gateway by region. Cost represented in USD/GB
# TYPE cloudcost_aws_natgateway_data_processing_usd_per_gb gauge
cloudcost_aws_natgateway_data_processing_usd_per_gb{region="us-east-1",service="Amazon VPC",usage_type="NatGateway-Bytes"} 1
# HELP cloudcost_aws_natgateway_hourly_rate_usd_per_hour Hourly cost of NAT Gateway by region. Cost represented in USD/hour
# TYPE cloudcost_aws_natgateway_hourly_rate_usd_per_hour gauge
cloudcost_aws_natgateway_hourly_rate_usd_per_hour{region="us-east-1",service="Amazon VPC",usage_type="NatGateway-Hours"} 1
# HELP cloudcost_exporter_aws_natgateway_cost_api_requests_errors_total Total number of errors when making requests to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_natgateway_cost_api_requests_errors_total counter
cloudcost_exporter_aws_natgateway_cost_api_requests_errors_total 0
# HELP cloudcost_exporter_aws_natgateway_cost_api_requests_total Total number of requests made to the AWS Cost Explorer API
# TYPE cloudcost_exporter_aws_natgateway_cost_api_requests_total counter
cloudcost_exporter_aws_natgateway_cost_api_requests_total 1
`
	require.NoError(t, testutil.CollectAndCompare(r, strings.NewReader(exposition),
		"cloudcost_aws_natgateway_hourly_rate_usd_per_hour",
		"cloudcost_aws_natgateway_data_processing_usd_per_gb",
		"cloudcost_exporter_aws_natgateway_cost_api_requests_total",
		"cloudcost_exporter_aws_natgateway_cost_api_requests_errors_total",
	))
}
