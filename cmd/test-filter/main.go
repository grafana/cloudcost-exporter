package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"golang.org/x/sync/errgroup"
)

func countInstances(ctx context.Context, client *ec2.Client, filters []types.Filter) (int, time.Duration) {
	start := time.Now()
	input := &ec2.DescribeInstancesInput{MaxResults: aws.Int32(1000)}
	if len(filters) > 0 {
		input.Filters = filters
	}
	total := 0
	for {
		resp, err := client.DescribeInstances(ctx, input)
		if err != nil {
			panic(err)
		}
		for _, r := range resp.Reservations {
			total += len(r.Instances)
		}
		if resp.NextToken == nil || *resp.NextToken == "" {
			break
		}
		input.NextToken = resp.NextToken
	}
	return total, time.Since(start)
}

func getAZs(ctx context.Context, client *ec2.Client) []string {
	resp, err := client.DescribeAvailabilityZones(ctx, &ec2.DescribeAvailabilityZonesInput{
		Filters: []types.Filter{{Name: aws.String("state"), Values: []string{"available"}}},
	})
	if err != nil {
		panic(err)
	}
	azs := make([]string, 0, len(resp.AvailabilityZones))
	for _, az := range resp.AvailabilityZones {
		if az.ZoneName != nil {
			azs = append(azs, *az.ZoneName)
		}
	}
	return azs
}

func countInstancesParallel(ctx context.Context, client *ec2.Client, filters []types.Filter) (int, time.Duration) {
	start := time.Now()
	azs := getAZs(ctx, client)

	var mu sync.Mutex
	total := 0
	eg, egCtx := errgroup.WithContext(ctx)
	for _, az := range azs {
		az := az
		eg.Go(func() error {
			f := append(filters, types.Filter{Name: aws.String("availability-zone"), Values: []string{az}})
			count, _ := countInstances(egCtx, client, f)
			mu.Lock()
			total += count
			mu.Unlock()
			return nil
		})
	}
	eg.Wait()
	return total, time.Since(start)
}

func main() {
	ctx := context.Background()
	cfg, _ := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-2"))
	client := ec2.NewFromConfig(cfg)

	stateFilter := []types.Filter{{
		Name:   aws.String("instance-state-name"),
		Values: []string{"pending", "running", "stopping", "stopped"},
	}}

	fmt.Println("1. Sequential, no filter (original behaviour)...")
	allCount, allDur := countInstances(ctx, client, nil)
	fmt.Printf("   %d instances in %s\n\n", allCount, allDur)

	fmt.Println("2. Sequential, with state filter...")
	filteredCount, filteredDur := countInstances(ctx, client, stateFilter)
	fmt.Printf("   %d instances in %s\n\n", filteredCount, filteredDur)

	fmt.Println("3. Parallel by AZ + state filter (new behaviour)...")
	parallelCount, parallelDur := countInstancesParallel(ctx, client, stateFilter)
	fmt.Printf("   %d instances in %s\n\n", parallelCount, parallelDur)

	fmt.Printf("Terminated instances: %d (%.1f%% of total)\n", allCount-filteredCount, float64(allCount-filteredCount)/float64(allCount)*100)
	fmt.Printf("vs original sequential: %.1fx faster\n", float64(allDur)/float64(parallelDur))
}
