package common

import (
	"context"
	"fmt"
	"log"
	"strings"

	billingv1 "cloud.google.com/go/billing/apiv1"
	"cloud.google.com/go/billing/apiv1/billingpb"
	"google.golang.org/api/iterator"
)

// getServiceName finds the full catalog service name by its display name.
func GetServiceName(ctx context.Context, cc *billingv1.CloudCatalogClient, displayName string) (string, error) {
	it := cc.ListServices(ctx, &billingpb.ListServicesRequest{PageSize: 5000})
	for {
		svc, err := it.Next()
		if err != nil {
			if err == iterator.Done {
				break
			}
			return "", err
		}
		if svc.GetDisplayName() == displayName {
			return svc.GetName(), nil
		}
	}
	return "", fmt.Errorf("service not found: %s", displayName)
}

// getPricing lists all SKUs for a given service.
func GetPricing(ctx context.Context, cc *billingv1.CloudCatalogClient, serviceName string) []*billingpb.Sku {
	var out []*billingpb.Sku
	it := cc.ListSkus(ctx, &billingpb.ListSkusRequest{Parent: serviceName})
	for {
		sku, err := it.Next()
		if err != nil {
			if err == iterator.Done {
				break
			}
			// TODO(jjo): use slog
			log.Printf("sku iteration error: %v", err)
			continue
		}
		out = append(out, sku)
	}
	return out
}

// ParseProjects takes a project ID and a comma-separated list of project IDs
func ParseProjects(projectID, projectsCSV string) []string {
	if projectsCSV == "" {
		if projectID == "" {
			return nil
		}
		return []string{projectID}
	}
	parts := strings.Split(projectsCSV, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
