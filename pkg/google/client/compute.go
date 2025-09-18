package client

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/api/compute/v1"
)

var ErrListInstances = errors.New("no list price was found for the sku")

type Compute struct {
	computeService *compute.Service
}

func newCompute(computeService *compute.Service) *Compute {
	return &Compute{
		computeService: computeService,
	}
}

func (c *Compute) getZones(project string) ([]*compute.Zone, error) {
	zones, err := c.computeService.Zones.List(project).Do()
	if err != nil {
		return nil, err
	}
	return zones.Items, nil
}

func (c *Compute) getRegions(project string) ([]*compute.Region, error) {
	regions, err := c.computeService.Regions.List(project).Do()
	if err != nil {
		return nil, err
	}
	return regions.Items, nil
}

func (c *Compute) listInstancesInZone(projectId, zone string) ([]*MachineSpec, error) {
	var allInstances []*MachineSpec
	var nextPageToken string

	for {
		instances, err := c.computeService.Instances.List(projectId, zone).
			PageToken(nextPageToken).
			Do()
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrListInstances, err.Error())
		}
		for _, instance := range instances.Items {
			allInstances = append(allInstances, NewMachineSpec(instance))
		}
		nextPageToken = instances.NextPageToken
		if nextPageToken == "" {
			break
		}
	}
	return allInstances, nil
}

// listDisks will list all disks in a given zone and return a slice of compute.Disk
func (c *Compute) listDisks(ctx context.Context, project string, zone string) ([]*compute.Disk, error) {
	var disks []*compute.Disk
	// TODO: How do we get this to work for multi regional disks?
	err := c.computeService.Disks.List(project, zone).Pages(ctx, func(page *compute.DiskList) error {
		if page == nil {
			return nil
		}
		disks = append(disks, page.Items...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return disks, nil
}

func (c *Compute) listForwardingRules(ctx context.Context, project string, region string) ([]*compute.ForwardingRule, error) {
	var forwardingRules []*compute.ForwardingRule

	err := c.computeService.ForwardingRules.List(project, region).Pages(ctx, func(page *compute.ForwardingRuleList) error {
		if page == nil {
			return nil
		}
		forwardingRules = append(forwardingRules, page.Items...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return forwardingRules, nil
}
