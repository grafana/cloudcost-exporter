package ec2

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	mockec2 "github.com/grafana/cloudcost-exporter/mocks/pkg/aws/services/ec2"
	mockpricing "github.com/grafana/cloudcost-exporter/mocks/pkg/aws/services/pricing"
	"github.com/grafana/cloudcost-exporter/pkg/aws/compute"
	ec2client "github.com/grafana/cloudcost-exporter/pkg/aws/services/ec2"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stdout, nil))

func TestNewEC2Collector(t *testing.T) {
	t.Run("Instance is created", func(t *testing.T) {
		ec2 := New(context.Background(), &Config{
			Logger: testLogger,
		}, nil, nil, nil)
		assert.NotNil(t, ec2)
		assert.Equal(t, subsystem, ec2.Name())
	})
}

func TestCollector_CollectMetrics(t *testing.T) {
	t.Run("Returns 0", func(t *testing.T) {
		ec2 := New(context.Background(), &Config{
			Logger: testLogger,
		}, nil, nil, nil)
		result := ec2.CollectMetrics(nil)
		assert.Equal(t, 0.0, result)
	})
}

func TestCollector_Describe(t *testing.T) {
	t.Run("Returns nil", func(t *testing.T) {
		ec2 := New(context.Background(), &Config{
			Logger: testLogger,
		}, nil, nil, nil)
		result := ec2.Describe(nil)
		assert.Nil(t, result)
	})
}

func TestCollector_Collect(t *testing.T) {
	regions := []ec2Types.Region{
		{
			RegionName: aws.String("us-east-1"),
		},
	}
	config := &Config{
		Logger:  testLogger,
		Regions: regions,
	}
	t.Run("Collect should return no error", func(t *testing.T) {
		collector := New(context.Background(), &Config{
			Logger: testLogger,
		}, nil, nil, nil)
		ch := make(chan prometheus.Metric)
		go func() {
			err := collector.Collect(ch)
			close(ch)
			assert.NoError(t, err)
		}()
	})

	t.Run("Collect should return an error if ListOnDemandPrices returns an error", func(t *testing.T) {
		ps := mockpricing.NewPricing(t)
		ps.EXPECT().GetProducts(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
					return nil, assert.AnError
				}).Times(1)
		collector := New(context.Background(), config, ps, nil, nil)
		ch := make(chan prometheus.Metric)
		err := collector.Collect(ch)
		close(ch)
		assert.Error(t, err)
	})
	t.Run("Collect should return a ClientNotFound Error if the client is nil", func(t *testing.T) {
		ps := mockpricing.NewPricing(t)
		ps.EXPECT().GetProducts(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
					return &pricing.GetProductsOutput{
						PriceList: []string{},
					}, nil
				}).Times(1)
		collector := New(context.Background(), config, ps, nil, nil)
		ch := make(chan prometheus.Metric)
		err := collector.Collect(ch)
		close(ch)
		assert.ErrorIs(t, err, ErrClientNotFound)
	})
	t.Run("Collect should return an error if ListComputeInstances returns an error", func(t *testing.T) {
		ec2s := mockec2.NewEC2(t)
		ec2s.EXPECT().DescribeSpotPriceHistory(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
					return nil, assert.AnError
				}).Times(1)
		ps := mockpricing.NewPricing(t)
		ps.EXPECT().GetProducts(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
					return &pricing.GetProductsOutput{
						PriceList: []string{},
					}, nil
				}).Times(1)
		regionClientMap := make(map[string]ec2client.EC2)
		for _, r := range regions {
			regionClientMap[*r.RegionName] = ec2s
		}
		collector := New(context.Background(), config, ps, ec2s, regionClientMap)
		ch := make(chan prometheus.Metric)
		err := collector.Collect(ch)
		close(ch)
		assert.ErrorIs(t, err, compute.ErrListSpotPrices)
	})
	t.Run("Collect should return an error if GeneratePricingMap returns an error", func(t *testing.T) {
		ec2s := mockec2.NewEC2(t)
		ec2s.EXPECT().DescribeSpotPriceHistory(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, input *ec2.DescribeSpotPriceHistoryInput, optFns ...func(options *ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
					return &ec2.DescribeSpotPriceHistoryOutput{
						SpotPriceHistory: []ec2Types.SpotPrice{
							{
								AvailabilityZone: aws.String("us-east-1a"),
								InstanceType:     ec2Types.InstanceTypeC5ad2xlarge,
								SpotPrice:        aws.String("0.4680000000"),
							},
						},
					}, nil
				}).Times(1)
		ps := mockpricing.NewPricing(t)
		ps.EXPECT().GetProducts(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(
				func(ctx context.Context, input *pricing.GetProductsInput, optFns ...func(*pricing.Options)) (*pricing.GetProductsOutput, error) {
					return &pricing.GetProductsOutput{
						PriceList: []string{
							"Unparsable String into json",
						},
					}, nil
				}).Times(1)
		regionClientMap := make(map[string]ec2client.EC2)
		for _, r := range regions {
			regionClientMap[*r.RegionName] = ec2s
		}
		collector := New(context.Background(), config, ps, ec2s, regionClientMap)
		ch := make(chan prometheus.Metric)
		defer close(ch)
		assert.ErrorIs(t, collector.Collect(ch), ErrGeneratePricingMap)
	})
}

func TestCollector_Register(t *testing.T) {
	t.Run("Runs register", func(t *testing.T) {
		ec2 := New(context.Background(), &Config{
			Logger: testLogger,
		}, nil, nil, nil)
		err := ec2.Register(nil)
		assert.Nil(t, err)
	})
}
