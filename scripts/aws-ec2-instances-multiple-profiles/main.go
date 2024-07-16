package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	ecfg "github.com/grafana/cloudcost-exporter/cmd/exporter/config"
)

type Config struct {
	roles   ecfg.StringSliceFlag
	profile string
}

func main() {
	c := &Config{}
	flag.Var(&c.roles, "role-arn", "Role ARN to connect too. Can be many")
	flag.StringVar(&c.profile, "profile", "", "AWS Profile to use for local authentication")
	flag.Parse()
	if len(c.roles) < 1 {
		log.Fatal("need at least one role")
	}
	for _, role := range c.roles {
		fmt.Printf("Role: %s\n", role)
	}
	options := []func(*config.LoadOptions) error{config.WithEC2IMDSRegion()}
	options = append(options, config.WithRegion("us-east-1"))
	if c.profile != "" {
		options = append(options, config.WithSharedConfigProfile(c.profile))
	}

	cfg, err := config.LoadDefaultConfig(context.TODO(), options...)
	if err != nil {
		log.Printf("Could not load config: %s", err.Error())
		os.Exit(1)
	}
	stsSvc := sts.NewFromConfig(cfg)

	creds := stscreds.NewAssumeRoleProvider(stsSvc, c.roles[0])
	cfg.Credentials = aws.NewCredentialsCache(creds)
	svc := ec2.NewFromConfig(cfg)
	reservations, err := svc.DescribeInstances(context.TODO(), &ec2.DescribeInstancesInput{
		DryRun:      nil,
		Filters:     nil,
		InstanceIds: nil,
		MaxResults:  nil,
		NextToken:   nil,
	})
	if err != nil {
		log.Printf("Could not describe instances %s", err.Error())
		os.Exit(1)
	}
	if reservations == nil {
		log.Printf("No reservations founds\n")
		os.Exit(1)
	}
	log.Printf("Found %d instances", len(reservations.Reservations))
}
