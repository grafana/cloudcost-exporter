package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

func main() {
	options := []func(*config.LoadOptions) error{config.WithEC2IMDSRegion()}
	options = append(options, config.WithRegion("us-east-2"))
	options = append(options, config.WithSharedConfigProfile(os.Getenv("AWS_PROFILE")))
	cfg, err := config.LoadDefaultConfig(context.TODO(), options...)
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	client := ec2.NewFromConfig(cfg)

	// Call DescribeSpotPriceHistory
	var spotPrices []types.SpotPrice
	starTime := time.Now().Add(-time.Hour * 24)
	endTime := time.Now()
	sphi := &ec2.DescribeSpotPriceHistoryInput{
		ProductDescriptions: []string{
			"Linux/UNIX (Amazon VPC)", // replace with your product description
		},
		StartTime: &starTime,
		EndTime:   &endTime,
	}
	for {
		resp, err := client.DescribeSpotPriceHistory(context.TODO(), sphi)
		if err != nil {
			break
		}
		spotPrices = append(spotPrices, resp.SpotPriceHistory...)
		if resp.NextToken == nil || *resp.NextToken == "" {
			break
		}
		sphi.NextToken = resp.NextToken
	}

	spotPriceMap := map[string]map[string]types.SpotPrice{}
	// Print the spot prices
	for _, spotPrice := range spotPrices {
		az := *spotPrice.AvailabilityZone
		instanceType := string(spotPrice.InstanceType)
		if _, ok := spotPriceMap[az]; !ok {
			spotPriceMap[az] = map[string]types.SpotPrice{}
		}
		if _, ok := spotPriceMap[az][instanceType]; ok {
			// Check to see if the price is newer
			if spotPriceMap[az][instanceType].Timestamp.After(*spotPrice.Timestamp) {
				continue
			}
		}
		spotPriceMap[az][instanceType] = spotPrice
	}
	for region, prices := range spotPriceMap {
		fmt.Printf("Region: %s\n", region)
		for instanceType, price := range prices {
			fmt.Printf("Instance type: %s, price: %s\n", instanceType, *price.SpotPrice)
		}
	}
}
